// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DescribeDomainDefinition } from './generated/definitions.js';
import type { Config, Credentials, DescribeDomainInput, DomainDetail } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface BaishanCloudClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve BaishanCloud credentials for each CDN domain query. */
export class BaishanCloudClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting BaishanCloud. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BaishanCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('BaishanCloud configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://cdn.api.baishan.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('BaishanCloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the CDN domain detail Query. */ describeDomain(): DescribeDomainOperation { return new DescribeDomainOperation(this); }

  /** Send one bounded CDN domain request. */
  async getDomain(call: Call, domain: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); token = credential.token instanceof SecretString ? credential.token.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 512 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ domains: domain, token });
    const request: ProviderRequest = { url: this.baseURL + '/v2/domain?' + query.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) || parseRetryAfter(header(response.headers, 'x-retry-after')) };
  }
}

/** Describe one CDN domain without origin or cache configuration. */
export class DescribeDomainOperation implements Query<DescribeDomainInput, DomainDetail> {
  /** Bind the provider client. */ constructor(private readonly client: BaishanCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return DescribeDomainDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: DescribeDomainInput): Promise<QueryAttempt<DomainDetail>> {
    if (!input || typeof input.domain !== 'string') return branch('defect', 'VALIDATION');
    const domain = input.domain.trim().toLowerCase();
    if (domain.length > 253 || !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$/.test(domain)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getDomain(call, domain);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire as { code?: unknown; data?: unknown };
    if (typeof result.code !== 'number' || !Number.isInteger(result.code)) return branch('invalidResponse', 'PROTOCOL');
    if (result.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(result.data) || result.data.length > 1) return branch('invalidResponse', 'PROTOCOL');
    if (result.data.length === 0) return branch('notFound', 'NOT_FOUND');
    const detail = result.data[0] as Partial<DomainDetail>;
    if (!detail || typeof detail.id !== 'string' || !detail.id || typeof detail.domain !== 'string' || detail.domain.toLowerCase() !== domain || typeof detail.type !== 'string' || !detail.type || typeof detail.status !== 'string' || !detail.status || typeof detail.cname !== 'string' || !detail.cname) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { id: detail.id, domain: detail.domain, type: detail.type, status: detail.status, cname: detail.cname });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'baishancloud', operation: 'describeDomain', kind, message: 'BaishanCloud domain query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<DomainDetail> { return newQueryBranch(name, { id: '', domain: '', type: '', status: '', cname: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<DomainDetail> { return newQueryRetry(failure(kind), delay); }
