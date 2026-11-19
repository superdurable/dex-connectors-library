// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchProductsDefinition } from './generated/definitions.js';
import type { Config, Credentials, Product, ProductPage, SearchProductsInput } from './generated/models.js';

/** Optional provider sender and signature clock for deterministic tests. */
export interface SMZDMClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the signature timestamp. */ now?: () => Date;
}

/** Read the first page of SMZDM product index entries. */
export class SMZDMClient {
  private readonly baseURL: string;
  private readonly requestFrom: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable connection settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SMZDMClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('SMZDM configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://openapi.smzdm.com').replace(/\/$/, '');
    this.requestFrom = config.requestFrom ?? 'dex';
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!validSource(this.requestFrom) || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('SMZDM request configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the product-search Query. */ searchProducts(): SearchProductsOperation { return new SearchProductsOperation(this); }
  /** Resolve credentials and dispatch one signed search request. */
  async request(call: Call, input: SearchProductsInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    if (!input || !validKeyword(input.keyword)) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (!validCredential(appKey) || !validCredential(appSecret)) return { error: 'credential', retryAfterMs: 0 };
    const parameters = { app_key: appKey, c: 'new_wiki', offset: '0', request_from: this.requestFrom, s: input.keyword, size: '20', timestamp: String(Math.floor(this.now().getTime() / 1000)) };
    const message = appSecret + Object.entries(parameters).sort(([left], [right]) => left.localeCompare(right)).filter(([, value]) => value).map(([name, value]) => name + value).join('') + appSecret;
    const sign = createHash('md5').update(message).digest('hex').toUpperCase();
    const query = new URLSearchParams({ ...parameters, sign });
    const request: ProviderRequest = { url: `${this.baseURL}/v1/searchapi/search/list?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
  }
}

/** Classify product-search outcomes. */
export class SearchProductsOperation implements Query<SearchProductsInput, ProductPage> {
  /** Bind the validated client. */ constructor(private readonly client: SMZDMClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchProductsDefinition; }
  /** Read a bounded first page of product entries. */
  async invoke(call: Call, input: SearchProductsInput): Promise<QueryAttempt<ProductPage>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', classifyHTTP(status));
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.status !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!wire.status) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.data) || !Number.isSafeInteger(wire.data.total_num) || (wire.data.total_num as number) < 0 || !Array.isArray(wire.data.rows) || wire.data.rows.length > 20) return branch('invalidResponse', 'PROTOCOL');
    const products: Product[] = [];
    for (const item of wire.data.rows) {
      if (!isRecord(item) || typeof item.title !== 'string' || !item.title || typeof item.pro_price !== 'number' || !Number.isFinite(item.pro_price) || item.pro_price < 0) return branch('invalidResponse', 'PROTOCOL');
      products.push({ title: item.title, priceYuan: item.pro_price });
    }
    return newQueryBranch('listed', { total: wire.data.total_num as number, products });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function validKeyword(value: unknown): value is string { return typeof value === 'string' && !!value && value.trim() === value && [...value].length <= 100 && ![...value].some(character => /\p{Cc}/u.test(character)); }
function validSource(value: string): boolean { return !!value && value.length <= 128 && value.trim() === value && ![...value].some(character => /\p{Cc}/u.test(character)); }
function validCredential(value: string): boolean { return !!value && value.length <= 2048 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function classifyHTTP(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function failure(kind: Failure['kind']): Failure { return { provider: 'smzdm', operation: 'searchProducts', kind, message: 'SMZDM product search failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ProductPage> { return newQueryBranch(name, { total: 0, products: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ProductPage> { return newQueryRetry(failure(kind), delay); }
