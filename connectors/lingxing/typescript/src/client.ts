// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createCipheriv, createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListStoresDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListStoresInput, StoreList } from './generated/models.js';

const empty: StoreList = { stores: [], truncated: false };
type Outcome = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Optional deterministic provider transport and clock. */
export interface LingxingClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for signatures and token expiry. */ now?: () => Date;
}
/** Resolve rotated credentials and cache a short-lived Lingxing access token in memory. */
export class LingxingClient {
  readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  private token = '';
  private tokenFingerprint = '';
  private tokenExpiry = 0;
  /** Validate settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: LingxingClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Lingxing configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://openapi.lingxing.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/' || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Lingxing endpoint or bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the authorized-store Query. */ listStores(): ListStoresOperation { return new ListStoresOperation(this); }
  /** Resolve credentials and reject invalid material before dispatch. */
  async resolve(call: Call): Promise<{ appId: string; appSecret: string } | null> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
      const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      if (![16, 24, 32].includes(Buffer.byteLength(appId)) || !validSecret(appId) || !validSecret(appSecret)) return null;
      return { appId, appSecret };
    } catch { return null; }
  }
  /** Obtain a token for the current credential pair. */
  async accessToken(call: Call, appId: string, appSecret: string): Promise<string | QueryAttempt<StoreList>> {
    const fingerprint = createHash('sha256').update(`${appId}\0${appSecret}`).digest('hex');
    if (this.token && this.tokenFingerprint === fingerprint && this.now().getTime() < this.tokenExpiry) return this.token;
    const boundary = 'lingxing-dex-boundary';
    const body = Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="appId"\r\n\r\n${appId}\r\n--${boundary}\r\nContent-Disposition: form-data; name="appSecret"\r\n\r\n${appSecret}\r\n--${boundary}--\r\n`);
    const response = await this.request(call, { url: `${this.baseUrl}/api/auth-server/oauth/access-token`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': `multipart/form-data; boundary=${boundary}` }, body });
    if (response.error === 'transport' || !response.response) return retry('AVAILABILITY');
    const { status } = response.response;
    if (status === 429) return retry('RATE_LIMIT', response.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY');
    if (response.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', httpKind(status));
    const value = decode(response.response.body);
    if (!isRecord(value) || providerCode(value.code) === null) return branch('invalidResponse', 'PROTOCOL');
    if (providerCode(value.code) !== 200) return branch('providerRejected', 'AUTHENTICATION');
    if (!isRecord(value.data) || typeof value.data.access_token !== 'string' || !validSecret(value.data.access_token)) return branch('invalidResponse', 'PROTOCOL');
    const expiry = providerCode(value.data.expires_in);
    if (expiry === null || expiry < 60 || expiry > 86400) return branch('invalidResponse', 'PROTOCOL');
    this.token = value.data.access_token;
    this.tokenFingerprint = fingerprint;
    this.tokenExpiry = this.now().getTime() + (expiry - 30) * 1000;
    return this.token;
  }
  /** Send a bounded request with the active cancellation signal. */
  async request(call: Call, request: Pick<ProviderRequest, 'url' | 'method' | 'headers' | 'body'>): Promise<Outcome> {
    try {
      const response = await this.send({ ...request, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
      const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now()) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}
/** Read authorized Amazon store metadata, without account details. */
export class ListStoresOperation implements Query<ListStoresInput, StoreList> {
  /** Bind one validated client. */ constructor(private readonly client: LingxingClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListStoresDefinition; }
  /** Acquire a token, sign the store request, and classify its response. */
  async invoke(call: Call): Promise<QueryAttempt<StoreList>> {
    const credentials = await this.client.resolve(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const token = await this.client.accessToken(call, credentials.appId, credentials.appSecret);
    if (typeof token !== 'string') return token;
    const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const canonical = `access_token=${token}&app_key=${credentials.appId}&timestamp=${timestamp}`;
    const digest = createHash('md5').update(canonical).digest('hex').toUpperCase();
    const cipher = createCipheriv(`aes-${Buffer.byteLength(credentials.appId) * 8}-ecb`, Buffer.from(credentials.appId), null);
    const signature = Buffer.concat([cipher.update(digest), cipher.final()]).toString('base64');
    const query = new URLSearchParams({ app_key: credentials.appId, timestamp, sign: signature });
    const outcome = await this.client.request(call, { url: `${this.client.baseUrl}/erp/sc/data/seller/lists?${query}`, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' } });
    if (outcome.error === 'transport' || !outcome.response) return retry('AVAILABILITY');
    const { status } = outcome.response;
    if (status === 429) return retry('RATE_LIMIT', outcome.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', outcome.retryAfterMs);
    if (outcome.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', httpKind(status));
    const data = decode(outcome.response.body);
    if (!isRecord(data) || providerCode(data.code) === null) return branch('invalidResponse', 'PROTOCOL');
    if (providerCode(data.code) === 3001008) return retry('RATE_LIMIT');
    if (providerCode(data.code) !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(data.data)) return branch('invalidResponse', 'PROTOCOL');
    const stores: NonNullable<StoreList['stores']> = [];
    for (const item of data.data) {
      if (!isRecord(item) || typeof item.sid !== 'number' || !Number.isSafeInteger(item.sid) || item.sid <= 0 || typeof item.name !== 'string' || !item.name || typeof item.region !== 'string' || typeof item.country !== 'string' || typeof item.status !== 'number' || !Number.isInteger(item.status) || item.status < 0 || item.status > 3) return branch('invalidResponse', 'PROTOCOL');
      if (stores.length < 200) stores.push({ storeID: item.sid, name: item.name, region: item.region, country: item.country, status: item.status });
    }
    return newQueryBranch('listed', { stores, truncated: data.data.length > 200 });
  }
}
function validSecret(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value); }
function providerCode(value: unknown): number | null { if (typeof value === 'number' && Number.isInteger(value)) return value; if (typeof value === 'string' && /^[+-]?\d+$/.test(value)) return Number(value); return null; }
function decode(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return null; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function httpKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function failure(kind: Failure['kind']): Failure { return { provider: 'lingxing', operation: 'listStores', kind, message: 'Lingxing store request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<StoreList> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<StoreList> { return newQueryRetry(failure(kind), delay); }
