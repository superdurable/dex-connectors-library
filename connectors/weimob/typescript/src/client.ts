// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetStoreBasicDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetStoreBasicInput, StoreBasic } from './generated/models.js';

const empty: StoreBasic = { storeID: 0, name: '' };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Optional provider transport and clock for deterministic verification. */
export interface WeimobClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for token expiry. */ now?: () => Date;
}
/** Resolve current Weimob credentials and cache one authorized store token. */
export class WeimobClient {
  readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private token = '';
  private tokenFingerprint = '';
  private tokenExpiry = 0;
  /** Validate endpoint and bounds without contacting Weimob. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WeimobClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Weimob configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://dopen.weimob.com').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.pathname !== '/' || endpoint.search || endpoint.hash || endpoint.username || endpoint.password) throw new TypeError('Weimob endpoint must be an HTTPS root');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Weimob request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the authorized store Query. */ getStoreBasic(): GetStoreBasicOperation { return new GetStoreBasicOperation(this); }
  /** Resolve and validate current credential material. */
  async resolve(call: Call): Promise<{ clientID: string; clientSecret: string; shopID: string } | null> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const clientID = credentials.clientID;
      const clientSecret = credentials.clientSecret instanceof SecretString ? credentials.clientSecret.reveal() : '';
      const shopID = credentials.shopID ?? '';
      return validSecret(clientID) && validSecret(clientSecret) && (shopID === '' || /^\d{1,20}$/.test(shopID)) ? { clientID, clientSecret, shopID } : null;
    } catch { return null; }
  }
  /** Obtain or reuse a token for this exact credential triple. */
  async accessToken(call: Call, clientID: string, clientSecret: string, shopID: string): Promise<string | QueryAttempt<StoreBasic>> {
    const fingerprint = createHash('sha256').update(`${clientID}\0${clientSecret}\0${shopID}`).digest('hex');
    if (this.token && fingerprint === this.tokenFingerprint && this.tokenExpiry - this.now().getTime() > 60_000) return this.token;
    const query = new URLSearchParams({ grant_type: 'client_credentials', client_id: clientID, client_secret: clientSecret });
    if (shopID) { query.set('shop_id', shopID); query.set('shop_type', 'business_operation_system_id'); }
    const exchange = await this.request(call, `${this.baseUrl}/fuwu/b/oauth2/token?${query}`, 8192, false);
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'PROTOCOL');
    const data = decode(body);
    if (!isRecord(data)) return branch('invalidResponse', 'PROTOCOL');
    if (typeof data.error === 'string' && data.error) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (typeof data.access_token !== 'string' || !validSecret(data.access_token) || typeof data.expires_in !== 'number' || !Number.isInteger(data.expires_in) || data.expires_in < 120 || data.expires_in > 604800) return branch('invalidResponse', 'PROTOCOL');
    this.token = data.access_token;
    this.tokenFingerprint = fingerprint;
    this.tokenExpiry = this.now().getTime() + data.expires_in * 1000;
    return this.token;
  }
  /** Send one bounded Weimob POST. */
  async request(call: Call, url: string, maxResponseBytes: number, includeAccept: boolean): Promise<Exchange> {
    const headers = { 'Content-Type': 'application/json; charset=utf-8', ...(includeAccept ? { Accept: 'application/json' } : {}) };
    try {
      const response = await this.send({ url, method: 'POST', headers, body: Buffer.from('{}'), timeoutMs: this.timeoutMs, maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
      const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
      return { response, error: response.body.length > maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
  /** Read the authorized store after obtaining a token. */
  async store(call: Call, token: string): Promise<QueryAttempt<StoreBasic>> {
    const exchange = await this.request(call, `${this.baseUrl}/apigw/bos/v2.0/info/get?accesstoken=${encodeURIComponent(token)}`, this.maxResponseBytes, true);
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = decode(body);
    if (!isRecord(data) || !isRecord(data.code) || typeof data.code.errcode !== 'string' || !data.code.errcode) return branch('invalidResponse', 'PROTOCOL');
    if (data.code.errcode !== '0') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(data.data) || typeof data.data.bosId !== 'number' || !Number.isSafeInteger(data.data.bosId) || data.data.bosId <= 0 || typeof data.data.bosName !== 'string' || !data.data.bosName || data.data.bosLogo !== undefined && typeof data.data.bosLogo !== 'string' || data.data.desc !== undefined && typeof data.data.desc !== 'string') return branch('invalidResponse', 'PROTOCOL');
    const store: StoreBasic = { storeID: data.data.bosId, name: data.data.bosName };
    if (typeof data.data.bosLogo === 'string' && data.data.bosLogo) store.logo = data.data.bosLogo;
    if (typeof data.data.desc === 'string' && data.data.desc) store.description = data.data.desc;
    return newQueryBranch('found', store);
  }
}
/** Read the public profile of the authorized WOS store. */
export class GetStoreBasicOperation implements Query<GetStoreBasicInput, StoreBasic> {
  /** Bind one validated client. */ constructor(private readonly client: WeimobClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetStoreBasicDefinition; }
  /** Obtain a token and read the authorized store. */
  async invoke(call: Call): Promise<QueryAttempt<StoreBasic>> {
    const credentials = await this.client.resolve(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const token = await this.client.accessToken(call, credentials.clientID, credentials.clientSecret, credentials.shopID);
    return typeof token === 'string' ? this.client.store(call, token) : token;
  }
}
function validSecret(value: string): boolean { return Boolean(value) && value.length <= 4096 && /^[!-~]+$/.test(value); }
function decode(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return null; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'weimob', operation: 'getStoreBasic', kind, message: 'Weimob store request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<StoreBasic> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<StoreBasic> { return newQueryRetry(failure(kind), delay); }
