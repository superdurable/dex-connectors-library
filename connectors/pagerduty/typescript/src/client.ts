// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetIncidentOperation, ListIncidentsOperation } from './operations.js';

/** Injectable provider sender for deterministic tests. */
export type PagerDutyRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Provider send dependencies. */
export interface PagerDutyClientOptions {
  /** Request sender, defaulting to the SDK transport. */ send?: PagerDutyRequestSender;
  /** Clock for Retry-After parsing. */ now?: () => Date;
}
/** Classified unsuccessful provider call. */
export interface ExchangeFailure {
  /** Retry or terminal disposition. */ disposition: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Safe failure details. */ failure: Failure;
  /** Provider retry delay in milliseconds. */ retryAfterMs: number;
}
/** Successful response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** PagerDuty client with per-attempt credential resolution. */
export class PagerDutyClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: PagerDutyRequestSender;
  private readonly now: () => Date;

  /** Validate settings without contacting PagerDuty. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: PagerDutyClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('PagerDuty configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.pagerduty.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('PagerDuty timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the incident list query. */ listIncidents(): ListIncidentsOperation { return new ListIncidentsOperation(this); }
  /** Bind the incident lookup query. */ getIncident(): GetIncidentOperation { return new GetIncidentOperation(this); }

  /** Send one bounded GET and classify its status. */
  async exchange(call: Call, operation: string, path: string, hasNotFoundBranch: boolean): Promise<ExchangeResult> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('invalid PagerDuty credentials');
      token = credentials.apiKey.reveal();
      if (!token.trim() || !isHeaderSafeCredential(token)) throw new TypeError('invalid PagerDuty token');
    } catch { return { outcome: this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials or request are invalid') }; }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseUrl + path, method: 'GET',
        headers: { Authorization: `Token token=${token}`, Accept: 'application/vnd.pagerduty+json;version=2', 'User-Agent': 'superdurable-pagerduty/0.1' },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === 'https://api.pagerduty.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) return { outcome: this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider request failed') };
    }
    const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (response.status === 429) return { outcome: this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the request', retryAfterMs) };
    if (response.status === 408 || response.status >= 500) return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider is temporarily unavailable', retryAfterMs) };
    if (response.status === 404 && hasNotFoundBranch) return { outcome: this.failure(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found') };
    if (response.status !== 200) return { outcome: this.failure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${response.status}`) };
    return { response };
  }

  private failure(operation: string, disposition: ExchangeFailure['disposition'], kind: Failure['kind'], message: string, retryAfterMs = 0): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'pagerduty', operation, message }, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}
