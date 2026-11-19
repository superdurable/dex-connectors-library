// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetOnlineVersionDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetOnlineVersionInput, GetOnlineVersionOutput } from './generated/models.js';

const endpoint = 'https://developer-api.vivo.com.cn/gateway/developer/app/publish/v1/get-online-version';
const emptyOutput: GetOnlineVersionOutput = { packageName: '', versionName: '', versionCode: 0 };
/** Send a signed vivo request through caller-owned transport. */
export type VersionRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional provider transport and timestamp source for deterministic tests. */
export interface VivoClientOptions { send?: VersionRequestSender; clock?: () => number }
/** Resolve developer credentials for each online-version Query. */
export class VivoClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: VersionRequestSender;
  readonly clock: () => number;
  /** Validate connection settings without contacting vivo. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: VivoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('vivo configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('vivo configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.clock = options.clock ?? Date.now;
    if (typeof this.clock !== 'function') throw new TypeError('vivo clock is required');
  }
  /** Bind the online-version Query. */
  getOnlineVersion(): GetOnlineVersionOperation { return new GetOnlineVersionOperation(this); }
}
/** Read one published Android app version. */
export class GetOnlineVersionOperation implements Query<GetOnlineVersionInput, GetOnlineVersionOutput> {
  /** Bind an authenticated client. */
  constructor(private readonly client: VivoClient) {}
  /** Return the generated Query definition. */
  definition(): QueryDefinition { return GetOnlineVersionDefinition; }
  /** Sign, send, and classify one version request. */
  async invoke(call: Call, input: GetOnlineVersionInput): Promise<QueryAttempt<GetOnlineVersionOutput>> {
    if (!input || typeof input.packageName !== 'string' || input.packageName.length > 255 || !/^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$/.test(input.packageName)) return branch('defect', 'VALIDATION', 'vivo package name is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'vivo developer credentials are unavailable or invalid'); }
    const account = credentials.account instanceof SecretString ? credentials.account.reveal() : '';
    const secret = credentials.secret instanceof SecretString ? credentials.secret.reveal() : '';
    if (!validCredential(account) || !validCredential(secret)) return branch('defect', 'AUTHENTICATION', 'vivo developer credentials are unavailable or invalid');
    const timestamp = this.client.clock();
    if (!Number.isSafeInteger(timestamp) || timestamp <= 0) return branch('defect', 'VALIDATION', 'vivo signing timestamp is invalid');
    const bizParam = { packageName: input.packageName };
    const signatureInput = `account=${account}&bizParam=${JSON.stringify(bizParam)}&timestamp=${timestamp}`;
    const sign = createHmac('sha256', secret).update(signatureInput).digest('hex');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: endpoint, method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode(JSON.stringify({ account, timestamp, sign, bizParam })), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'vivo response exceeded configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'vivo version request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'vivo returned malformed version data'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL', 'vivo returned malformed version data');
    const code = wire.code as number;
    if ([11001, 11002, 11003, 11004].includes(code)) return newQueryBranch('noData', { packageName: input.packageName, versionName: '', versionCode: 0 });
    if ([10003, 10006, 500].includes(code)) return retry('AVAILABILITY', 'vivo reported temporary query failure');
    if (code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `vivo rejected version request with code ${code}`);
    if (!isRecord(wire.data) || wire.data.packageName !== input.packageName || typeof wire.data.versionName !== 'string' || !wire.data.versionName) return branch('invalidResponse', 'PROTOCOL', 'vivo returned mismatched version data');
    const versionCode = typeof wire.data.versionCode === 'string' && /^\d+$/.test(wire.data.versionCode) ? Number(wire.data.versionCode) : wire.data.versionCode;
    if (typeof versionCode !== 'number' || !Number.isSafeInteger(versionCode) || versionCode <= 0) return branch('invalidResponse', 'PROTOCOL', 'vivo returned invalid version code');
    return newQueryBranch('found', { packageName: input.packageName, versionName: wire.data.versionName, versionCode });
  }
}
function validCredential(value: string): boolean { return !!value && value.length <= 1024 && value.trim() === value && ![...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetOnlineVersionOutput> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'vivo rate limited version request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'vivo API is temporarily unavailable', delay);
  return branch('providerRejected', 'PROVIDER_REJECTION', `vivo rejected version request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'vivo', operation: 'getOnlineVersion', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetOnlineVersionOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetOnlineVersionOutput> { return newQueryRetry(failure(kind, message), delayMs); }
