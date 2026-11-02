// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { ListPullRequestsOperation, ListRepositoriesOperation } from './operations.js';

const defaultBaseURL = 'https://api.bitbucket.org/2.0';
const defaultMaxResponseBytes = 2_097_152;
const defaultTimeoutMs = 15_000;

/** Injectable HTTP sender for deterministic Bitbucket provider tests. */
export type BitbucketRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Dependencies for provider tests. */
export interface BitbucketClientOptions {
  /** Sender; defaults to the SDK's bounded, no redirect transport. */
  send?: BitbucketRequestSender;
  /** Clock for Retry-After parsing. */
  now?: () => Date;
}

/** One provider exchange classified against the Go connector contract. */
export type ReadResult = { response: ProviderResponse } | { branch: 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect'; failure: Failure } | { retry: true; failure: Failure; retryAfterMs: number };

/** Bitbucket client with immutable settings and call-time credential resolution. */
export class BitbucketClient {
  private readonly baseURL: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: BitbucketRequestSender;
  private readonly now: () => Date;

  /** Validate settings without contacting Bitbucket. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: BitbucketClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Bitbucket configuration and credential provider are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) {
      throw new TypeError('timeout and maxResponseBytes must be positive');
    }
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the repository listing Query. */
  listRepositories(): ListRepositoriesOperation { return new ListRepositoriesOperation(this); }
  /** Bind the pull request listing Query. */
  listPullRequests(): ListPullRequestsOperation { return new ListPullRequestsOperation(this); }

  /** Resolve credentials, send one bounded GET, and classify the exchange. */
  async read(call: Call, operation: string, path: string): Promise<ReadResult> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.accessToken instanceof SecretString)) throw new TypeError('invalid token');
      token = resolved.accessToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid token');
    } catch {
      return this.branch(operation, 'defect', 'AUTHENTICATION', 'Bitbucket credentials are unavailable');
    }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseURL + path, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) return this.branch(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      return this.retry(operation, 'AVAILABILITY', 'provider read failed');
    }
    const status = response.status;
    if (status === 429) return this.retry(operation, 'RATE_LIMIT', 'provider rate limited the read', this.retryAfter(response));
    if (status === 408 || status >= 500) return this.retry(operation, 'AVAILABILITY', 'provider read is temporarily unavailable', this.retryAfter(response));
    if (status === 404) return this.branch(operation, 'notFound', 'PROVIDER_REJECTION', 'resource was not found');
    if (status === 401) return this.branch(operation, 'providerRejected', 'AUTHENTICATION', 'provider rejected credentials');
    if (status === 403) return this.branch(operation, 'providerRejected', 'AUTHORIZATION', 'provider denied access');
    if (status !== 200) return this.branch(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected read with HTTP ${status}`);
    return { response };
  }

  private retryAfter(response: ProviderResponse): number {
    const value = response.headers['retry-after'];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
  }

  private branch(operation: string, branch: 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect', kind: Failure['kind'], message: string): ReadResult {
    return { branch, failure: { kind, provider: 'bitbucket', operation, message } };
  }

  private retry(operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0): ReadResult {
    return { retry: true, failure: { kind, provider: 'bitbucket', operation, message }, retryAfterMs };
  }
}
