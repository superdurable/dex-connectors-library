// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { GetMessageDetailsOperation, ListOutboundMessagesOperation } from './operations.js';

const defaultBaseURL = 'https://api.postmarkapp.com';
const defaultMaxResponseBytes = 2_097_152;
const defaultTimeoutMs = 15_000;

/** Injected Postmark transport for deterministic provider tests. */
export type PostmarkRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional dependencies owned by a Postmark client. */
export interface PostmarkClientOptions {
  /** Provider sender; omitted in production. */
  send?: PostmarkRequestSender;
  /** Clock used for Retry-After dates. */
  now?: () => Date;
}

/** Validated Postmark HTTP client with call-time credentials. */
export class PostmarkClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: PostmarkRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Postmark. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: PostmarkClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Postmark configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) {
      throw new TypeError('Postmark timeout and response size must be nonnegative integers');
    }
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the outbound page query. */
  listOutboundMessages(): ListOutboundMessagesOperation { return new ListOutboundMessagesOperation(this); }

  /** Bind the outbound detail query. */
  getMessageDetails(): GetMessageDetailsOperation { return new GetMessageDetailsOperation(this); }

  /** Resolve credentials and send one GET; no secret enters a returned failure. */
  async request(call: Call, path: string): Promise<PostmarkResponse> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('invalid credentials');
      token = credentials.apiKey.reveal();
      if (!token.trim() || !isHeaderSafeCredential(token)) throw new TypeError('invalid credentials');
    } catch { return { kind: 'credentials' }; }
    try {
      const response = await this.send({
        url: this.baseURL + path, method: 'GET',
        headers: { 'X-Postmark-Server-Token': token, Accept: 'application/json', 'User-Agent': 'superdurable-postmark/0.1' },
        maxResponseBytes: Math.max(this.maxResponseBytes, 65_536),
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      });
      return { kind: 'response', response };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined) return { kind: 'oversized', status: error.status, headers: error.headers ?? {} };
        return { kind: 'oversized', status: 200, headers: {} };
      }
      return { kind: 'transport' };
    }
  }

  /** Classify a GET response like the Go connector, preserving only safe metadata. */
  classify<Output>(operation: string, received: PostmarkResponse, zero: Output, successBranch: string, notFoundBranch: string, decode: (body: Uint8Array) => Output): QueryAttempt<Output> {
    const failure = (kind: Failure['kind'], message: string): Failure => ({ kind, provider: 'postmark', operation, message });
    const branch = (id: string, kind: Failure['kind'], message: string): QueryAttempt<Output> => ({ kind: 'branch', branch: id, value: zero, receipt: {}, failure: failure(kind, message) });
    if (received.kind === 'credentials') return branch('defect', 'AUTHENTICATION', 'connection credentials or request are invalid');
    if (received.kind === 'transport') return { kind: 'retry', failure: failure('AVAILABILITY', 'provider request failed'), retryAfterMs: 0 };
    const status = received.kind === 'response' ? received.response.status : received.status;
    const headers = received.kind === 'response' ? received.response.headers : received.headers;
    const header = (name: string): string => {
      const value = headers[name.toLowerCase()];
      return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
    };
    if (status === 429) return { kind: 'retry', failure: failure('RATE_LIMIT', 'provider rate limited the request'), retryAfterMs: parseRetryAfter(header('retry-after'), this.now()) };
    if (status === 408 || status >= 500) return { kind: 'retry', failure: failure('AVAILABILITY', 'provider is temporarily unavailable'), retryAfterMs: parseRetryAfter(header('retry-after'), this.now()) };
    if (operation === 'getMessageDetails' && status === 422) {
      let errorCode = 0;
      if (received.kind === 'response') {
        try { const parsed = JSON.parse(Buffer.from(received.response.body).toString('utf8')) as { ErrorCode?: unknown }; if (typeof parsed?.ErrorCode === 'number') errorCode = parsed.ErrorCode; }
        catch { /* Malformed provider error bodies do not change the terminal rejection. */ }
      }
      if (header('x-pm-apierrorcode') === '701' || errorCode === 701) return branch(notFoundBranch, 'NOT_FOUND', 'Postmark message was not found');
      return branch('providerRejected', 'PROVIDER_REJECTION', 'Postmark rejected the message request');
    }
    if (status === 404 && notFoundBranch) return branch(notFoundBranch, 'NOT_FOUND', 'provider resource was not found');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`);
    if (received.kind === 'oversized' || received.response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
    try { return { kind: 'branch', branch: successBranch, value: decode(received.response.body), receipt: {} }; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'provider response is incomplete or malformed'); }
  }
}

type PostmarkResponse = { kind: 'credentials' } | { kind: 'transport' } | { kind: 'response'; response: ProviderResponse } | { kind: 'oversized'; status: number; headers: ProviderResponse['headers'] };
