// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSiteTDKDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetSiteTDKInput, SiteTDK } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface ChinazClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Chinaz credentials for each public site metadata query. */
export class ChinazClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Chinaz. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ChinazClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Chinaz configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://apidatav2.chinaz.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Chinaz request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the public site TDK Query. */ getSiteTDK(): GetSiteTDKOperation { return new GetSiteTDKOperation(this); }

  /** Send one bounded multipart site TDK request. */
  async getTDK(call: Call, domain: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 2048 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const boundary = 'dex-' + randomBytes(16).toString('hex');
    const form = '--' + boundary + '\r\nContent-Disposition: form-data; name="key"\r\n\r\n' + apiKey + '\r\n--' + boundary + '\r\nContent-Disposition: form-data; name="domain"\r\n\r\n' + domain + '\r\n--' + boundary + '--\r\n';
    const request: ProviderRequest = { url: this.baseURL + '/single/tdk', method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'multipart/form-data; boundary=' + boundary }, body: new TextEncoder().encode(form), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read public site title, keywords, description, and IP address. */
export class GetSiteTDKOperation implements Query<GetSiteTDKInput, SiteTDK> {
  /** Bind the provider client. */ constructor(private readonly client: ChinazClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetSiteTDKDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: GetSiteTDKInput): Promise<QueryAttempt<SiteTDK>> {
    if (!input || typeof input.domain !== 'string' || !validDomain(input.domain)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getTDK(call, input.domain);
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
    const response = wire as { StateCode?: unknown; Result?: unknown };
    if (typeof response.StateCode !== 'number' || !Number.isInteger(response.StateCode)) return branch('invalidResponse', 'PROTOCOL');
    const state = response.StateCode;
    if (state === 10025) return branch('notFound', 'PROVIDER_REJECTION');
    if ([10011, 10012, 10013, 10026].includes(state)) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if ([-1, 10020, 10024, 10029, 10034].includes(state)) return retry('AVAILABILITY', exchange.retryAfterMs);
    if ([10001, 10003, 10004, 10009].includes(state)) return branch('providerRejected', 'AUTHENTICATION');
    if ([10002, 10005, 10008, 10022, 10027, 10032].includes(state)) return branch('providerRejected', 'AUTHORIZATION');
    if (state !== 1) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!response.Result || typeof response.Result !== 'object' || Array.isArray(response.Result)) return branch('invalidResponse', 'PROTOCOL');
    const result = response.Result as { Title?: unknown; Keywords?: unknown; Description?: unknown; IP?: unknown };
    if ([result.Title, result.Keywords, result.Description, result.IP].some(value => value !== undefined && typeof value !== 'string')) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { domain: input.domain, title: (result.Title as string | undefined) ?? '', keywords: (result.Keywords as string | undefined) ?? '', description: (result.Description as string | undefined) ?? '', ipAddress: (result.IP as string | undefined) ?? '' });
  }
}

function validDomain(domain: string): boolean { return domain.length >= 3 && domain.length <= 253 && domain === domain.toLowerCase() && domain.split('.').length >= 2 && domain.split('.').every(label => label.length >= 1 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'chinaz', operation: 'getSiteTDK', kind, message: 'Chinaz site TDK query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SiteTDK> { return newQueryBranch(name, { domain: '', title: '', keywords: '', description: '', ipAddress: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SiteTDK> { return newQueryRetry(failure(kind), delay); }
