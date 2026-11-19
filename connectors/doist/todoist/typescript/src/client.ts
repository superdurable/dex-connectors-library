// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, Task, ListTasksInput, TaskPage } from './generated/models.js';
import { CreateTaskOperation, DeleteTaskOperation, GetTaskOperation, ListTasksOperation, UpdateTaskOperation } from './operations.js';

const publicBaseURL = 'https://api.todoist.com/api/v1';
const taskIDPattern = /^[A-Za-z0-9_-]{1,128}$/;
export const emptyTask: Task = { id: '', projectId: '', content: '', description: '', isCompleted: false };
export const emptyPage: TaskPage = { tasks: [] };
export const isTaskID = (value: string): boolean => taskIDPattern.test(value);
export const isValidUTF8 = (value: string): boolean => !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(value);

/** Injectable HTTP sender for bounded provider requests. */
export type TodoistRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Dependencies for provider tests. */
export interface TodoistClientOptions {
  /** Supply a deterministic transport. */ send?: TodoistRequestSender;
  /** Supply a deterministic clock. */ now?: () => Date;
}
/** Classified response from one request. */
export interface TodoistExchange {
  status: number;
  body: Uint8Array;
  task?: Task;
  failure?: Failure;
  retryAfterMs: number;
  isLocalDefect: boolean;
  isTransient: boolean;
  isUndispatched: boolean;
}
/** One resolved credential and request budget, reused for read-back. */
export interface TodoistAttempt {
  token?: string;
  failure?: TodoistExchange;
  writeDeadline: number;
  attemptDeadline: number;
  minimumRequestMs: number;
}

/** Authenticated Todoist API client with one credential resolution per attempt. */
export class TodoistClient {
  readonly baseURL: string;
  readonly maxResponseBytes: number;
  readonly requestTimeoutMs: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: TodoistRequestSender;
  private readonly now: () => Date;

  /** Validate immutable configuration and retain the credential source. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TodoistClientOptions = {}) {
    if (!credentials) throw new TypeError('Todoist credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || publicBaseURL);
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    this.requestTimeoutMs = config.timeoutMs || 15_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !Number.isSafeInteger(this.requestTimeoutMs) || this.requestTimeoutMs <= 0) throw new TypeError('Todoist response limit and timeout must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the task query. */ getTask(): GetTaskOperation { return new GetTaskOperation(this); }
  /** Bind the update mutation. */ updateTaskContent(): UpdateTaskOperation { return new UpdateTaskOperation(this); }
  /** Bind the page query. */ listTasks(): ListTasksOperation { return new ListTasksOperation(this); }
  /** Bind the creation mutation. */ createTask(): CreateTaskOperation { return new CreateTaskOperation(this); }
  /** Bind the deletion mutation. */ deleteTask(): DeleteTaskOperation { return new DeleteTaskOperation(this); }

  /** Resolve credentials and reserve write and read-back portions of the Execute attempt. */
  async attempt(call: Call, operation: string): Promise<TodoistAttempt> {
    const start = this.now().getTime();
    const available = Math.max(0, Math.min(30_000, call.context.deadline ? call.context.deadline.getTime() - start : 30_000));
    const budget: TodoistAttempt = { writeDeadline: start + available * 7 / 12, attemptDeadline: start + available * 5 / 6, minimumRequestMs: available / 10 };
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiToken instanceof SecretString) || !isHeaderSafeCredential(credentials.apiToken.reveal())) {
        return { ...budget, failure: localFailure(operation, 'AUTHENTICATION', 'connection credentials are invalid') };
      }
      return { ...budget, token: credentials.apiToken.reveal() };
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || error instanceof ObjectNotFoundError) {
        return { ...budget, failure: localFailure(operation, 'AUTHENTICATION', 'connection credentials are missing or need reauthorization') };
      }
      return { ...budget, failure: { ...baseExchange(), failure: todoistFailure(operation, 'AVAILABILITY', 'connection credentials could not be read, so the request was not sent'), isTransient: true, isUndispatched: true } };
    }
  }

  /** Send one task read or update and validate its task projection. */
  async taskRequest(call: Call, attempt: TodoistAttempt, operation: string, method: string, taskID: string, payload?: Uint8Array, write = false): Promise<TodoistExchange & { task?: Task }> {
    const result = await this.request(call, attempt, operation, method, `/tasks/${taskID}`, payload, '', write);
    if (result.failure) return result;
    try {
      const task = decodeTask(result.body);
      if (task.id !== taskID) throw new TypeError('task ID mismatch');
      return { ...result, task };
    } catch {
      return { ...result, failure: todoistFailure(operation, 'PROTOCOL', 'Todoist returned an invalid task response') };
    }
  }

  /** Send one bounded HTTP request without forwarding credentials on redirects. */
  async request(call: Call, attempt: TodoistAttempt, operation: string, method: string, endpoint: string, payload?: Uint8Array, requestID = '', write = false): Promise<TodoistExchange> {
    const deadline = write ? attempt.writeDeadline : attempt.attemptDeadline;
    const remaining = deadline - this.now().getTime();
    if (attempt.minimumRequestMs <= 0 || remaining < attempt.minimumRequestMs) return { ...baseExchange(), failure: todoistFailure(operation, 'AVAILABILITY', 'too little attempt time remained, so the request was not sent'), isTransient: true, isUndispatched: true };
    if (!attempt.token) return attempt.failure ?? localFailure(operation, 'AUTHENTICATION', 'connection credentials are invalid');
    const headers: Record<string, string> = { Authorization: `Bearer ${attempt.token}`, Accept: 'application/json' };
    if (method === 'POST') headers['Content-Type'] = 'application/json';
    if (requestID) headers['X-Request-ID'] = requestID;
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.baseURL + endpoint, method, headers, ...(payload ? { body: payload } : {}), maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.baseURL === publicBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: Math.max(1, Math.min(this.requestTimeoutMs, Math.floor(remaining))), signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof AddressRefusal) return localFailure(operation, 'LOCAL_DEFECT', 'Todoist request could not be built');
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        if (method === 'DELETE' && status >= 200 && status < 300) return baseExchange(status);
        if (status < 200 || status >= 300) return classifyHTTPFailure(operation, { status, headers: error.headers ?? {}, body: new Uint8Array() }, this.now());
        return { ...baseExchange(status), failure: todoistFailure(operation, 'RESPONSE_TOO_LARGE', 'Todoist response was interrupted or exceeded the response limit') };
      }
      const isUndispatched = error instanceof ProviderTransportError && error.isProvablyNotSent;
      return { ...baseExchange(), failure: todoistFailure(operation, 'TRANSPORT', isUndispatched ? 'Todoist could not be reached, so the request was not sent' : 'Todoist request did not return a complete response'), isTransient: true, isUndispatched };
    }
    if (method === 'DELETE' && response.status >= 200 && response.status < 300) return baseExchange(response.status);
    if (response.status < 200 || response.status >= 300) return classifyHTTPFailure(operation, response, this.now());
    if (Buffer.from(response.body).includes(Buffer.from(attempt.token))) return { ...baseExchange(response.status), failure: todoistFailure(operation, 'PROTOCOL', 'Todoist returned credential material in the response') };
    return { ...baseExchange(response.status), body: response.body };
  }
}

/** Decode only the bounded public task projection. */
export function decodeTask(body: Uint8Array): Task {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid task');
  const task = value as Record<string, unknown>;
  if (typeof task.id !== 'string' || !isTaskID(task.id) || typeof task.project_id !== 'string' || !isTaskID(task.project_id) || typeof task.content !== 'string' || typeof task.description !== 'string' || typeof task.checked !== 'boolean') throw new TypeError('invalid task');
  return { id: task.id, projectId: task.project_id, content: task.content, description: task.description, isCompleted: task.checked };
}
/** Decode one bounded cursor page with unique task identities. */
export function decodeTaskPage(body: Uint8Array, input: ListTasksInput): TaskPage {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid task page');
  const page = value as Record<string, unknown>;
  if (!Array.isArray(page.results) || typeof page.next_cursor !== 'string' && page.next_cursor !== null) throw new TypeError('invalid task page');
  if (page.results.length > (input.limit || 50)) throw new TypeError('oversized task page');
  const cursor = page.next_cursor ?? '';
  if (!isValidCursor(cursor) || cursor && cursor === input.cursor) throw new TypeError('invalid continuation cursor');
  const tasks: Task[] = [];
  const seen = new Set<string>();
  for (const item of page.results) {
    const task = decodeTask(Buffer.from(JSON.stringify(item)));
    if (seen.has(task.id) || input.projectId && task.projectId !== input.projectId) throw new TypeError('invalid task page item');
    seen.add(task.id);
    tasks.push(task);
  }
  return { tasks, ...(cursor ? { nextCursor: cursor } : {}) };
}
/** Validate Todoist's opaque pagination token. */
export function isValidCursor(value: string): boolean { return value === '' || value.length <= 4096 && /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(value); }
/** Derive Todoist's UUID-shaped stable create key from the Dex Call ID. */
export function createRequestID(callID: string): string {
  const digest = createHash('sha256').update(`todoist/createTask/${callID}`).digest();
  digest[6] = (digest[6]! & 0x0f) | 0x50;
  digest[8] = (digest[8]! & 0x3f) | 0x80;
  const hex = digest.subarray(0, 16).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
/** Construct a safe connector failure. */
export function todoistFailure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'todoist', operation, kind, message }; }
function baseExchange(status = 0): TodoistExchange { return { status, body: new Uint8Array(), retryAfterMs: 0, isLocalDefect: false, isTransient: false, isUndispatched: false }; }
function localFailure(operation: string, kind: Failure['kind'], message: string): TodoistExchange { return { ...baseExchange(), failure: todoistFailure(operation, kind, message), isLocalDefect: true }; }
function classifyHTTPFailure(operation: string, response: ProviderResponse, now: Date): TodoistExchange {
  const status = response.status;
  let kind: Failure['kind'] = 'PROVIDER_REJECTION';
  if (status === 401) kind = 'AUTHENTICATION';
  else if (status === 403) kind = 'AUTHORIZATION';
  else if (status === 404) kind = 'NOT_FOUND';
  else if (status === 409) kind = 'CONFLICT';
  else if (status === 400 || status === 422) kind = 'VALIDATION';
  else if (status === 429) kind = 'RATE_LIMIT';
  else if (status === 408 || status >= 500) kind = 'AVAILABILITY';
  const retryAfter = status === 429 || status === 408 || status >= 500 ? parseRetryAfter(String(response.headers['retry-after'] ?? ''), now) : 0;
  return { ...baseExchange(status), failure: todoistFailure(operation, kind, `Todoist returned HTTP ${status}`), retryAfterMs: retryAfter, isTransient: status === 408 || status >= 500 };
}
