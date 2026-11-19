// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListStockCompaniesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListStockCompaniesInput, ListStockCompaniesOutput, StockCompany } from './generated/models.js';

const emptyOutput: ListStockCompaniesOutput = { companies: [] };
/** Send a Lixinger request through a caller-owned transport. */
export type CompanyRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface LixingerClientOptions { /** Request sender; production uses the Connector SDK transport. */ send?: CompanyRequestSender; }
/** Resolve Lixinger credentials for each company query. */
export class LixingerClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: CompanyRequestSender;
  /** Validate configuration without contacting Lixinger. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: LixingerClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Lixinger configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://open.lixinger.com').replace(/\/$/, '');
    const parsed = new URL(this.baseUrl);
    if (parsed.hostname !== 'open.lixinger.com' || parsed.protocol !== 'https:' || parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Lixinger base URL must be its documented HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Lixinger timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the stock-company Query. */
  listStockCompanies(): ListStockCompaniesOperation { return new ListStockCompaniesOperation(this); }
}
/** Read basic listing metadata for A-share companies. */
export class ListStockCompaniesOperation implements Query<ListStockCompaniesInput, ListStockCompaniesOutput> {
  /** Bind an authenticated Lixinger client. */
  constructor(private readonly client: LixingerClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListStockCompaniesDefinition; }
  /** Send and classify one company-list request. */
  async invoke(call: Call, input: ListStockCompaniesInput): Promise<QueryAttempt<ListStockCompaniesOutput>> {
    if (!Array.isArray(input.stockCodes) || input.stockCodes.length < 1 || input.stockCodes.length > 100 || input.stockCodes.some(code => !/^[0-9]{6}$/.test(code)) || new Set(input.stockCodes).size !== input.stockCodes.length) return branch('defect', 'VALIDATION', 'stockCodes must contain one to one hundred unique six-digit codes');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Lixinger token is unavailable or invalid'); }
    const token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!token || token.length > 512 || !/^[!-~]+$/.test(token)) return branch('defect', 'AUTHENTICATION', 'Lixinger token is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${this.client.baseUrl}/api/cn/company`, method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: new TextEncoder().encode(JSON.stringify({ stockCodes: input.stockCodes, token })),
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Lixinger temporarily unavailable', getRetryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Lixinger response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Lixinger request failed');
    }
    const delay = getRetryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'Lixinger rate limited the request', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Lixinger temporarily unavailable', delay);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Lixinger rejected the request with HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Lixinger returned invalid company data'); }
    if (!isRecord(document) || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL', 'Lixinger returned invalid company data');
    if (document.code !== 1) return branch('providerRejected', 'PROVIDER_REJECTION', `Lixinger rejected the request, code ${document.code}`);
    if (!Array.isArray(document.data) || document.data.length > input.stockCodes.length) return branch('invalidResponse', 'PROTOCOL', 'Lixinger returned invalid company count');
    const requested = new Set(input.stockCodes);
    const seen = new Set<string>();
    const companies: StockCompany[] = [];
    for (const item of document.data as unknown[]) {
      if (!isRecord(item) || typeof item.stockCode !== 'string' || !requested.has(item.stockCode) || seen.has(item.stockCode) || typeof item.name !== 'string' || !item.name || item.name.length > 512 || !['sh','sz','bj'].includes(String(item.exchange)) || (item.listingStatus !== undefined && (typeof item.listingStatus !== 'string' || item.listingStatus.length > 128))) return branch('invalidResponse', 'PROTOCOL', 'Lixinger returned invalid company metadata');
      seen.add(item.stockCode);
      companies.push({ stockCode: item.stockCode, name: item.name, exchange: item.exchange as string, listingStatus: item.listingStatus as string ?? '' });
    }
    return newQueryBranch('listed', { companies });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function getRetryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'lixinger', operation: 'listStockCompanies', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListStockCompaniesOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListStockCompaniesOutput> { return newQueryRetry(failure(kind, message), delayMs); }
