// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchPoliciesDefinition } from './generated/definitions.js';
import type { Config, Credentials, PolicyCard, SearchPoliciesInput, SearchPoliciesOutput } from './generated/models.js';

const emptyOutput: SearchPoliciesOutput = { policies: [] };

/** Optional sender for isolated Go conformance replay. */
export interface EnlvonClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Search Enlvon electricity policy cards without retaining full documents. */
export class EnlvonClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Enlvon. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: EnlvonClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Enlvon configuration and credentials are required');
    const baseUrl = config.baseUrl || 'https://enlvon.com';
    let parsed: URL;
    try { parsed = new URL(baseUrl); } catch { throw new TypeError('Enlvon production origin is invalid'); }
    if (parsed.protocol !== 'https:' || parsed.hostname !== 'enlvon.com' || parsed.pathname !== '/' || parsed.username || parsed.password || parsed.search || parsed.hash || baseUrl.endsWith('/')) throw new TypeError('Enlvon production origin is invalid');
    this.baseUrl = baseUrl;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Enlvon request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the policy search Query. */ searchPolicies(): SearchPoliciesOperation { return new SearchPoliciesOperation(this); }

  /** Send one bounded policy search request. */
  async readPolicies(call: Call, input: SearchPoliciesInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey.startsWith('enl_') || apiKey.length > 2048 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ q: input.query });
    if (input.topK) query.set('top_k', String(input.topK));
    const request: ProviderRequest = { url: this.baseUrl + '/api/search?' + query.toString(), method: 'GET', headers: { 'X-API-Key': apiKey, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read bounded source metadata for matching electricity policies. */
export class SearchPoliciesOperation implements Query<SearchPoliciesInput, SearchPoliciesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: EnlvonClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchPoliciesDefinition; }
  /** Classify one search according to the Go connector contract. */
  async invoke(call: Call, input: SearchPoliciesInput): Promise<QueryAttempt<SearchPoliciesOutput>> {
    if (!input || typeof input.query !== 'string' || !input.query || Buffer.byteLength(input.query, 'utf8') > 1024 || (input.topK !== undefined && (!Number.isInteger(input.topK) || input.topK < 0 || input.topK > 50))) return branch('defect', 'VALIDATION');
    const exchange = await this.client.readPolicies(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.count) || !Array.isArray(wire.results) || wire.count !== wire.results.length || wire.results.length > 50) return branch('invalidResponse', 'PROTOCOL');
    const policies: PolicyCard[] = [];
    for (const item of wire.results) {
      if (!isRecord(item) || typeof item['标题'] !== 'string' || !item['标题'] || Buffer.byteLength(item['标题'], 'utf8') > 2048 || !bounded(item['日期'], 128) || !bounded(item['来源'], 128) || !bounded(item['原文链接'], 4096)) return branch('invalidResponse', 'PROTOCOL');
      policies.push({ title: item['标题'], date: (item['日期'] as string | undefined) ?? '', source: (item['来源'] as string | undefined) ?? '', originalURL: (item['原文链接'] as string | undefined) ?? '' });
    }
    return newQueryBranch('found', { policies }, {});
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function bounded(value: unknown, maxBytes: number): boolean { return value === undefined || (typeof value === 'string' && Buffer.byteLength(value, 'utf8') <= maxBytes); }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'enlvon', operation: 'searchPolicies', kind, message: 'Enlvon policy search failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SearchPoliciesOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SearchPoliciesOutput> { return newQueryRetry(failure(kind), delay); }
