// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchDomainAssetsDefinition } from './generated/definitions.js';
import type { Config, Credentials, SearchDomainAssetsInput, SearchDomainAssetsOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface FofaClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptyResult: SearchDomainAssetsOutput = { total: 0, assets: [] };

/** Resolve FOFA credentials for each public domain search. */
export class FofaClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting FOFA. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FofaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('FOFA configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('FOFA request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only domain asset Query. */ searchDomainAssets(): SearchDomainAssetsOperation { return new SearchDomainAssetsOperation(this); }

  /** Send one bounded first-page FOFA request without logging the query key. */
  async search(call: Call, domain: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('https://fofa.info/api/v1/search/all');
    url.searchParams.set('fields', 'host,ip,port');
    url.searchParams.set('key', apiKey);
    url.searchParams.set('page', '1');
    url.searchParams.set('qbase64', Buffer.from('domain="' + domain + '"').toString('base64'));
    url.searchParams.set('r_type', 'json');
    url.searchParams.set('size', '5');
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Search indexed public hosts for one exact DNS domain. */
export class SearchDomainAssetsOperation implements Query<SearchDomainAssetsInput, SearchDomainAssetsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FofaClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchDomainAssetsDefinition; }
  /** Classify one provider response with the Go connector contract. */
  async invoke(call: Call, input: SearchDomainAssetsInput): Promise<QueryAttempt<SearchDomainAssetsOutput>> {
    const domain = input?.domain;
    if (typeof domain !== 'string' || !isValidDomain(domain)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.search(call, domain);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire as { error?: unknown; size?: unknown; results?: unknown };
    if (typeof result.error !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (result.error) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Number.isInteger(result.size) || (result.size as number) < 0 || !Array.isArray(result.results) || result.results.length > 5) return branch('invalidResponse', 'PROTOCOL');
    const assets: SearchDomainAssetsOutput['assets'] = [];
    for (const entry of result.results) {
      if (!Array.isArray(entry) || entry.length !== 3 || typeof entry[0] !== 'string' || !entry[0] || entry[0].length > 2048 || typeof entry[1] !== 'string' || !isIP(entry[1]) || typeof entry[2] !== 'string' || !/^[0-9]+$/.test(entry[2])) return branch('invalidResponse', 'PROTOCOL');
      const port = Number(entry[2]);
      if (!Number.isInteger(port) || port < 1 || port > 65535) return branch('invalidResponse', 'PROTOCOL');
      assets.push({ host: entry[0], ipAddress: entry[1], port });
    }
    return newQueryBranch('found', { total: result.size as number, assets });
  }
}

function isValidDomain(domain: string): boolean { return domain.length > 0 && domain.length <= 253 && domain.includes('.') && domain === domain.toLowerCase() && !domain.endsWith('.') && domain.split('.').every(label => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fofa', operation: 'searchDomainAssets', kind, message: 'FOFA domain asset search failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SearchDomainAssetsOutput> { return newQueryBranch(name, emptyResult, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SearchDomainAssetsOutput> { return newQueryRetry(failure(kind), delay); }
