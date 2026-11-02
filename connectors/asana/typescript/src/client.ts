// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { ListTasksOperation, GetTaskOperation, CreateTaskOperation, UpdateTaskOperation, AddCommentOperation } from './operations.js';

const publicEndpoint = 'https://app.asana.com/api/1.0';

/** Injectable transport and clock for deterministic provider tests. */
export interface ClientOptions {
  /** Provider request sender; the default pins DNS and refuses redirects. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and Retry-After clock. */ now?: () => Date;
}

/** Bounded provider exchange, including failures before or after dispatch. */
export type Exchange =
  | { response: ProviderResponse; isTooLarge: boolean; reflectsSecret: boolean }
  | { error: 'beforeSend' | 'afterSend' | 'notBuilt' | 'interrupted' };

/** Authenticated Asana API client shared by operation-specific Step factories. */
export class Client {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate settings and own the credential source without resolving a secret. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ClientOptions = {}) {
    if (!credentials) throw new TypeError('Asana credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || publicEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Asana maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the task list query. */ listTasks(): ListTasksOperation { return new ListTasksOperation(this); }
  /** Bind the task detail query. */ getTask(): GetTaskOperation { return new GetTaskOperation(this); }
  /** Bind the task create mutation. */ createTask(): CreateTaskOperation { return new CreateTaskOperation(this); }
  /** Bind the absolute task update mutation. */ updateTask(): UpdateTaskOperation { return new UpdateTaskOperation(this); }
  /** Bind the comment create mutation. */ addComment(): AddCommentOperation { return new AddCommentOperation(this); }

  /** Resolve the current personal token for exactly one operation attempt. */
  async token(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString)) return undefined;
      const token = credentials.accessToken.reveal();
      return isHeaderSafeCredential(token) ? token : undefined;
    } catch { return undefined; }
  }

  /** Send one Asana JSON request while bounding response bytes and elapsed time. */
  async exchange(call: Call, token: string, method: 'GET' | 'POST' | 'PUT', path: string, query: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<Exchange> {
    const remaining = call.context.deadline ? call.context.deadline.getTime() - this.now().getTime() - 1_000 : 25_000;
    if (remaining < 2_500) return { error: 'beforeSend' };
    const sortedQuery = [...query.entries()].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
    const search = new URLSearchParams(sortedQuery).toString();
    const request: ProviderRequest = {
      url: `${this.endpoint}${path}${search ? `?${search}` : ''}`,
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(payload === undefined ? {} : { 'Content-Type': 'application/json' }) },
      ...(payload === undefined ? {} : { body: Buffer.from(JSON.stringify(payload), 'utf8') }),
      maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
      timeoutMs: Math.min(20_000, remaining),
      addressPolicy: this.endpoint === publicEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: signal ?? call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      const isTooLarge = response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes;
      return { response: { ...response, body: isTooLarge ? new Uint8Array() : response.body }, isTooLarge, reflectsSecret: Buffer.from(response.body).includes(Buffer.from(token)) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, isTooLarge: true, reflectsSecret: false };
      if (error instanceof AddressRefusal || error instanceof TypeError) return { error: 'notBuilt' };
      if (error instanceof ProviderTransportError) return { error: error.isProvablyNotSent ? 'beforeSend' : 'afterSend' };
      return { error: 'afterSend' };
    }
  }

  /** Build a secret-free receipt, with no Asana request ID header. */
  receipt(call: Call, providerObjectId = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'asana', ...(providerObjectId ? { providerObjectId } : {}), observedAt: this.now().toISOString() };
  }

  /** Interpret Retry-After with the same bounded SDK parser as other connectors. */
  retryAfter(exchange: Exchange): number {
    if (!('response' in exchange)) return 0;
    const value = exchange.response.headers['retry-after'];
    return parseRetryAfter(typeof value === 'string' ? value : '', this.now());
  }
}

/** Secret-free Asana failure for one manifest operation. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'asana', operation, message };
}

/** Classify a conclusive provider status without reflecting its free-text body. */
export function rejection(operation: string, subject: string, status: number): Failure {
  const kind: Failure['kind'] = status === 400 ? 'VALIDATION' : status === 401 ? 'AUTHENTICATION' : status === 402 || status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : 'PROVIDER_REJECTION';
  return failure(operation, kind, `Asana rejected the ${subject} with HTTP ${status}`);
}

/** Convert a successful response envelope into one untrusted JSON object. */
export function dataObject(exchange: Exchange): Record<string, unknown> {
  if (!('response' in exchange)) throw new TypeError('Asana response is unavailable');
  const parsed: unknown = JSON.parse(Buffer.from(exchange.response.body).toString('utf8'));
  if (!parsed || typeof parsed !== 'object' || !('data' in parsed) || !parsed.data || typeof parsed.data !== 'object' || Array.isArray(parsed.data)) throw new TypeError('response has no data object');
  return parsed.data as Record<string, unknown>;
}
