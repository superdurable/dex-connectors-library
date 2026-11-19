// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { QueryVectorsOperation, UpsertVectorsOperation } from './operations.js';

/** Injectable bounded provider request sender. */
export type PineconeRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional constructor dependencies for deterministic provider tests. */
export interface PineconeClientOptions {
  /** Override the production provider transport. */
  send?: PineconeRequestSender;
  /** Override the clock used for Retry-After. */
  now?: () => Date;
}
/** One classified failed exchange. */
export interface PineconeExchangeFailure {
  /** Retry or terminal branch. */ disposition: 'retry' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Secret-safe Dex failure. */ failure: Failure;
  /** Provider-directed retry delay. */ retryAfterMs: number;
}
/** Bounded response or classified failure. */
export type PineconeExchange = { body: unknown; rawBody: Uint8Array } | { outcome: PineconeExchangeFailure };

/** Pinecone data-plane client with call-time credential resolution. */
export class PineconeClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: PineconeRequestSender;
  private readonly now: () => Date;

  /** Validate local configuration without contacting Pinecone. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: PineconeClientOptions = {}) {
    if (!credentials) throw new TypeError('Pinecone credential provider is required');
    this.baseUrl = validateBaseURL(config.baseUrl).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Pinecone timeoutMs must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Pinecone maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the upsert mutation. */ upsertVectors(): UpsertVectorsOperation { return new UpsertVectorsOperation(this); }
  /** Bind the vector query. */ queryVectors(): QueryVectorsOperation { return new QueryVectorsOperation(this); }

  /** Send one Pinecone request and classify its bounded response. */
  async exchange(call: Call, operation: string, path: string, input: unknown): Promise<PineconeExchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('invalid Pinecone credential');
      token = credentials.apiKey.reveal();
    } catch { return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable') }; }
    if (!token || !isHeaderSafeCredential(token)) return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'connection credential is invalid') };
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify(input), 'utf8'); }
    catch { return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', 'request could not be built') }; }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseUrl + path, method: 'POST',
        headers: { Accept: 'application/json', 'User-Agent': 'superdurable-pinecone/0.1', 'Api-Key': token, 'X-Pinecone-Api-Version': '2025-04', 'Content-Type': 'application/json' },
        body, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status === undefined || error.status === 200) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      } else return { outcome: this.failure('retry', operation, 'AVAILABILITY', 'provider request failed') };
    }
    const status = response.status;
    if (status === 429 || status === 408 || status === 409 || status >= 500) {
      const kind = status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY';
      const retryAfterMs = parseRetryAfter(this.header(response, 'retry-after'), this.now());
      return { outcome: this.failure('retry', operation, kind, 'provider is temporarily unavailable', retryAfterMs) };
    }
    if (status !== 200) {
      const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return { outcome: this.failure('providerRejected', operation, kind, `provider rejected the request with HTTP ${status}`) };
    }
    try { return { body: JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown, rawBody: response.body }; }
    catch { return { outcome: this.failure('invalidResponse', operation, 'PROTOCOL', 'provider returned malformed JSON') }; }
  }

  private header(response: ProviderResponse, name: string): string {
    const value = response.headers[name];
    return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
  }

  private failure(disposition: PineconeExchangeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0): PineconeExchangeFailure {
    return { disposition, failure: { kind, provider: 'pinecone', operation, message }, retryAfterMs };
  }
}
