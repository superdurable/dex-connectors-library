// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListRootGoodsCategoriesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput } from './generated/models.js';

const empty: ListRootGoodsCategoriesOutput = { categories: [] };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Optional provider transport and clock for deterministic verification. */
export interface XiaoetechClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply current time for token expiry. */ now?: () => Date;
}
/** Cache an access token for the active XiaoeTech credential triple. */
export class XiaoetechClient {
  readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private token = '';
  private tokenFingerprint = '';
  private tokenExpiry = 0;
  /** Validate endpoint and bounds without contacting XiaoeTech. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: XiaoetechClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('XiaoeTech configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://api.xiaoe-tech.com').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.pathname !== '/' || endpoint.search || endpoint.hash || endpoint.username || endpoint.password) throw new TypeError('XiaoeTech endpoint must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('XiaoeTech request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the root category Query. */ listRootGoodsCategories(): ListRootGoodsCategoriesOperation { return new ListRootGoodsCategoriesOperation(this); }
  /** Resolve current credentials before dispatch. */
  async resolve(call: Call): Promise<{ appId: string; clientId: string; secretKey: string } | null> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId;
      const clientId = credentials.clientId;
      const secretKey = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
      return [appId, clientId, secretKey].every(validSecret) ? { appId, clientId, secretKey } : null;
    } catch { return null; }
  }
  /** Obtain a token for this exact credential triple. */
  async accessToken(call: Call, appId: string, clientId: string, secretKey: string): Promise<string | QueryAttempt<ListRootGoodsCategoriesOutput>> {
    const fingerprint = createHash('sha256').update(`${appId}\0${clientId}\0${secretKey}`).digest('hex');
    if (this.token && this.tokenFingerprint === fingerprint && this.tokenExpiry - this.now().getTime() > 300_000) return this.token;
    const query = new URLSearchParams({ app_id: appId, client_id: clientId, secret_key: secretKey, grant_type: 'client_credential' });
    const exchange = await this.request(call, `${this.baseUrl}/token?${query}`, 'GET');
    const issue = classify(exchange, true);
    if (issue) return issue;
    const data = decode(exchange.response?.body);
    if (!isRecord(data) || typeof data.code !== 'number' || !Number.isInteger(data.code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.code !== 0) return providerCode(data.code, false);
    if (!isRecord(data.data) || typeof data.data.access_token !== 'string' || !validSecret(data.data.access_token) || typeof data.data.expires_in !== 'number' || !Number.isInteger(data.data.expires_in) || data.data.expires_in < 600 || data.data.expires_in > 86400) return branch('invalidResponse', 'PROTOCOL');
    this.token = data.data.access_token;
    this.tokenFingerprint = fingerprint;
    this.tokenExpiry = this.now().getTime() + data.data.expires_in * 1000;
    return this.token;
  }
  /** Send one bounded token or category request. */
  async request(call: Call, url: string, method: 'GET' | 'POST', body?: Uint8Array): Promise<Exchange> {
    try {
      const response = await this.send({ url, method, headers: method === 'GET' ? { Accept: 'application/json' } : { Accept: 'application/json', 'Content-Type': 'application/json' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
      const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
  /** Read top-level goods categories using a cached token. */
  async categories(call: Call, token: string): Promise<QueryAttempt<ListRootGoodsCategoriesOutput>> {
    const body = Buffer.from(JSON.stringify({ access_token: token, parent_id: 0 }));
    const exchange = await this.request(call, `${this.baseUrl}/xe.goods.category.list/2.0.0`, 'POST', body);
    const issue = classify(exchange, false);
    if (issue) return issue;
    const data = decode(exchange.response?.body);
    if (!isRecord(data) || typeof data.code !== 'number' || !Number.isInteger(data.code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.code !== 0) {
      if (data.code === 2008) this.token = '';
      return providerCode(data.code, true);
    }
    if (!Array.isArray(data.data) || data.data.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const categories: NonNullable<ListRootGoodsCategoriesOutput['categories']> = [];
    for (const item of data.data) {
      if (!isRecord(item) || typeof item.id !== 'number' || !Number.isSafeInteger(item.id) || item.id <= 0 || typeof item.title !== 'string' || !item.title.trim() || item.title.length > 512 || (item.has_children !== 0 && item.has_children !== 1) || item.parent_id !== 0) return branch('invalidResponse', 'PROTOCOL');
      categories.push({ id: item.id, title: item.title, hasChildren: item.has_children === 1 });
    }
    return newQueryBranch('listed', { categories });
  }
}
/** Read only top-level goods categories from the authorized store. */
export class ListRootGoodsCategoriesOperation implements Query<ListRootGoodsCategoriesInput, ListRootGoodsCategoriesOutput> {
  /** Bind one validated client. */ constructor(private readonly client: XiaoetechClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListRootGoodsCategoriesDefinition; }
  /** Obtain a token and read root goods categories. */
  async invoke(call: Call): Promise<QueryAttempt<ListRootGoodsCategoriesOutput>> {
    const credentials = await this.client.resolve(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const token = await this.client.accessToken(call, credentials.appId, credentials.clientId, credentials.secretKey);
    return typeof token === 'string' ? this.client.categories(call, token) : token;
  }
}
function validSecret(value: string): boolean { return typeof value === 'string' && Boolean(value) && value.length <= 512 && /^[!-~]+$/.test(value); }
function decode(body: Uint8Array | undefined): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return null; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classify(exchange: Exchange, isToken: boolean): QueryAttempt<ListRootGoodsCategoriesOutput> | null {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const { status } = exchange.response;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', isToken ? 0 : exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', isToken ? 'AUTHENTICATION' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  return null;
}
function providerCode(code: number, isCategory: boolean): QueryAttempt<ListRootGoodsCategoriesOutput> {
  if (code === 1001) return retry('AVAILABILITY');
  if (code === 1002) return branch('invalidResponse', 'PROTOCOL');
  if (code === 2053 || code === 2054) return retry('RATE_LIMIT');
  if (code === 2008 && isCategory) return retry('AUTHENTICATION');
  if ([2001, 2002, 2003, 2004, 2005, 2006, 2008, 2020].includes(code)) return branch('providerRejected', 'AUTHENTICATION');
  if (code === 2017 || code === 2051) return branch('providerRejected', 'AUTHORIZATION');
  return branch('providerRejected', 'PROVIDER_REJECTION');
}
function failure(kind: Failure['kind']): Failure { return { provider: 'xiaoetech', operation: 'listRootGoodsCategories', kind, message: 'XiaoeTech category request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListRootGoodsCategoriesOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListRootGoodsCategoriesOutput> { return newQueryRetry(failure(kind), delay); }
