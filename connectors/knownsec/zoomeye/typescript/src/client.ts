// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountQuotaDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountQuotaInput, GetAccountQuotaOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface ZoomEyeClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve ZoomEye credentials for each account quota query. */
export class ZoomEyeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting ZoomEye. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZoomEyeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ZoomEye configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.zoomeye.ai');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('ZoomEye request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the account quota Query. */ getAccountQuota(): GetAccountQuotaOperation { return new GetAccountQuotaOperation(this); }

  /** Send one bounded account quota request. */
  async getQuota(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 512 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: this.baseURL + '/v2/userinfo', method: 'POST', headers: { Accept: 'application/json', 'API-KEY': apiKey }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read subscription and remaining point counts without personal profile fields. */
export class GetAccountQuotaOperation implements Query<GetAccountQuotaInput, GetAccountQuotaOutput> {
  /** Bind the provider client. */ constructor(private readonly client: ZoomEyeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetAccountQuotaDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: GetAccountQuotaInput): Promise<QueryAttempt<GetAccountQuotaOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getQuota(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire as { code?: unknown; data?: unknown };
    if (typeof result.code !== 'number' || !Number.isInteger(result.code)) return branch('invalidResponse', 'PROTOCOL');
    if (result.code !== 60000) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!result.data || typeof result.data !== 'object' || Array.isArray(result.data)) return branch('invalidResponse', 'PROTOCOL');
    const subscription = (result.data as { subscription?: unknown }).subscription;
    if (!subscription || typeof subscription !== 'object' || Array.isArray(subscription)) return branch('invalidResponse', 'PROTOCOL');
    const quota = subscription as { plan?: unknown; points?: unknown; zoomeye_points?: unknown };
    if (typeof quota.plan !== 'string' || !quota.plan || typeof quota.points !== 'string' || !/^\d{1,30}$/.test(quota.points) || typeof quota.zoomeye_points !== 'string' || !/^\d{1,30}$/.test(quota.zoomeye_points)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('fetched', { plan: quota.plan, generalPoints: quota.points, zoomeyePoints: quota.zoomeye_points });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'zoomeye', operation: 'getAccountQuota', kind, message: 'ZoomEye account quota query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountQuotaOutput> { return newQueryBranch(name, { plan: '', generalPoints: '', zoomeyePoints: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountQuotaOutput> { return newQueryRetry(failure(kind), delay); }
