// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchProductsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ProductSearchItem, ProductSearchPage, SearchProductsInput } from './generated/models.js';

const emptyOutput: ProductSearchPage = { pageNumber: 0, total: 0, products: [] };
/** Optional transport and signing clock for deterministic tests. */
export interface OneboundClientOptions { /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; /** Signature clock. */ now?: () => Date; }
/** Search public 1688 products through Onebound. */
export class OneboundClient {
  private readonly baseUrl: string; private readonly timeoutMs: number; private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>; private readonly now: () => Date;
  /** Validate immutable request configuration. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OneboundClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Onebound configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://api-gw.onebound.cn';
    if (!/^https:\/\/[^/?#]+$/.test(this.baseUrl)) throw new TypeError('Onebound base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 15_000; this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Onebound request limits are invalid');
    this.send = options.send ?? sendProviderRequest; this.now = options.now ?? (() => new Date());
  }
  /** Bind the product-search Query. */ searchProducts(): SearchProductsOperation { return new SearchProductsOperation(this); }
  /** Resolve credentials and dispatch one signed search. */
  async request(call: Call, input: SearchProductsInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; requestedPage: number }> {
    if (!input || typeof input.keyword !== 'string' || !input.keyword || [...input.keyword].length > 100 || input.keyword.trim() !== input.keyword || /[\p{Cc}&=?#]/u.test(input.keyword) || !Number.isSafeInteger(input.pageNumber) || input.pageNumber < 0 || input.pageNumber > 100_000) return { error: 'input', retryAfterMs: 0, requestedPage: 0 };
    const requestedPage = input.pageNumber || 1;
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0, requestedPage }; }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    const apiSecret = credentials.apiSecret instanceof SecretString ? credentials.apiSecret.reveal() : '';
    if (![apiKey, apiSecret].every(value => !!value && value.length <= 2048 && /^[!-~]+$/.test(value))) return { error: 'credential', retryAfterMs: 0, requestedPage };
    const dateLine = new Date(this.now().getTime() + 8 * 3_600_000).toISOString().slice(0, 19).replace('T', ' ');
    const parameters = { api_name: 'item_search', key: apiKey, q: input.keyword, page: String(requestedPage), page_size: '40', dateline: dateLine };
    const secret = apiSecret.length === 32 ? apiSecret : createHash('md5').update(apiSecret).digest('hex');
    const payload = Object.entries(parameters).sort(([left], [right]) => left.localeCompare(right)).filter(([, value]) => value && !value.startsWith('@')).map(([key, value]) => `${key}=${value}`).join('&');
    const signature = createHash('md5').update(payload + secret).digest('hex');
    const queryValues = { key: apiKey, q: input.keyword, page: String(requestedPage), page_size: "40", dateline: dateLine };
    const url = `${this.baseUrl}/1688/item_search/?${new URLSearchParams({ ...queryValues, sign: signature }).toString()}`;
    const request: ProviderRequest = { url, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0, requestedPage };
      return { error: 'transport', retryAfterMs: 0, requestedPage };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? ''), requestedPage };
  }
}
/** Classify Onebound search outcomes. */
export class SearchProductsOperation implements Query<SearchProductsInput, ProductSearchPage> {
  /** Bind one validated client. */ constructor(private readonly client: OneboundClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchProductsDefinition; }
  /** Search one page and expose only public product metadata. */
  async invoke(call: Call, input: SearchProductsInput): Promise<QueryAttempt<ProductSearchPage>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.error_code !== 'string' || !wire.error_code) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.error_code;
    if (code === '2000') return newQueryBranch('listed', { pageNumber: exchange.requestedPage, total: 0, products: [] });
    if (code === '0000') {
      const items = wire.items;
      if (!isRecord(items) || typeof items.page !== 'string' || !/^\d+$/.test(items.page) || Number(items.page) < 1 || !Number.isSafeInteger(items.total_results) || (items.total_results as number) < 0 || !Array.isArray(items.item) || items.item.length > 40) return branch('invalidResponse', 'PROTOCOL');
      const products: ProductSearchItem[] = [];
      for (const item of items.item) {
        if (!isRecord(item) || !['num_iid', 'title', 'price', 'detail_url'].every(field => typeof item[field] === 'string' && item[field])) return branch('invalidResponse', 'PROTOCOL');
        products.push({ productID: item.num_iid as string, title: item.title as string, price: item.price as string, detailURL: item.detail_url as string });
      }
      return newQueryBranch('listed', { pageNumber: Number(items.page), total: items.total_results as number, products });
    }
    if (['4000', '4001', '4002', '4009', '4017', '5000'].includes(code)) return retry('AVAILABILITY');
    if (code === '4008') return retry('RATE_LIMIT');
    if (['4004', '4005'].includes(code)) return branch('providerRejected', 'AUTHENTICATION');
    if (['4006', '4007', '4012', '4013', '4016'].includes(code)) return branch('providerRejected', 'AUTHORIZATION');
    return branch('providerRejected', 'PROVIDER_REJECTION');
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'onebound', operation: 'searchProducts', kind, message: 'Onebound provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ProductSearchPage> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ProductSearchPage> { return newQueryRetry(failure(kind), delay); }
