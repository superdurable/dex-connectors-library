// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newQueryBranch, newQueryRetry } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetStatsOperation, ListTemplatesOperation } from './operations.js';

/** Injectable HTTP sender for deterministic provider tests. */
export type SendGridRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional dependencies for provider tests. */
export interface SendGridClientOptions { /** Custom sender. */ send?: SendGridRequestSender; /** Clock for Retry-After. */ now?: () => Date }

/** Validated SendGrid client resolving the current key for every attempt. */
export class SendGridClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: SendGridRequestSender;
  private readonly now: () => Date;

  /** Validate local configuration without making a provider request. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SendGridClientOptions = {}) {
    if (!credentials) throw new TypeError('SendGrid credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.sendgrid.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('SendGrid timeout and maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the statistics query. */ getStats(): GetStatsOperation { return new GetStatsOperation(this); }
  /** Bind the template listing query. */ listTemplates(): ListTemplatesOperation { return new ListTemplatesOperation(this); }

  /** Perform one provider attempt and classify its transport and status. */
  async query<Output>(call: Call, operation: string, branch: string, path: string, parameters: URLSearchParams, empty: Output, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    const failure = (kind: Failure['kind'], message: string): Failure => ({ kind, provider: 'sendgrid', operation, message });
    const failed = (selected: string, kind: Failure['kind'], message: string): QueryAttempt<Output> => newQueryBranch(selected, empty, {}, failure(kind, message));
    let key: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiKey instanceof SecretString)) throw new TypeError('invalid SendGrid key');
      key = resolved.apiKey.reveal();
      if (!isHeaderSafeCredential(key)) throw new TypeError('invalid SendGrid key');
    } catch { return failed('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const now = this.now;
    const maxResponseBytes = this.maxResponseBytes;
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: `${this.baseUrl}${path}?${encodeQueryParameters(parameters)}`, method: 'GET',
        headers: { Authorization: `Bearer ${key}`, Accept: 'application/json' },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === 'https://api.sendgrid.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return failed('invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers?.['retry-after']);
      return newQueryRetry(failure('AVAILABILITY', 'provider request failed'));
    }
    return classifyStatus(response.status, response.headers['retry-after'], response.body);

    function classifyStatus(status: number, retryHeader: string | string[] | undefined, body?: Uint8Array): QueryAttempt<Output> {
      const retryAfterMs = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '', now());
      if (status === 429) return newQueryRetry(failure('RATE_LIMIT', 'provider rate limited the request'), retryAfterMs);
      if (status === 408 || status >= 500) return newQueryRetry(failure('AVAILABILITY', 'provider is temporarily unavailable'), retryAfterMs);
      if (status !== 200) return failed('providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`);
      if (!body || body.length > maxResponseBytes) return failed('invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      try { return newQueryBranch(branch, decode(body), {}); }
      catch { return failed('invalidResponse', 'PROTOCOL', 'provider returned an invalid response'); }
    }
  }
}

function encodeQueryParameters(parameters: URLSearchParams): string {
  return [...parameters.entries()]
    .sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
    .map(([key, value]) => `${goQueryEscape(key)}=${goQueryEscape(value)}`)
    .join('&');
}

function goQueryEscape(value: string): string {
  let encoded = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    if ((byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) ||
      (byte >= 48 && byte <= 57) || byte === 45 || byte === 46 || byte === 95 || byte === 126) {
      encoded += String.fromCharCode(byte);
    } else if (byte === 32) {
      encoded += '+';
    } else {
      encoded += `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
    }
  }
  return encoded;
}
