// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDevicesDefinition } from './generated/definitions.js';
import type { Config, Credentials, Device, DevicePage, ListDevicesInput } from './generated/models.js';
import { signDeviceRequest } from './signature.js';

const emptyPage: DevicePage = { totalCount: 0, pageNum: 0, pageSize: 0, devices: [] };

/** Send one device-list request through a caller-owned transport. */
export type DeviceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional request dependencies for deterministic provider tests. */
export interface AqaraClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: DeviceRequestSender;
  /** Request clock; production uses the current system time. */
  clock?: () => Date;
  /** Sixteen random nonce bytes; production uses cryptographic randomness. */
  nonceSource?: () => Uint8Array;
}

/** Resolve Aqara project credentials for each device query. */
export class AqaraClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: DeviceRequestSender;
  readonly clock: () => Date;
  readonly nonceSource: () => Uint8Array;

  /** Validate configuration without contacting Aqara. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: AqaraClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Aqara configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://open-cn.aqara.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Aqara timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.clock = options.clock ?? (() => new Date());
    this.nonceSource = options.nonceSource ?? (() => randomBytes(16));
  }

  /** Bind the one-page device Query. */
  listDevices(): ListDevicesOperation { return new ListDevicesOperation(this); }
}

/** Read one bounded page of authorized Aqara devices. */
export class ListDevicesOperation implements Query<ListDevicesInput, DevicePage> {
  /** Bind an authenticated Aqara client. */
  constructor(private readonly client: AqaraClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListDevicesDefinition; }

  /** Sign, send, and classify one device-list request. */
  async invoke(call: Call, input: ListDevicesInput): Promise<QueryAttempt<DevicePage>> {
    const pageNum = input?.pageNum === 0 ? 1 : input?.pageNum;
    const pageSize = input?.pageSize === 0 ? 50 : input?.pageSize;
    if (!Number.isSafeInteger(pageNum) || !Number.isSafeInteger(pageSize) || (pageNum ?? 0) < 1 || (pageNum ?? 0) > 1_000_000 || (pageSize ?? 0) < 1 || (pageSize ?? 0) > 100) return branch('defect', 'VALIDATION', 'pageNum or pageSize is outside the supported range');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Aqara connection credentials are unavailable or invalid'); }
    const appId = credentials.appId;
    const keyId = credentials.keyId;
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (![appId, keyId, appKey, accessToken].every(isValidCredential)) return branch('defect', 'AUTHENTICATION', 'Aqara connection credentials are unavailable or invalid');
    let nonce: string;
    try {
      const bytes = this.client.nonceSource();
      if (bytes.length !== 16) throw new TypeError('nonce must contain sixteen bytes');
      nonce = Buffer.from(bytes).toString('hex');
    } catch { return retry('AVAILABILITY', 'secure nonce generation failed'); }
    const timestamp = String(this.client.clock().getTime());
    if (!/^\d+$/.test(timestamp)) return branch('defect', 'VALIDATION', 'device request is invalid');
    const body = Buffer.from(JSON.stringify({ intent: 'query.device.info', data: { pageNum, pageSize } }), 'utf8');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${this.client.baseUrl}/v3.0/open/api`, method: 'POST', body,
        headers: {
          Accept: 'application/json', 'Content-Type': 'application/json',
          Accesstoken: accessToken, Appid: appId, Keyid: keyId, Nonce: nonce, Time: timestamp,
          Sign: signDeviceRequest(accessToken, appId, keyId, nonce, timestamp, appKey),
        },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: this.client.baseUrl === 'https://open-cn.aqara.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const early = classifyEarlyStatus(error.status, error.headers ?? {});
        if (early) return early;
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Aqara response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Aqara request failed');
    }
    const early = classifyEarlyStatus(response.status, response.headers);
    if (early) return early;
    if (response.status !== 200) {
      const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 400 || response.status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return branch('providerRejected', kind, `Aqara rejected the query with HTTP ${response.status}`);
    }
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Aqara returned an invalid device response'); }
    if (!isRecord(document) || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL', 'Aqara returned an invalid device response');
    const code = document.code as number;
    if (code !== 0) return classifyProviderCode(code, retryAfter(response.headers));
    const result = document.result;
    if (!isRecord(result) || !Array.isArray(result.data)) return branch('invalidResponse', 'PROTOCOL', 'Aqara returned an invalid device response');
    const totalCount = result.totalCount === undefined ? 0 : result.totalCount;
    if (!Number.isSafeInteger(totalCount) || (totalCount as number) < 0 || result.data.length > pageSize!) return branch('invalidResponse', 'PROTOCOL', 'Aqara returned an invalid device response');
    const devices: Device[] = [];
    for (const row of result.data) {
      if (!isRecord(row) || typeof row.did !== 'string' || !row.did || typeof row.model !== 'string' || !row.model || !Number.isInteger(row.state) || (row.state !== 0 && row.state !== 1) || !isOptionalString(row.deviceName) || !isOptionalString(row.firmwareVersion) || !isOptionalString(row.positionId) || !isOptionalString(row.parentDid) || !isOptionalString(row.timeZone) || !isOptionalInteger(row.modelType)) return branch('invalidResponse', 'PROTOCOL', 'Aqara returned an invalid device response');
      devices.push({
        deviceId: row.did, deviceName: optionalString(row.deviceName), model: row.model,
        modelType: optionalInteger(row.modelType), online: row.state === 1,
        firmwareVersion: optionalString(row.firmwareVersion), positionId: optionalString(row.positionId),
        parentDeviceId: optionalString(row.parentDid), timeZone: optionalString(row.timeZone),
      });
    }
    return newQueryBranch('listed', { totalCount: totalCount as number, pageNum: pageNum!, pageSize: pageSize!, devices });
  }
}

function classifyEarlyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<DevicePage> | undefined {
  const delay = retryAfter(headers);
  if (status === 429) return retry('RATE_LIMIT', 'Aqara rate limited the request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Aqara is temporarily unavailable', delay);
  return undefined;
}
function classifyProviderCode(code: number, delay: number): QueryAttempt<DevicePage> {
  if (code === 429) return retry('RATE_LIMIT', 'Aqara rate limited the request', delay);
  if ([100, 104, 500, 501].includes(code)) return retry('AVAILABILITY', 'Aqara is temporarily unavailable', delay);
  const kind: Failure['kind'] = [106, 107, 108, 109, 804, 2002, 2004, 2005].includes(code) ? 'AUTHENTICATION' : [403, 603, 706, 717, 755, 756, 803, 2008, 2010, 2013, 2014].includes(code) ? 'AUTHORIZATION' : [101, 102, 103, 105, 302, 303, 304, 305, 306, 604].includes(code) ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Aqara rejected the query with code ${code}`);
}
function retryAfter(headers: ProviderResponse['headers']): number {
  const value = headers['retry-after'];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
}
function optionalString(value: unknown): string { return typeof value === 'string' ? value : ''; }
function optionalInteger(value: unknown): number { return Number.isSafeInteger(value) ? value as number : 0; }
function isOptionalString(value: unknown): boolean { return value === undefined || value === null || typeof value === 'string'; }
function isOptionalInteger(value: unknown): boolean { return value === undefined || value === null || Number.isSafeInteger(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidCredential(value: unknown): boolean { return typeof value === 'string' && value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'aqara', operation: 'listDevices', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<DevicePage> { return newQueryBranch(id, emptyPage, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<DevicePage> { return newQueryRetry(failure(kind, message), delayMs); }
