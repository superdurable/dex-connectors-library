// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProductsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProductsInput, ProductList } from './generated/models.js';

/** Optional provider sender and clock for deterministic tests. */
export interface ZenTaoClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the current time for signed or retry requests. */ now?: () => Date;
}

/** Read ZenTao provider data through Dex Queries. */
export class ZenTaoClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable provider settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZenTaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ZenTao configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl ?? '');
    if (this.baseUrl.endsWith('/api.php') || this.baseUrl.includes('/api.php/')) throw new TypeError('ZenTao requires the installation root without /api.php');
    this.timeoutMs = config.timeoutMs ?? 15000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('ZenTao request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the provider Query. */ listProducts(): ListProductsOperation { return new ListProductsOperation(this); }
  /** Execute one provider request and classify its outcome. */
  async invoke(call: Call, _input: ListProductsInput): Promise<QueryAttempt<ProductList>> {
    void _input;
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const secret = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!isHeaderSafe(secret)) return branch('defect', 'AUTHENTICATION');
    const request: ProviderRequest = { url: this.baseUrl + '/api.php/v1/products', method: 'GET', headers: { Accept: 'application/json', Token: secret }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    let oversized = false;
    try { response = await this.send(request); }
    catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return retry('AVAILABILITY');
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      oversized = true;
    }
    const { status, body } = response;
    const retryHeader = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '', this.now());
    if (status === 429) return retry('RATE_LIMIT', delay);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
    if (oversized || body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isSafeInteger(wire.total) || (wire.total as number) < 0 || !Array.isArray(wire.products) || wire.products.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const products: ProductList['products'] = [];
    for (const item of wire.products) {
      if (!isRecord(item) || !Number.isSafeInteger(item.id) || (item.id as number) <= 0 || typeof item.name !== 'string' || !item.name || (item.code != null && typeof item.code !== 'string') || (typeof item.type !== 'string' || !['normal','branch','platform'].includes(item.type)) || (typeof item.acl !== 'string' || !['open','private'].includes(item.acl))) return branch('invalidResponse', 'PROTOCOL');
      products.push({ id: item.id as number, name: item.name, code: item.code ?? '', type: item.type as string, accessControl: item.acl as string });
    }
    return newQueryBranch('listed', { products, total: wire.total as number });
  }
}

/** Query adapter backed by a validated ZenTao client. */
export class ListProductsOperation implements Query<ListProductsInput, ProductList> {
  /** Bind the provider client. */ constructor(private readonly client: ZenTaoClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListProductsDefinition; }
  /** Invoke the provider Query. */ invoke(call: Call, input: ListProductsInput): Promise<QueryAttempt<ProductList>> { return this.client.invoke(call, input); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isHeaderSafe(value: string): boolean { return !!value && value.trim() === value && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zentao', operation: 'listProducts', kind, message: 'ZenTao query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ProductList> { return newQueryBranch(name, { products: [], total: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ProductList> { return newQueryRetry(failure(kind), delay); }
