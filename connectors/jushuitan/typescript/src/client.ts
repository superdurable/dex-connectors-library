// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListShopsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListShopsInput, ListShopsOutput, Shop } from './generated/models.js';

const emptyOutput: ListShopsOutput = { pageIndex: 0, pageSize: 0, hasNext: false, shops: [] };
/** Optional transport and signing clock for deterministic tests. */
export interface JushuitanClientOptions { /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; /** Signature clock. */ now?: () => Date; }
/** Query enabled ERP shops through the signed Jushuitan API. */
export class JushuitanClient {
  private readonly timeoutMs: number; private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>; private readonly now: () => Date;
  /** Validate immutable request limits. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JushuitanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Jushuitan configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000; this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Jushuitan request limits are invalid');
    this.send = options.send ?? sendProviderRequest; this.now = options.now ?? (() => new Date());
  }
  /** Bind the shop-list Query. */ listShops(): ListShopsOperation { return new ListShopsOperation(this); }
  /** Resolve credentials and dispatch one signed request. */
  async request(call: Call, input: ListShopsInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    if (!input || !Number.isSafeInteger(input.pageIndex) || input.pageIndex < 1 || !Number.isSafeInteger(input.pageSize) || input.pageSize < 1 || input.pageSize > 100) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    const accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (![appKey, appSecret, accessToken].every(value => !!value && value.length <= 2048 && value.trim() === value)) return { error: 'credential', retryAfterMs: 0 };
    const form = new URLSearchParams({ access_token: accessToken, app_key: appKey, biz: JSON.stringify({ page_index: input.pageIndex, page_size: input.pageSize }), charset: 'utf-8', timestamp: Math.floor(this.now().getTime() / 1000).toString(), version: '2' });
    const text = appSecret + [...form.entries()].sort(([left], [right]) => left.localeCompare(right)).filter(([, value]) => value).map(([key, value]) => key + value).join('');
    form.set('sign', createHash('md5').update(text).digest('hex'));
    const request: ProviderRequest = { url: 'https://openapi.jushuitan.com/open/shops/query', method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify one Jushuitan shop page. */
export class ListShopsOperation implements Query<ListShopsInput, ListShopsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: JushuitanClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListShopsDefinition; }
  /** Read and validate one shop page. */
  async invoke(call: Call, input: ListShopsInput): Promise<QueryAttempt<ListShopsOutput>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 199 || wire.code === 200) return retry('RATE_LIMIT');
    if (wire.code === 150) return retry('AVAILABILITY');
    if (wire.code !== 0) return branch('providerRejected', wire.code === 100 ? 'AUTHENTICATION' : wire.code === 110 || wire.code === 190 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    const page = wire.data;
    if (!isRecord(page) || page.page_index !== input.pageIndex || !Number.isSafeInteger(page.page_size) || (page.page_size as number) < 1 || (page.page_size as number) > 100 || typeof page.has_next !== 'boolean' || !Array.isArray(page.datas)) return branch('invalidResponse', 'PROTOCOL');
    const shops: Shop[] = [];
    for (const item of page.datas) {
      if (!isRecord(item) || !Number.isSafeInteger(item.shop_id) || (item.shop_id as number) <= 0 || typeof item.shop_name !== 'string' || !item.shop_name || (item.shop_site !== undefined && typeof item.shop_site !== 'string')) return branch('invalidResponse', 'PROTOCOL');
      shops.push({ shopId: item.shop_id as number, name: item.shop_name, site: typeof item.shop_site === 'string' ? item.shop_site : '' });
    }
    return newQueryBranch('found', { pageIndex: input.pageIndex, pageSize: page.page_size as number, hasNext: page.has_next, shops });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'jushuitan', operation: 'listShops', kind, message: 'Jushuitan provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListShopsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListShopsOutput> { return newQueryRetry(failure(kind), delay); }
