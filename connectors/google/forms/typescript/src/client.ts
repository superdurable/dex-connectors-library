// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetFormOperation, GetResponseOperation, ListResponsesOperation } from './operations.js';

const defaultEndpoint = 'https://forms.googleapis.com';
const defaultMaxResponseBytes = 1_048_576;
const defaultPageSize = 50;
const requestTimeoutMs = 25_000;

/** Injectable request sender and clock for deterministic provider tests. */
export interface GoogleFormsClientOptions {
  /** HTTP sender; defaults to the SDK's bounded transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and Retry-After clock. */
  now?: () => Date;
}

/** One failed provider read. */
export interface ReadFailure {
  /** Selected branch, or retry for a repeatable failure. */
  disposition: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Safe failure for the Dex attempt. */
  failure: Failure;
  /** Provider directed delay. */
  retryAfterMs: number;
  /** Correlation data for a terminal provider response. */
  receipt: ReceiptDraft;
}

/** A successful provider response or classified read failure. */
export type ReadResult = { response: ProviderResponse } | { outcome: ReadFailure };

/** Authenticated, bounded Google Forms read client. */
export class GoogleFormsClient {
  readonly maxResponseBytes: number;
  readonly responsePageSize: number;
  private readonly endpoint: string;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Google', auth: Authorization, credentialFields: CredentialFields });

  /** Validate settings and bind host owned credentials. */
  constructor(config: Config, readonly credentials: CredentialSource, options: GoogleFormsClientOptions = {}) {
    if (!credentials) throw new TypeError('Google Forms credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint) + '/v1';
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.responsePageSize = config.responsePageSize || defaultPageSize;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Google Forms response limit must be positive');
    if (!Number.isSafeInteger(this.responsePageSize) || this.responsePageSize < 1 || this.responsePageSize > 1000) throw new TypeError('Google Forms response page size must be from 1 to 1000');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the form query. */ getForm(): GetFormOperation { return new GetFormOperation(this); }
  /** Bind the response listing query. */ listResponses(): ListResponsesOperation { return new ListResponsesOperation(this); }
  /** Bind the response query. */ getResponse(): GetResponseOperation { return new GetResponseOperation(this); }

  /** Produce one receipt for a completed provider exchange. */
  receipt(call: Call, requestId = '', objectId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'google-forms', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(requestId ? { providerRequestId: requestId } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}) };
  }

  /** Resolve credentials, send a GET, and classify the provider status. */
  async read(call: Call, operation: string, path: string): Promise<ReadResult> {
    let token: string;
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credential.accessToken?.reveal() ?? '';
      if (!isHeaderSafeCredential(token) || !['', 'google-oauth', 'workspace-domain-delegation'].includes(credential.authMethodId ?? '')) throw new TypeError('invalid Google Forms credential');
    } catch { return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable') }; }
    let response: ProviderResponse | undefined;
    let isOversized = false;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        response = await this.send({ url: this.endpoint + path, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' },
          addressPolicy: this.endpoint.startsWith(defaultEndpoint) ? 'publicHTTPS' : 'privateNetworkHTTP',
          maxResponseBytes: Math.max(this.maxResponseBytes, 65_536), timeoutMs: requestTimeoutMs, signal: call.context.cancellationSignal });
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
          response = { status: error.status, headers: error.headers, body: new Uint8Array() };
          isOversized = true;
        } else return { outcome: this.failure('retry', operation, 'TRANSPORT', 'provider is unavailable') };
      }
      if (response.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.credentials)) break;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        if (!replacement.accessToken) break;
        token = replacement.accessToken.reveal();
        if (!isHeaderSafeCredential(token)) break;
      } catch { break; }
    }
    if (!response) return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', 'authenticated request retry was exhausted') };
    const requestId = header(response, 'x-goog-request-id') || header(response, 'x-request-id');
    const receipt = this.receipt(call, requestId);
    if (response.status >= 200 && response.status < 300) {
      if (isOversized || response.body.length > this.maxResponseBytes) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit', receipt) };
      return { response };
    }
    let tokens: string[] = [];
    try {
      const value: unknown = JSON.parse(new TextDecoder().decode(response.body));
      if (value && typeof value === 'object') {
        const error = (value as Record<string, unknown>).error;
        if (error && typeof error === 'object') {
          const entry = error as Record<string, unknown>;
          tokens = [entry.status, (Array.isArray(entry.details) && entry.details[0] && typeof entry.details[0] === 'object' ? (entry.details[0] as Record<string, unknown>).reason : undefined),
            (Array.isArray(entry.errors) && entry.errors[0] && typeof entry.errors[0] === 'object' ? (entry.errors[0] as Record<string, unknown>).reason : undefined)].filter((token): token is string => typeof token === 'string');
        }
      }
    } catch { /* An unparseable error body is classified by HTTP status. */ }
    const rateLimited = response.status === 429 || (response.status === 403 && tokens.some(token => ['RATE_LIMIT_EXCEEDED', 'rateLimitExceeded', 'userRateLimitExceeded'].includes(token)));
    const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : rateLimited ? 'RATE_LIMIT' : response.status === 403 ? 'AUTHORIZATION' : response.status === 404 ? 'NOT_FOUND' : response.status === 409 ? 'CONFLICT' : response.status === 408 || response.status >= 500 ? 'AVAILABILITY' : 'PROVIDER_REJECTION';
    if (response.status === 408 || response.status >= 500 || rateLimited) return { outcome: this.failure('retry', operation, kind, 'provider temporarily rejected the request', {}, parseRetryAfter(header(response, 'retry-after'), this.now())) };
    if (response.status === 404) return { outcome: this.failure('notFound', operation, kind, 'form or response was not found or is not visible to the connection', receipt) };
    return { outcome: this.failure('providerRejected', operation, kind, 'provider rejected the request', receipt) };
  }

  private failure(disposition: ReadFailure['disposition'], operation: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, retryAfterMs = 0): ReadFailure {
    return { disposition, failure: { kind, provider: 'google-forms', operation, message }, receipt, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
