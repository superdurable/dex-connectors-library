// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchWebDefinition } from './generated/definitions.js';
import type { Config, Credentials, SearchWebInput, SearchWebOutput, WebResult } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface BochaClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Bocha credentials for each billed web search. */
export class BochaClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Bocha. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BochaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Bocha configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Bocha request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the public web search Query. */ searchWeb(): SearchWebOperation { return new SearchWebOperation(this); }

  /** Send one bounded web search request. */
  async getSearchResults(call: Call, query: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: 'https://api.bochaai.com/v1/web-search', method: 'POST', headers: { Accept: 'application/json', Authorization: 'Bearer ' + apiKey, 'Content-Type': 'application/json' }, body: new TextEncoder().encode(JSON.stringify({ query, count: 5, summary: false })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Search public web pages and return at most five results. */
export class SearchWebOperation implements Query<SearchWebInput, SearchWebOutput> {
  /** Bind the provider client. */ constructor(private readonly client: BochaClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchWebDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: SearchWebInput): Promise<QueryAttempt<SearchWebOutput>> {
    if (!input || typeof input.query !== 'string' || !input.query.trim() || [...input.query].length > 256) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getSearchResults(call, input.query);
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
    const result = wire as { code?: unknown; data?: { webPages?: { value?: unknown } } };
    if (typeof result.code !== 'number' || !Number.isInteger(result.code)) return branch('invalidResponse', 'PROTOCOL');
    if (result.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const pages = result.data?.webPages?.value;
    if (!Array.isArray(pages) || pages.length > 50) return branch('invalidResponse', 'PROTOCOL');
    const results: WebResult[] = [];
    for (const page of pages.slice(0, 5)) {
      if (!page || typeof page !== 'object' || Array.isArray(page)) return branch('invalidResponse', 'PROTOCOL');
      const candidate = page as { name?: unknown; url?: unknown; snippet?: unknown };
      if (typeof candidate.name !== 'string' || !candidate.name || Buffer.byteLength(candidate.name) > 500 || typeof candidate.url !== 'string' || typeof candidate.snippet !== 'string' || Buffer.byteLength(candidate.snippet) > 2000) return branch('invalidResponse', 'PROTOCOL');
      let parsed: URL;
      try { parsed = new URL(candidate.url); } catch { return branch('invalidResponse', 'PROTOCOL'); }
      if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password) return branch('invalidResponse', 'PROTOCOL');
      results.push({ title: candidate.name, url: candidate.url, snippet: candidate.snippet });
    }
    return newQueryBranch('found', { results });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'bocha', operation: 'searchWeb', kind, message: 'Bocha web search failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SearchWebOutput> { return newQueryBranch(name, { results: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SearchWebOutput> { return newQueryRetry(failure(kind), delay); }
