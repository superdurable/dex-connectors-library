// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { EmbedOperation, RerankOperation } from './operations.js';

const defaultBaseURL = 'https://api.cohere.com';
const defaultTimeoutMs = 30_000;
const defaultMaxResponseBytes = 4_194_304;

/** Injectable provider sender for deterministic HTTP tests. */
export type CohereRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional HTTP dependencies for a Cohere client. */
export interface CohereClientOptions {
  /** Sender; omit for the SDK's bounded, redirect-safe transport. */
  send?: CohereRequestSender;
  /** Clock used to parse HTTP Retry-After dates. */
  now?: () => Date;
}

/** Immutable Cohere HTTP configuration and call-time credential source. */
export class CohereClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialSource;
  private readonly send: CohereRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Cohere. Zero values use manifest defaults. */
  constructor(config: Config, credentials: CredentialSource, options: CohereClientOptions = {}) {
    if (!config) throw new TypeError('Cohere configuration is required');
    if (!credentials) throw new TypeError('Cohere credential source is required');
    if (config.timeoutMs !== undefined && (!Number.isSafeInteger(config.timeoutMs) || config.timeoutMs < 0)) throw new TypeError('Cohere timeout must be nonnegative');
    if (config.maxResponseBytes !== undefined && (!Number.isSafeInteger(config.maxResponseBytes) || config.maxResponseBytes < 0)) throw new TypeError('Cohere maxResponseBytes must be nonnegative');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind one embedding query. */
  embed(): EmbedOperation { return new EmbedOperation(this); }
  /** Bind one reranking query. */
  rerank(): RerankOperation { return new RerankOperation(this); }

  /** POST one JSON request and classify its provider outcome. */
  async invokeJSON<Output>(call: Call, operation: string, path: string, payload: unknown, successBranch: string, empty: Output, decode: (body: unknown) => Output): Promise<QueryAttempt<Output>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return this.failed(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable'); }
    if (!(credentials.apiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.apiKey.reveal())) {
      return this.failed(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are invalid');
    }
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify(payload), 'utf8'); }
    catch { return this.failed(operation, 'defect', empty, 'VALIDATION', 'input cannot be encoded'); }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseURL + path, method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json', Authorization: `Bearer ${credentials.apiKey.reveal()}` },
        body, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return this.failed(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'Cohere response exceeded configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return this.classifyStatus(operation, error.status, error.headers ?? {}, empty);
      return this.retry(operation, 'AVAILABILITY', 'Cohere request failed');
    }
    if (response.status !== 200) return this.classifyStatus(operation, response.status, response.headers, empty);
    try {
      return newQueryBranch(successBranch, decode(JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown), zeroReceipt());
    } catch {
      return this.failed(operation, 'invalidResponse', empty, 'PROTOCOL', 'Cohere returned unusable JSON');
    }
  }

  private classifyStatus<Output>(operation: string, status: number, headers: Record<string, unknown>, empty: Output): QueryAttempt<Output> {
    const retryAfterHeader = headers['retry-after'] ?? headers['Retry-After'];
    const retryAfter = parseRetryAfter(typeof retryAfterHeader === 'string' ? retryAfterHeader : '', this.now());
    if (status === 429) return this.retry(operation, 'RATE_LIMIT', 'Cohere rate limit', retryAfter);
    if (status === 408 || status === 409 || status >= 500) return this.retry(operation, 'AVAILABILITY', 'Cohere temporarily unavailable', retryAfter);
    if (status === 401 || status === 498) return this.failed(operation, 'providerRejected', empty, 'AUTHENTICATION', 'Cohere rejected the API key');
    if (status === 403) return this.failed(operation, 'providerRejected', empty, 'AUTHORIZATION', 'Cohere denied the request');
    return this.failed(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `Cohere rejected HTTP ${status}`);
  }

  private failed<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
    return newQueryBranch(branch, empty, zeroReceipt(), { kind, provider: 'cohere', operation, message });
  }

  private retry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> {
    return newQueryRetry({ kind, provider: 'cohere', operation, message }, delay);
  }
}
