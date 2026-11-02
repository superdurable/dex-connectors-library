// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { AddCommentOperation, CreateTaskOperation, FindMemberByEmailOperation, GetTaskOperation, SearchTasksOperation, UpdateTaskOperation, UpdateTaskTagsOperation } from './operations.js';

const apiBaseURL = 'https://api.clickup.com/api/v2';
const defaultMaxResponseBytes = 4_194_304;

/** Provider request sender, injectable for conformance and provider tests. */
export type ClickUpRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Dependencies for deterministic provider tests. */
export interface ClickUpClientOptions {
  /** Use the SDK transport when omitted. */
  send?: ClickUpRequestSender;
  /** Use the system clock when omitted. */
  now?: () => Date;
  /** Override only with a ClickUp compatible test endpoint. */
  apiBaseURL?: string;
}
/** Outcome of one ClickUp HTTP exchange. */
export type Exchange =
  | { kind: 'succeeded'; response: ProviderResponse }
  | { kind: 'notFound' | 'notSent' | 'rateLimited' | 'unconfirmed' | 'rejected' | 'invalid' | 'defect'; failure: Failure; retryAfterMs: number };

/** Authenticated ClickUp API client; credentials are resolved for each request. */
export class ClickUpClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: ClickUpRequestSender;
  private readonly now: () => Date;

  /** Validate the client configuration without contacting ClickUp. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ClickUpClientOptions = {}) {
    if (!credentials) throw new TypeError('ClickUp credential provider is required');
    this.endpoint = validateBaseURL(options.apiBaseURL ?? apiBaseURL);
    this.maxResponseBytes = config.maxResponseBytes ?? defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('ClickUp maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the Workspace task search. */ searchTasks(): SearchTasksOperation { return new SearchTasksOperation(this); }
  /** Bind the task read. */ getTask(): GetTaskOperation { return new GetTaskOperation(this); }
  /** Bind the task creation. */ createTask(): CreateTaskOperation { return new CreateTaskOperation(this); }
  /** Bind the task update. */ updateTask(): UpdateTaskOperation { return new UpdateTaskOperation(this); }
  /** Bind the task tag update. */ updateTaskTags(): UpdateTaskTagsOperation { return new UpdateTaskTagsOperation(this); }
  /** Bind the comment creation. */ addComment(): AddCommentOperation { return new AddCommentOperation(this); }
  /** Bind the Workspace member lookup. */ findMemberByEmail(): FindMemberByEmailOperation { return new FindMemberByEmailOperation(this); }

  /** Create a secret safe receipt. */
  receipt(call: Call, objectId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'clickup', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}) };
  }

  /** Return the current time for retry and reconciliation decisions. */
  currentTime(): Date { return this.now(); }

  /** Send a bounded API request and classify its response. */
  async exchange(call: Call, operation: string, method: string, path: string, query?: URLSearchParams, payload?: unknown): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiToken instanceof SecretString)) throw new TypeError('missing token');
      token = credentials.apiToken.reveal();
      if (!isHeaderSafeCredential(token) || !token.startsWith('pk_') || token.length <= 3) throw new TypeError('invalid token');
    } catch {
      return this.outcome('defect', operation, 'AUTHENTICATION', 'ClickUp connection credentials are unavailable');
    }
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload)); }
    catch { return this.outcome('defect', operation, 'LOCAL_DEFECT', 'ClickUp request could not be built'); }
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.endpoint + path + (query?.size ? `?${query}` : ''), method,
        headers: { Authorization: token, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.endpoint === apiBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: 9_000, signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) return this.outcome('invalid', operation, 'RESPONSE_TOO_LARGE', 'ClickUp response exceeds the configured maxResponseBytes limit');
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) return this.outcome('notSent', operation, 'TRANSPORT', 'ClickUp could not be reached; no request was sent');
      return this.outcome('unconfirmed', operation, 'TRANSPORT', 'ClickUp request failed before a response arrived');
    }
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(token))) return this.outcome('invalid', operation, 'PROTOCOL', 'ClickUp response reflected the connection credential');
      return { kind: 'succeeded', response };
    }
    const status = response.status;
    const code = readErrorCode(response.body);
    const withCode = (message: string) => message + (code ? ` [${code}]` : '');
    if (status === 429) return this.outcome('rateLimited', operation, 'RATE_LIMIT', withCode('ClickUp rate limited the token (HTTP 429)'), this.rateLimitDelay(response));
    if (status === 408 || status >= 500) return this.outcome('unconfirmed', operation, 'AVAILABILITY', withCode(`ClickUp could not complete the request (HTTP ${status})`));
    if (status === 404 || status === 410) return this.outcome('notFound', operation, 'NOT_FOUND', withCode(`ClickUp found no such resource (HTTP ${status})`));
    if (status === 401 && ['OAUTH_023', 'OAUTH_026', 'OAUTH_027', ...Array.from({ length: 17 }, (_, index) => `OAUTH_${String(index + 29).padStart(3, '0')}`)].includes(code))
      return this.outcome('notFound', operation, 'AUTHORIZATION', withCode("ClickUp reports that the resource's Workspace is not authorized for the token (HTTP 401)"));
    if (status >= 300 && status < 400) return this.outcome('rejected', operation, 'PROTOCOL', `ClickUp redirected the request (HTTP ${status})`);
    if (status === 401) return this.outcome('rejected', operation, 'AUTHENTICATION', withCode('ClickUp rejected the personal API token (HTTP 401)'));
    const kind: Failure['kind'] = status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : [400, 422].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return this.outcome('rejected', operation, kind, withCode(`ClickUp rejected the request (HTTP ${status})`));
  }

  private rateLimitDelay(response: ProviderResponse): number {
    const header = (name: string) => { const value = response.headers[name]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; };
    const retryAfter = parseRetryAfter(header('retry-after'), this.now());
    if (retryAfter > 0) return Math.min(retryAfter, 60_000);
    const reset = Number(header('x-ratelimit-reset'));
    return Number.isSafeInteger(reset) && reset > 0 ? Math.min(60_000, Math.max(1_000, reset * 1_000 - this.now().getTime())) : 0;
  }

  private outcome(kind: Exclude<Exchange['kind'], 'succeeded'>, operation: string, failureKind: Failure['kind'], message: string, retryAfterMs = 0): Exchange {
    return { kind, failure: { kind: failureKind, provider: 'clickup', operation, message }, retryAfterMs };
  }
}

function readErrorCode(body: Uint8Array): string {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString());
    if (value && typeof value === 'object' && 'ECODE' in value && typeof value.ECODE === 'string' && /^[A-Z][A-Z0-9]{0,15}_[0-9]{1,6}$/.test(value.ECODE)) return value.ECODE;
  } catch { /* Provider text is never exposed. */ }
  return '';
}
