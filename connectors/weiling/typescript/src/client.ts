// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { integerCode, parseBalanceResponse } from './provider-json.js';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDepartmentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListDepartmentsInput, ListDepartmentsOutput } from './generated/models.js';

const empty: ListDepartmentsOutput = { departments: [] };
type StageResponse = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Optional provider transport and clock for verification. */
export interface WeilingClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for token expiry. */ now?: () => Date;
}
/** Resolve rotated Weiling credentials and cache their access token in memory. */
export class WeilingClient {
  readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private token = '';
  private tokenFingerprint = '';
  private tokenExpiry = 0;
  private refreshFinished: Promise<void> = Promise.resolve();
  /** Validate documented origin and bounds without contacting Weiling. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WeilingClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Weiling configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://openapi.weiling.cn').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.host !== 'openapi.weiling.cn' || endpoint.pathname !== '/' || endpoint.search || endpoint.hash || endpoint.username || endpoint.password) throw new TypeError('Weiling origin must be the documented HTTPS host');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Weiling request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the department-directory Query. */ listDepartments(): ListDepartmentsOperation { return new ListDepartmentsOperation(this); }
  /** Resolve the current credential pair before dispatch. */
  async resolve(call: Call): Promise<{ appId: string; appSecret: string } | null> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
      const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      return validSecret(appId) && validSecret(appSecret) ? { appId, appSecret } : null;
    } catch { return null; }
  }
  /** Acquire and cache a token for the active credentials. */
  async accessToken(call: Call, appId: string, appSecret: string): Promise<string | QueryAttempt<ListDepartmentsOutput>> {
    const previous = this.refreshFinished;
    let release!: () => void;
    this.refreshFinished = new Promise<void>(resolve => { release = resolve; });
    await previous;
    try { return await this.refreshToken(call, appId, appSecret); }
    finally { release(); }
  }
  private async refreshToken(call: Call, appId: string, appSecret: string): Promise<string | QueryAttempt<ListDepartmentsOutput>> {
    const fingerprint = createHash('sha256').update(`${appId}\0${appSecret}`).digest('hex');
    if (this.token && fingerprint === this.tokenFingerprint && this.now().getTime() < this.tokenExpiry) return this.token;
    const query = new URLSearchParams({ app_id: appId, app_secret: appSecret });
    const outcome = await this.request(call, `${this.baseUrl}/openapi/auth/access_token/get?${query}`);
    const issue = classify(outcome);
    if (issue) return issue;
    const data = decode(outcome.response?.body);
    if (!isRecord(data) || (data.data != null && !isRecord(data.data))) return branch('invalidResponse', 'PROTOCOL');
    const code = integerCode(data.code);
    const token = data.data?.access_token ?? '', expiry = data.data?.expires_in == null ? 0n : integerCode(data.data.expires_in);
    if (code === undefined || expiry === undefined || typeof token !== 'string') return branch('invalidResponse', 'PROTOCOL');
    if (code !== 0n) return providerFailure(code);
    if (!validSecret(token) || expiry < 180n || expiry > 86400n) return branch('invalidResponse', 'PROTOCOL');
    this.token = token;
    this.tokenFingerprint = fingerprint;
    this.tokenExpiry = this.now().getTime() + (Number(expiry) - 120) * 1000;
    return this.token;
  }
  /** Send one bounded authorization or department request. */
  async request(call: Call, url: string): Promise<StageResponse> {
    try {
      const response = await this.send({ url, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });

      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: retryDelay(response.headers) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: retryDelay(error.headers ?? {}) };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}
/** Read accessible Weiling CRM departments without employee identifiers. */
export class ListDepartmentsOperation implements Query<ListDepartmentsInput, ListDepartmentsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: WeilingClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListDepartmentsDefinition; }
  /** Obtain a token, then read the directory. */
  async invoke(call: Call): Promise<QueryAttempt<ListDepartmentsOutput>> {
    const credentials = await this.client.resolve(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const token = await this.client.accessToken(call, credentials.appId, credentials.appSecret);
    if (typeof token !== 'string') return token;
    const query = new URLSearchParams({ access_token: token });
    const outcome = await this.client.request(call, `${this.client.baseUrl}/openapi/department/list?${query}`);
    const issue = classify(outcome);
    if (issue) return issue;
    const data = decode(outcome.response?.body);
    if (!isRecord(data) || (data.data != null && !isRecord(data.data))) return branch('invalidResponse', 'PROTOCOL');
    const code = integerCode(data.code), rows = data.data?.data_list;
    if (code === undefined || (rows != null && !Array.isArray(rows))) return branch('invalidResponse', 'PROTOCOL');
    const departments: NonNullable<ListDepartmentsOutput['departments']> = [];
    for (const item of (rows ?? []) as unknown[]) {
      if (item !== null && !isRecord(item)) return branch('invalidResponse', 'PROTOCOL');
      const id = item?.dept_id ?? '', name = item?.name ?? '', parentID = item?.parent_id ?? '';
      if (typeof id !== 'string' || typeof name !== 'string' || typeof parentID !== 'string') return branch('invalidResponse', 'PROTOCOL');
      departments.push({ id, name, parentID });
    }
    if (code !== 0n) return providerFailure(code);
    if (!rows || departments.some(row => !row.id || Buffer.byteLength(row.id) > 256 || !row.name || Buffer.byteLength(row.name) > 512 || Buffer.byteLength(row.parentID) > 256)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { departments });
  }
}
function retryDelay(headers: ProviderResponse['headers']): number {
  const value = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
}
function validSecret(value: string): boolean { return Boolean(value) && value.length <= 2048 && /^[!-~]+$/.test(value); }
function decode(body: Uint8Array | undefined): unknown { try { return body ? parseBalanceResponse(body) : null; } catch { return null; } }
function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && Object.getPrototypeOf(value) === Object.prototype; }
function classify(outcome: StageResponse): QueryAttempt<ListDepartmentsOutput> | null {
  if (outcome.error === 'transport' || !outcome.response) return retry('AVAILABILITY');
  const status = outcome.response.status;
  if (status === 429) return retry('RATE_LIMIT', outcome.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', outcome.retryAfterMs);
  if (outcome.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
  return null;
}
function providerFailure(code: bigint): QueryAttempt<ListDepartmentsOutput> { return code === 1009n ? retry('RATE_LIMIT') : branch('providerRejected', [403n, 1007n, 1008n].includes(code) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION'); }
function failure(kind: Failure['kind']): Failure { return { provider: 'weiling', operation: 'listDepartments', kind, message: 'Weiling department request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListDepartmentsOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListDepartmentsOutput> { return newQueryRetry(failure(kind), delay); }
