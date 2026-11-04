// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetCurrentWeatherOperation, GetForecastOperation } from './operations.js';

const defaultBaseURL = 'https://api.openweathermap.org';
const defaultTimeoutMs = 15_000;
const defaultMaxResponseBytes = 1_048_576;

/** Injectable bounded provider HTTP sender. */
export type OpenWeatherMapRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Provider dependencies, with production HTTP as the default. */
export interface OpenWeatherMapClientOptions {
  /** Substitute an HTTP sender for deterministic provider replay. */
  send?: OpenWeatherMapRequestSender;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}

/** Validated OpenWeatherMap client with call-time credential resolution. */
export class OpenWeatherMapClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: OpenWeatherMapRequestSender;
  private readonly now: () => Date;
  private readonly credentials: CredentialProvider<Credentials>;

  /** Validate configuration without contacting the provider. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: OpenWeatherMapClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OpenWeatherMap configuration and credential source are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('OpenWeatherMap timeout and response limit must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.credentials = credentials;
  }

  /** Bind current weather to this client. */
  getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }
  /** Bind the five-day forecast to this client. */
  getForecast(): GetForecastOperation { return new GetForecastOperation(this); }

  /** Send one provider GET and classify the Go operation outcomes. */
  async query<Output>(call: Call, operation: string, path: string, query: URLSearchParams, empty: Output, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey.reveal();
      if (!isHeaderSafeCredential(apiKey)) throw new TypeError('invalid API key');
    } catch {
      return failed(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    }
    query.set('appid', apiKey);
    query.sort();
    const request: ProviderRequest = {
      url: `${this.baseURL}${path}?${query.toString()}`, method: 'GET', headers: { Accept: 'application/json' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed'));
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      isTooLarge = true;
    }
    const retryAfterMs = parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now());
    if (response.status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the request'), retryAfterMs);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is temporarily unavailable'), retryAfterMs);
    if (response.status === 404) return failed(operation, 'notFound', empty, 'NOT_FOUND', 'provider did not find the resource');
    if (response.status !== 200) return failed(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${response.status}`);
    if (isTooLarge) return failed(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
    try { return newQueryBranch('found', decode(response.body)); }
    catch { return failed(operation, 'invalidResponse', empty, 'PROTOCOL', 'provider returned an invalid response'); }
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'openweathermap', operation, message };
}
function failed<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind, message));
}
