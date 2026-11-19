// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CountClassObjectsDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, CountClassObjectsInput, CountClassObjectsOutput } from './generated/models.js';

/** Provider transport override for tests and approved gateways. */
export interface BmobClientOptions { /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Bmob REST client for count-only class queries. */
export class BmobClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the app-specific API origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: BmobClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Bmob configuration and credential source are required');
    const url = new URL(config.baseUrl);
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Bmob API URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Bmob request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the countClassObjects Query. */ countClassObjects(): CountClassObjectsOperation { return new CountClassObjectsOperation(this); }

  /** Send one count-only request with credentials resolved per call. */
  async requestCount(call: Call, className: string): Promise<{ response?: ProviderResponse; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean }> {
    let applicationId: string;
    let restApiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      applicationId = credentials.applicationId instanceof SecretString ? credentials.applicationId.reveal() : '';
      restApiKey = credentials.restApiKey instanceof SecretString ? credentials.restApiKey.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (!validCredential(applicationId) || !validCredential(restApiKey)) return { credentialFailed: true };
    const url = new URL(`${this.baseURL}/1/classes/${className}`);
    url.searchParams.set('count', '1'); url.searchParams.set('limit', '0');
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET', headers: { Accept: 'application/json', 'X-Bmob-Application-Id': applicationId, 'X-Bmob-REST-API-Key': restApiKey },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }
}

/** Read the number of objects in one named Bmob class. */
export class CountClassObjectsOperation implements Query<CountClassObjectsInput, CountClassObjectsOutput> {
  /** Bind the client. */ constructor(private readonly client: BmobClient) {}
  /** Return the generated Query definition. */ definition() { return CountClassObjectsDefinition; }
  /** Classify one count request without reading object contents. */
  async invoke(call: Call, input: CountClassObjectsInput): Promise<QueryAttempt<CountClassObjectsOutput>> {
    if (!validClassName(input.className)) return terminal('defect', 'VALIDATION', 'class name is invalid');
    const result = await this.client.requestCount(call, input.className);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (result.transportFailed || !result.response) return newQueryRetry(failure('AVAILABILITY', 'Bmob request failed'));
    const response = result.response;
    const retryAfter = retryDelay(response);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Bmob rate limited the request'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Bmob is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Bmob response exceeded the configured limit');
    if (response.status !== 200) {
      const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : [400, 404].includes(response.status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return terminal('providerRejected', kind, `Bmob rejected the request with HTTP ${response.status}`);
    }
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Bmob returned an invalid count response'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'Bmob returned an invalid count response');
    const fields = value as Record<string, unknown>;
    if (!Number.isSafeInteger(fields.count) || (fields.count as number) < 0 || !Array.isArray(fields.results) || fields.results.length !== 0)
      return terminal('invalidResponse', 'PROTOCOL', 'Bmob returned an invalid count response');
    return newQueryBranch('counted', { count: fields.count as number }, zeroReceipt());
  }
}

function validClassName(value: unknown): value is string { return typeof value === 'string' && /^[A-Za-z][A-Za-z0-9_]{0,127}$/.test(value); }
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'bmob', operation: 'countClassObjects', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CountClassObjectsOutput> { return newQueryBranch(branch, { count: 0 }, zeroReceipt(), failure(kind, message)); }
function retryDelay(response: ProviderResponse): number {
  const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '') || 0;
}
