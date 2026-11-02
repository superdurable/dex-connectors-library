// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { ListMonitorsOperation, QueryMetricsOperation } from './operations.js';

const defaultBaseURL = 'https://api.datadoghq.com';
const defaultTimeoutMs = 15_000;
const defaultMaxResponseBytes = 2_097_152;

/** Injectable bounded HTTP sender for provider tests. */
export type DatadogRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Transport dependency for a Datadog client. */
export interface DatadogClientOptions {
  /** Omit to use the Connector SDK's redirect-safe provider transport. */
  send?: DatadogRequestSender;
  /** Clock used to interpret HTTP Retry-After dates. */
  now?: () => Date;
}

/** Classified result of one provider request. */
export type DatadogExchange =
  | { response: ProviderResponse }
  | { branch: 'defect' | 'providerRejected' | 'invalidResponse'; failure: Failure }
  | { retry: true; failure: Failure; retryAfterMs: number };

/** Datadog HTTP client that resolves keys for every Step attempt. */
export class DatadogClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: DatadogRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without making a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: DatadogClientOptions = {}) {
    if (!credentials) throw new TypeError('Datadog credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Datadog timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Datadog maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the timeseries Query. */
  queryMetrics(): QueryMetricsOperation { return new QueryMetricsOperation(this); }
  /** Bind the monitor-page Query. */
  listMonitors(): ListMonitorsOperation { return new ListMonitorsOperation(this); }

  /** Send one GET, keeping credential errors terminal and transport errors retryable. */
  async exchange(call: Call, operation: string, path: string, query: URLSearchParams): Promise<DatadogExchange> {
    let apiKey: string;
    let applicationKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString) || !(credentials.applicationKey instanceof SecretString)) {
        return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials are invalid');
      }
      apiKey = credentials.apiKey.reveal();
      applicationKey = credentials.applicationKey.reveal();
    } catch {
      return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable');
    }
    if (!apiKey || !applicationKey || !isHeaderSafeCredential(apiKey) || !isHeaderSafeCredential(applicationKey)) {
      return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials are invalid');
    }
    const sortedQuery = [...query.entries()].sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0);
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: `${this.baseURL}${path}?${sortedQuery.map(([key, value]) => `${encodeGoQueryComponent(key)}=${encodeGoQueryComponent(value)}`).join('&')}`, method: 'GET',
        headers: { 'DD-API-KEY': apiKey, 'DD-APPLICATION-KEY': applicationKey, Accept: 'application/json' },
        timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) {
          return this.classifyStatus(operation, error.status, error.headers ?? {})
            ?? this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
        }
        return this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
      }
      return { retry: true, failure: this.failureValue(operation, 'AVAILABILITY', 'Datadog request failed'), retryAfterMs: 0 };
    }
    const statusOutcome = this.classifyStatus(operation, response.status, response.headers);
    return statusOutcome ?? { response };
  }

  private classifyStatus(operation: string, status: number, headers: ProviderResponse['headers']): DatadogExchange | undefined {
    if (status === 429 || status === 408 || status === 409 || status >= 500) {
      const header = headers['retry-after'];
      const retryAfterMs = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '', this.now());
      return { retry: true, failure: this.failureValue(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', `Datadog returned HTTP ${status}`), retryAfterMs };
    }
    if (status !== 200) {
      const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return this.failure(operation, 'providerRejected', kind, `Datadog returned HTTP ${status}`);
    }
    return undefined;
  }

  private failure(operation: string, branch: 'defect' | 'providerRejected' | 'invalidResponse', kind: Failure['kind'], message: string): DatadogExchange {
    return { branch, failure: this.failureValue(operation, kind, message) };
  }

  private failureValue(operation: string, kind: Failure['kind'], message: string): Failure {
    return { kind, provider: 'datadog', operation, message };
  }
}

function encodeGoQueryComponent(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}
