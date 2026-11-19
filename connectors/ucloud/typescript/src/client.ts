// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetRegionDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetRegionInput, GetRegionOutput, RegionInfo } from './generated/models.js';

const emptyOutput: GetRegionOutput = { regions: [] };
/** Send a bounded UCloud HTTP request. */
export type RegionRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned transport. */
export interface UCloudClientOptions { send?: RegionRequestSender }
/** Resolve UCloud API keys for each region Query. */
export class UCloudClient {
  readonly endpoint: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: RegionRequestSender;
  /** Validate local settings without contacting UCloud. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: UCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('UCloud configuration and credentials are required');
    this.endpoint = config.baseUrl ?? 'https://api.ucloud.cn';
    const url = new URL(this.endpoint);
    if (url.protocol !== 'https:' || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('UCloud base URL is invalid');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('UCloud configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the GetRegion Query. */ getRegion(): GetRegionOperation { return new GetRegionOperation(this); }
}
/** Read the account's accessible regions. */
export class GetRegionOperation implements Query<GetRegionInput, GetRegionOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: UCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetRegionDefinition; }
  /** Sign and classify one GetRegion response. */
  async invoke(call: Call, input: GetRegionInput): Promise<QueryAttempt<GetRegionOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const publicKey = credentials.publicKey instanceof SecretString ? credentials.publicKey.reveal() : '';
    const privateKey = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    if (!isValidKey(publicKey) || !isValidKey(privateKey)) return branch('defect', 'AUTHENTICATION');
    const signature = createHash('sha1').update(`ActionGetRegionPublicKey${publicKey}${privateKey}`).digest('hex');
    const requestBody = JSON.stringify({ Action: 'GetRegion', PublicKey: publicKey, Signature: signature });
    let response: ProviderResponse;
    try { response = await this.client.send({ url: this.client.endpoint, method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode(requestBody), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || wire.Action !== 'GetRegionResponse' || !Number.isInteger(wire.RetCode)) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.RetCode as number;
    if ([110, 130, 150].includes(code)) return retry('AVAILABILITY');
    if (code === 153) return retry('RATE_LIMIT');
    if ([170, 171, 172, 174].includes(code)) return branch('providerRejected', 'AUTHENTICATION');
    if ([293, 294, 295, 299].includes(code)) return branch('providerRejected', 'AUTHORIZATION');
    if (code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (wire.Regions !== undefined && !Array.isArray(wire.Regions)) return branch('invalidResponse', 'PROTOCOL');
    const regions: RegionInfo[] = [];
    for (const item of (wire.Regions ?? []) as unknown[]) {
      if (!isRecord(item) || !Number.isSafeInteger(item.RegionId) || typeof item.RegionName !== 'string' || !item.RegionName || typeof item.IsDefault !== 'boolean' || typeof item.BitMaps !== 'string' || typeof item.Region !== 'string' || !item.Region || typeof item.Zone !== 'string' || !item.Zone) return branch('invalidResponse', 'PROTOCOL');
      regions.push({ regionId: item.RegionId as number, regionName: item.RegionName, isDefault: item.IsDefault, bitMaps: item.BitMaps, region: item.Region, zone: item.Zone });
    }
    return newQueryBranch('found', { regions });
  }
}
function isValidKey(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetRegionOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'ucloud', operation: 'getRegion', kind, message: 'UCloud GetRegion failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<GetRegionOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetRegionOutput> { return newQueryRetry(failure(kind), delayMs); }
