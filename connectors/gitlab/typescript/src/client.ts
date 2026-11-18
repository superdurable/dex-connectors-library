// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { isCredentialMaterialError, type Config, type Credentials } from './generated/models.js';
import { GetProjectOperation, ListIssuesOperation, UpdateIssueLabelsOperation } from './operations.js';

const defaultBaseUrl = 'https://gitlab.com/api/v4';
const defaultMaxResponseBytes = 2_097_152;
const defaultTimeoutMs = 15_000;

/** Injectable bounded HTTP sender. */
export type GitLabRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock for deterministic provider tests. */
export interface GitLabClientOptions {
  /** Override the production HTTP sender. */ send?: GitLabRequestSender;
  /** Override the clock used for Retry-After dates. */ now?: () => Date;
}
/** Classified response from one GitLab exchange. */
export type GitLabExchange = { response: ProviderResponse } | { branch: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect'; failure: Failure; retryAfterMs: number };

/** Validated GitLab client with credentials resolved for each call. */
export class GitLabClient {
  private readonly baseUrl: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: GitLabRequestSender;
  private readonly now: () => Date;

  /** Validate local configuration without contacting GitLab. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: GitLabClientOptions = {}) {
    if (!credentials) throw new TypeError('GitLab credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('maxResponseBytes must be positive');
    if (!Number.isFinite(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('timeoutMs must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the project query. */ getProject() { return new GetProjectOperation(this); }
  /** Bind the issue page query. */ listIssues() { return new ListIssuesOperation(this); }
  /** Bind the absolute label replacement mutation. */ updateIssueLabels() { return new UpdateIssueLabelsOperation(this); }

  /** Resolve credentials, send one request, and classify the provider response. */
  async exchange(call: Call, operation: string, method: 'GET' | 'PUT', path: string, body?: string): Promise<GitLabExchange> {
    let credentials: Credentials;
    try {
      credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
    } catch (error) {
      if (isTerminalCredentialError(error)) return this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable');
      return this.failure('retry', operation, 'AVAILABILITY', 'connection credentials could not be read, so nothing was sent');
    }
    if (!(credentials?.accessToken instanceof SecretString)) return this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are invalid');
    const token = credentials.accessToken.reveal();
    if (!token || !isHeaderSafeCredential(token)) return this.failure('defect', operation, 'AUTHENTICATION', 'connection credentials are invalid');
    try {
      const response = await this.send({
        url: this.baseUrl + path, method,
        headers: { 'PRIVATE-TOKEN': token, Accept: 'application/json', ...(body !== undefined ? { 'Content-Type': 'application/x-www-form-urlencoded' } : {}) },
        ...(body !== undefined ? { body: Buffer.from(body, 'utf8') } : {}),
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
      const statusOutcome = this.classifyStatus(operation, method, response.status, response.headers);
      if (statusOutcome) return statusOutcome;
      return { response };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const statusOutcome = this.classifyStatus(operation, method, error.status, error.headers);
        if (statusOutcome) return statusOutcome;
        if (method === 'PUT') return this.failure('retry', operation, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
        return this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
      }
      return this.failure('retry', operation, 'AVAILABILITY', 'GitLab request failed');
    }
  }

  private classifyStatus(operation: string, method: 'GET' | 'PUT', status: number | undefined, headers: ProviderResponse['headers'] | undefined): GitLabExchange | undefined {
    if (status === undefined || status === 200) return undefined;
    if (status === 429 || status === 408 || status === 409 || status >= 500) {
      return this.failure('retry', operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', `GitLab returned HTTP ${status}`, parseRetryAfter(header(headers, 'retry-after'), this.now()));
    }
    // Replacing the complete label set is safe to repeat, so the PUT retries unknown answers.
    if (method === 'PUT' && !definiteRefusalStatuses.has(status)) return this.failure('retry', operation, 'AVAILABILITY', `GitLab answered HTTP ${status}, so the outcome is unknown`);
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', `GitLab returned HTTP ${status}`);
    const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return this.failure('providerRejected', operation, kind, `GitLab returned HTTP ${status}`);
  }

  private failure(branch: Exclude<GitLabExchange, { response: ProviderResponse }>['branch'], operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0): GitLabExchange {
    return { branch, failure: { kind, provider: 'gitlab', operation, message }, retryAfterMs };
  }
}

/** Report a credential resolution failure that no retry fixes; every other error is transient. */
function isTerminalCredentialError(error: unknown): boolean {
  return error instanceof ReauthorizationRequiredError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || error instanceof ObjectNotFoundError
    || isCredentialMaterialError(error);
}

const definiteRefusalStatuses: ReadonlySet<number> = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

function header(headers: ProviderResponse['headers'] | undefined, name: string): string {
  const value = headers?.[name];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}

/** Encode one Go url.PathEscape segment, preserving its RFC 3986 path subdelimiters. */
export function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3A/g, ':').replace(/%3D/g, '=').replace(/%40/g, '@').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
}
