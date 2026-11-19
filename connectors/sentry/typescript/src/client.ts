// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { isCredentialMaterialError, type Config, type Credentials } from './generated/models.js';
import { GetIssueOperation, ListIssuesOperation, UpdateIssueStatusOperation } from './operations.js';

const defaultBaseURL = 'https://sentry.io/api/0';
const defaultMaxResponseBytes = 2_097_152;
const defaultTimeoutMs = 15_000;

/** HTTP sender injectable for deterministic provider tests. */
export type SentryRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Dependencies for deterministic provider tests. */
export interface SentryClientOptions {
  /** Sender; omit to use the SDK's bounded, no-redirect transport. */
  send?: SentryRequestSender;
  /** Clock used to parse Retry-After. */
  now?: () => Date;
}
/** Classified outcome of one provider exchange. */
export type SentryExchange = { body: Uint8Array } | { branch: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect'; failure: Failure; retryAfterMs: number };

/** Immutable Sentry HTTP settings with call-time credential resolution. */
export class SentryClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: SentryRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Sentry. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SentryClientOptions = {}) {
    if (!credentials) throw new TypeError('Sentry credential provider is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('Sentry timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Sentry maxResponseBytes is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the issue list query. */ listIssues(): ListIssuesOperation { return new ListIssuesOperation(this); }
  /** Bind the issue detail query. */ getIssue(): GetIssueOperation { return new GetIssueOperation(this); }
  /** Bind the absolute status mutation. */ updateIssueStatus(): UpdateIssueStatusOperation { return new UpdateIssueStatusOperation(this); }

  /** Send one request and classify Go-compatible provider failures. */
  async exchange(call: Call, operation: string, method: 'GET' | 'PUT', path: string, body?: Uint8Array, hasNotFoundBranch = false): Promise<SentryExchange> {
    let resolved: Credentials;
    try {
      resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
    } catch (error) {
      if (isTerminalCredentialError(error)) return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials or request are invalid');
      return this.failure(operation, 'retry', 'AVAILABILITY', 'connection credentials could not be read, so nothing was sent');
    }
    if (!(resolved?.apiKey instanceof SecretString)) return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials or request are invalid');
    const token = resolved.apiKey.reveal();
    if (!token.trim() || !isHeaderSafeCredential(token)) return this.failure(operation, 'defect', 'AUTHENTICATION', 'connection credentials or request are invalid');
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseURL + path, method,
        headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', 'User-Agent': 'superdurable-sentry/0.1', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status;
        const retryAfter = parseRetryAfter(header({ status: status ?? 0, headers: error.headers ?? {}, body: new Uint8Array() }, 'retry-after'), this.now());
        if (status === 429) return this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the request', retryAfter);
        if (status === 408 || (status !== undefined && status >= 500)) return this.failure(operation, 'retry', 'AVAILABILITY', 'provider is temporarily unavailable', retryAfter);
        if (status === 404 && hasNotFoundBranch) return this.failure(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found');
        if (status !== undefined && (method === 'GET' ? status !== 200 : ![200, 201, 202].includes(status))) return this.rejectedOrUnknown(operation, method, status);
        if (method === 'PUT') return this.failure(operation, 'retry', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
        return this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      }
      return this.failure(operation, 'retry', 'AVAILABILITY', 'provider request failed');
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (status === 429) return this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the request', retryAfter);
    if (status === 408 || status >= 500) return this.failure(operation, 'retry', 'AVAILABILITY', 'provider is temporarily unavailable', retryAfter);
    if (status === 404 && hasNotFoundBranch) return this.failure(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found');
    if (method === 'GET' ? status !== 200 : ![200, 201, 202].includes(status)) return this.rejectedOrUnknown(operation, method, status);
    return { body: response.body };
  }

  /** Select providerRejected for a definite refusal; the safe PUT retries every other answer, including 409. */
  private rejectedOrUnknown(operation: string, method: 'GET' | 'PUT', status: number): SentryExchange {
    if (method === 'PUT' && !definiteRefusalStatuses.has(status)) return this.failure(operation, 'retry', 'AVAILABILITY', `provider answered HTTP ${status}, so the outcome is unknown`);
    return this.failure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`);
  }

  private failure(operation: string, branch: Exclude<SentryExchange, { body: Uint8Array }>['branch'], kind: Failure['kind'], message: string, retryAfterMs = 0): SentryExchange {
    return { branch, failure: { kind, provider: 'sentry', operation, message }, retryAfterMs };
  }
}

/** Report a credential resolution failure that no retry fixes; every other error is transient. */
function isTerminalCredentialError(error: unknown): boolean {
  return error instanceof ReauthorizationRequiredError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || error instanceof ObjectNotFoundError
    || isCredentialMaterialError(error);
}

const definiteRefusalStatuses: ReadonlySet<number> = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}
