// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { GetMovieOperation, SearchMoviesOperation } from './operations.js';

/** HTTP transport dependency for provider tests. */
export type TMDBRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional provider transport. */
export interface TMDBClientOptions { /** Send one HTTP request. */ send?: TMDBRequestSender }

/** One classified unsuccessful exchange. */
export interface ExchangeFailure {
  /** Outcome classification. */ disposition: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Secret-safe failure. */ failure: Failure;
  /** Provider-directed retry delay in milliseconds. */ retryAfterMs: number;
}

/** Provider response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** Immutable TMDB connection settings with call-time credential resolution. */
export class TMDBClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: TMDBRequestSender;

  /** Validate configuration without making a network call. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TMDBClientOptions = {}) {
    if (!credentials) throw new TypeError('TMDB credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.themoviedb.org/3');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('TMDB timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('TMDB maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the movie search Query. */ searchMovies(): SearchMoviesOperation { return new SearchMoviesOperation(this); }
  /** Bind the movie lookup Query. */ getMovie(): GetMovieOperation { return new GetMovieOperation(this); }

  /** Resolve credentials and classify one GET attempt. */
  async exchange(call: Call, operation: string, path: string, hasNotFoundBranch: boolean): Promise<ExchangeResult> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('invalid credential');
      token = credentials.apiKey.reveal();
      if (!token.trim() || !isHeaderSafeCredential(token)) throw new TypeError('invalid credential');
    } catch { return { outcome: this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials or request are invalid') }; }
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.baseUrl + path, method: 'GET',
        headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', 'User-Agent': 'superdurable-tmdb/0.1' },
        maxResponseBytes: Math.max(this.maxResponseBytes, 16 * 1024 * 1024), timeoutMs: this.timeoutMs,
        addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const responseStatus = error.status;
        if (responseStatus === 429) return { outcome: this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the request', parseRetryAfter(header(error.headers, 'retry-after'))) };
        if (responseStatus === 408 || responseStatus >= 500) return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider is temporarily unavailable', parseRetryAfter(header(error.headers, 'retry-after'))) };
        if (responseStatus === 404 && hasNotFoundBranch) return { outcome: this.failure(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found') };
        if (responseStatus !== 200) return { outcome: this.failure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${responseStatus}`) };
        return { outcome: this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      }
      return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider request failed') };
    }
    const status = response.status;
    if (status === 429) return { outcome: this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the request', parseRetryAfter(header(response.headers, 'retry-after'))) };
    if (status === 408 || status >= 500) return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider is temporarily unavailable', parseRetryAfter(header(response.headers, 'retry-after'))) };
    if (status === 404 && hasNotFoundBranch) return { outcome: this.failure(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found') };
    if (status !== 200) return { outcome: this.failure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { outcome: this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
    return { response };
  }

  private failure(operation: string, disposition: ExchangeFailure['disposition'], kind: Failure['kind'], message: string, retryAfterMs = 0): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'tmdb', operation, message }, retryAfterMs };
  }
}

function header(headers: NodeJS.Dict<string | string[] | number | undefined> | undefined, name: string): string {
  const value = headers?.[name];
  return Array.isArray(value) ? value[0] ?? '' : String(value ?? '');
}
