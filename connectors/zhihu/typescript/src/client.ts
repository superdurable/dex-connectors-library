// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListQuotasDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListQuotasInput, QuotaPage } from './generated/models.js';

/** Optional provider sender and clock for deterministic tests. */
export interface ZhihuClientOptions {
  /** Send one bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the current time for signed or retry requests. */ now?: () => Date;
}

/** Read Zhihu provider data through Dex Queries. */
export class ZhihuClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable provider settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZhihuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Zhihu configuration and credentials are required');
    this.baseUrl = (config.baseUrl ?? 'https://developer.zhihu.com').replace(/\/$/, '');
    const url = new URL(this.baseUrl);
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname))) || url.search || url.hash || false) throw new TypeError('Zhihu base URL is invalid');
    this.timeoutMs = config.timeoutMs ?? 15000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Zhihu request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the provider Query. */ listQuotas(): ListQuotasOperation { return new ListQuotasOperation(this); }
  /** Execute one provider request and classify its outcome. */
  async invoke(call: Call, _input: ListQuotasInput): Promise<QueryAttempt<QuotaPage>> {
    void _input;
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const secret = credentials.accessSecret instanceof SecretString ? credentials.accessSecret.reveal() : '';
    if (!isHeaderSafe(secret) || secret.length > 2048 || secret.toLowerCase().startsWith('bearer ')) return branch('defect', 'AUTHENTICATION');
    const request: ProviderRequest = { url: this.baseUrl + '/api/v1/quota', method: 'GET', headers: { Accept: 'application/json', Authorization: 'Bearer ' + secret, 'X-Request-Timestamp': String(Math.floor(this.now().getTime() / 1000)) }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const { status, body } = response;
    const retryHeader = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '', this.now());
    if (status === 429) return retry('RATE_LIMIT', delay);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
    if (body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isSafeInteger(wire.Code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.Code === 30001) return retry('RATE_LIMIT', delay);
    if (wire.Code === 90001) return retry('AVAILABILITY', delay);
    if (wire.Code === 20001) return branch('providerRejected', 'AUTHENTICATION');
    if (wire.Code === 10001) return branch('providerRejected', 'VALIDATION');
    if (wire.Code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(wire.Data) || wire.Data.length > 100) return branch('invalidResponse', 'PROTOCOL');
    const quotas: QuotaPage['quotas'] = [];
    for (const item of wire.Data) {
      if (!isRecord(item) || typeof item.APIID !== 'string' || !item.APIID || typeof item.APIName !== 'string' || !item.APIName || ![item.TotalQuota,item.TotalUsed,item.RemainingQuota].every(value => Number.isSafeInteger(value) && (value as number) >= 0)) return branch('invalidResponse', 'PROTOCOL');
      quotas.push({ apiId: item.APIID, apiName: item.APIName, totalQuota: item.TotalQuota as number, totalUsed: item.TotalUsed as number, remainingQuota: item.RemainingQuota as number });
    }
    return newQueryBranch('listed', { quotas });
  }
}

/** Query adapter backed by a validated Zhihu client. */
export class ListQuotasOperation implements Query<ListQuotasInput, QuotaPage> {
  /** Bind the provider client. */ constructor(private readonly client: ZhihuClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListQuotasDefinition; }
  /** Invoke the provider Query. */ invoke(call: Call, input: ListQuotasInput): Promise<QueryAttempt<QuotaPage>> { return this.client.invoke(call, input); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isHeaderSafe(value: string): boolean { return !!value && value.trim() === value && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zhihu', operation: 'listQuotas', kind, message: 'Zhihu query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<QuotaPage> { return newQueryBranch(name, { quotas: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<QuotaPage> { return newQueryRetry(failure(kind), delay); }
