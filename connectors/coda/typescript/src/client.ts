// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { GetRowOperation, ListRowsOperation } from './operations.js';

const defaultBaseUrl = 'https://coda.io/apis/v1';
const defaultMaxResponseBytes = 2_097_152;
const defaultTimeoutMs = 15_000;

/** HTTP sender injected by tests or supplied by the connector SDK. */
export type CodaRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional provider transport dependency. */
export interface CodaClientOptions {
  /** Override the bounded SDK HTTP sender for deterministic provider tests. */
  send?: CodaRequestSender;
}

/** Classified response from one Coda exchange. */
export interface CodaExchangeFailure {
  /** Retry or terminal branch. */
  branch: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Secret-safe failure. */
  failure: Failure;
  /** Provider-directed retry delay in milliseconds. */
  retryAfterMs: number;
}

/** A successful response or a classified exchange failure. */
export type CodaExchangeResult = { body: Uint8Array } | { outcome: CodaExchangeFailure };

/** Validated Coda HTTP client with credentials resolved for each call. */
export class CodaClient {
  private readonly baseUrl: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: CodaRequestSender;

  /** Construct without sending a provider request. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: CodaClientOptions = {}) {
    if (!credentials) throw new TypeError('Coda credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Coda maxResponseBytes must be positive');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Coda timeoutMs must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the list query. */
  listRows(): ListRowsOperation { return new ListRowsOperation(this); }
  /** Bind the single-row query. */
  getRow(): GetRowOperation { return new GetRowOperation(this); }

  /** Send one authenticated GET request and classify its response. */
  async exchange(call: Call, operation: string, path: string, query: URLSearchParams, hasNotFound: boolean): Promise<CodaExchangeResult> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiToken instanceof SecretString)) throw new TypeError('invalid Coda token');
      token = resolved.apiToken.reveal();
      if (!token || !isHeaderSafeCredential(token)) throw new TypeError('invalid Coda token');
    } catch {
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable') };
    }
    const address = this.baseUrl + path + (query.size ? `?${query.toString()}` : '');
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: address, method: 'GET',
        headers: { Accept: 'application/json', 'User-Agent': 'superdurable-coda/0.1', Authorization: `Bearer ${token}` },
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      return { outcome: this.failure('retry', operation, 'AVAILABILITY', 'provider request failed') };
    }
    const status = response.status;
    if (status === 429 || status === 408 || status === 409 || status >= 500) {
      const kind = status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY';
      const retryAfter = response.headers['retry-after'];
      return { outcome: this.failure('retry', operation, kind, 'provider is temporarily unavailable', parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '')) };
    }
    if (status === 404 && hasNotFound) return { outcome: this.failure('notFound', operation, 'NOT_FOUND', 'resource was not found') };
    if (status !== 200) {
      const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return { outcome: this.failure('providerRejected', operation, kind, `provider rejected the request with HTTP ${status}`) };
    }
    try { JSON.parse(Buffer.from(response.body).toString('utf8')); }
    catch { return { outcome: this.failure('invalidResponse', operation, 'PROTOCOL', 'provider returned malformed JSON') }; }
    return { body: response.body };
  }

  private failure(branch: CodaExchangeFailure['branch'], operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0): CodaExchangeFailure {
    return { branch, failure: { kind, provider: 'coda', operation, message }, retryAfterMs };
  }
}
