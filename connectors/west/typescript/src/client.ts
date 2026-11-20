// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListDomainsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListDomainsInput, ListDomainsOutput } from './generated/models.js';

const empty: ListDomainsOutput = { domains: [], page: 0, totalPages: 0, total: 0 };
/** Optional provider transport and clock for deterministic verification. */
export interface WestClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for the reseller token. */ now?: () => Date;
}
/** Read a West.cn reseller domain page using the current credential pair. */
export class WestClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate endpoint and request bounds without contacting West.cn. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WestClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('West configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://api.west.cn').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.pathname !== '/' || endpoint.search || endpoint.hash || endpoint.username || endpoint.password) throw new TypeError('West base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('West request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the domain-page Query. */ listDomains(): ListDomainsOperation { return new ListDomainsOperation(this); }
  /** Sign and read one domain page. */
  async request(call: Call, input: ListDomainsInput): Promise<QueryAttempt<ListDomainsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const username = credentials.username;
    const password = credentials.apiPassword instanceof SecretString ? credentials.apiPassword.reveal() : '';
    if (typeof username !== 'string' || !validPart(username) || !validPart(password)) return branch('defect', 'AUTHENTICATION');
    const page = input.page || 1;
    const limit = input.limit || 20;
    if (!Number.isInteger(page) || page < 1 || page > 1_000_000 || !Number.isInteger(limit) || limit < 1 || limit > 100) return branch('defect', 'VALIDATION');
    const timestamp = String(this.now().getTime());
    const token = createHash('md5').update(`${username}${password}${timestamp}`).digest('hex');
    const query = new URLSearchParams({ act: 'getdomains', username, time: timestamp, token, page: String(page), limit: String(limit) });
    const request: ProviderRequest = { url: `${this.baseUrl}/api/v2/domain/?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('gbk', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || typeof data.result !== 'number' || !Number.isInteger(data.result)) return branch('invalidResponse', 'PROTOCOL');
    if (data.result !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(data.data) || !Array.isArray(data.data.items) || data.data.items.length > limit || !validCount(data.data.pageno) || !validCount(data.data.totalpages) || !validCount(data.data.total)) return branch('invalidResponse', 'PROTOCOL');
    const domains: NonNullable<ListDomainsOutput['domains']> = [];
    for (const item of data.data.items) {
      if (!isRecord(item) || typeof item.domain !== 'string' || !item.domain || item.domain.length > 253 || item.domain.trim() !== item.domain || typeof item.expdate !== 'string' || !item.expdate || item.expdate.length > 64) return branch('invalidResponse', 'PROTOCOL');
      domains.push({ name: item.domain, expiresAt: item.expdate });
    }
    return newQueryBranch('listed', { domains, page: data.data.pageno, totalPages: data.data.totalpages, total: data.data.total });
  }
}
/** Read a single reseller domain page. */
export class ListDomainsOperation implements Query<ListDomainsInput, ListDomainsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: WestClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListDomainsDefinition; }
  /** Invoke one safe provider read. */ invoke(call: Call, input: ListDomainsInput): Promise<QueryAttempt<ListDomainsOutput>> { return this.client.request(call, input); }
}
function validPart(value: string): boolean { return Boolean(value) && value.length <= 512 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 0x20 && character.charCodeAt(0) !== 0x7f); }
function validCount(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'west', operation: 'listDomains', kind, message: 'West domain request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListDomainsOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListDomainsOutput> { return newQueryRetry(failure(kind), delay); }
