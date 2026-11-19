// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DescribeDomainDefinition } from './generated/definitions.js';
import type { Config, Credentials, DescribeDomainInput, DomainDetail } from './generated/models.js';

const emptyDomain = (): DomainDetail => ({ id: '', domain: '', type: '', status: '', cname: '' });

/** Inject the provider sender for local tests. */
export interface BaishanCloudClientOptions {
  /** Provider transport; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read CDN domain metadata with credentials resolved for each call. */
export class BaishanCloudClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the connection without contacting BaishanCloud. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BaishanCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('BaishanCloud configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://cdn.api.baishan.com');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('BaishanCloud request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the describeDomain Query. */ describeDomain(): DescribeDomainOperation { return new DescribeDomainOperation(this); }

  /** Send one bounded domain request. */
  async get(call: Call, domain: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validToken(token)) return { error: 'credential', retryAfterMs: 0 };
    const url = `${this.baseURL}/v2/domain?${new URLSearchParams({ domains: domain, token }).toString()}`;
    const request: ProviderRequest = { url, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after')) || parseRetryAfter(header(response.headers, 'x-retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Describe one BaishanCloud CDN domain. */
export class DescribeDomainOperation implements Query<DescribeDomainInput, DomainDetail> {
  /** Bind the provider client. */ constructor(private readonly client: BaishanCloudClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return DescribeDomainDefinition; }
  /** Classify one domain request according to the Go connector contract. */
  async invoke(call: Call, input: DescribeDomainInput): Promise<QueryAttempt<DomainDetail>> {
    const domain = typeof input?.domain === 'string' ? input.domain.trim().toLowerCase() : '';
    if (domain.length > 253 || !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$/.test(domain)) return branch('defect', 'VALIDATION', 'BaishanCloud domain name is invalid');
    const exchange = await this.client.get(call, domain);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'BaishanCloud token is unavailable or invalid');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY', 'BaishanCloud domain request failed');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', 'BaishanCloud rate limited the request', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', 'BaishanCloud is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'BaishanCloud response exceeded the configured limit');
    if (status !== 200) return branch('providerRejected', classifyHTTP(status), `BaishanCloud rejected the query with HTTP ${status}`);
    let wire: unknown;
    try { wire = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL', 'BaishanCloud returned an invalid domain response'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL', 'BaishanCloud returned an invalid domain response');
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `BaishanCloud rejected the query with code ${wire.code}`);
    if (!Array.isArray(wire.data) || wire.data.length > 1) return branch('invalidResponse', 'PROTOCOL', 'BaishanCloud returned an invalid domain response');
    if (wire.data.length === 0) return branch('notFound', 'NOT_FOUND', 'BaishanCloud domain was not found');
    const detail = wire.data[0];
    if (!isObject(detail) || typeof detail.id !== 'string' || !detail.id || typeof detail.domain !== 'string' || detail.domain.toLowerCase() !== domain || typeof detail.type !== 'string' || !detail.type || typeof detail.status !== 'string' || !detail.status || typeof detail.cname !== 'string' || !detail.cname) return branch('invalidResponse', 'PROTOCOL', 'BaishanCloud returned an invalid domain response');
    return newQueryBranch('found', { id: detail.id, domain: detail.domain, type: detail.type, status: detail.status, cname: detail.cname });
  }
}

function validToken(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function classifyHTTP(status: number): Failure['kind'] { if (status === 401) return 'AUTHENTICATION'; if (status === 403) return 'AUTHORIZATION'; if (status === 400 || status === 404) return 'VALIDATION'; return 'PROVIDER_REJECTION'; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'baishancloud', operation: 'describeDomain', kind, message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<DomainDetail> { return newQueryBranch(name, emptyDomain(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<DomainDetail> { return newQueryRetry(failure(kind, message), delay); }
