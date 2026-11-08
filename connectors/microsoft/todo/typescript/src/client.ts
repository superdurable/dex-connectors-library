// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTaskListsDefinition, ListTasksDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, ListTaskListsInput, ListTasksInput, Task, TaskList, TaskListsPage, TasksPage } from './generated/models.js';

const graphRoot = 'https://graph.microsoft.com/v1.0';
const maxPages = 20;
const maxItems = 2000;

/** Replace the provider sender for tests. */
export interface MicrosoftTodoClientOptions {
  /** Sender used for Graph requests. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

class ReadFailure extends Error {
  constructor(readonly branch: string, readonly kind: Failure['kind'], message: string, readonly retryAfterMs = 0) { super(message); }
}

/** Microsoft Graph To Do client with bounded collection reads. */
export class MicrosoftTodoClient {
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Microsoft', auth: Authorization, credentialFields: CredentialFields });

  /** Validate collection settings and bind the credential source. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: MicrosoftTodoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Microsoft To Do configuration and credentials are required');
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Microsoft To Do max response bytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listTaskLists Query. */ listTaskLists(): ListTaskListsOperation { return new ListTaskListsOperation(this); }
  /** Bind the listTasks Query. */ listTasks(): ListTasksOperation { return new ListTasksOperation(this); }

  /** Read every Graph page within the connector's page and item limits. */
  async readCollection(call: Call, path: string): Promise<Array<Record<string, unknown>>> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) throw new ReadFailure('defect', 'AUTHENTICATION', 'To Do connection needs authorization');
      throw new ReadFailure('', 'TRANSPORT', 'To Do credential refresh failed');
    }
    if (!isHeaderSafeCredential(token)) throw new ReadFailure('defect', 'AUTHENTICATION', 'To Do access token is unavailable');
    let target = graphRoot + path;
    const seen = new Set<string>();
    const records: Array<Record<string, unknown>> = [];
    for (let page = 0; page < maxPages; page++) {
      if (seen.has(target)) throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph repeated a page');
      seen.add(target);
      const request: ProviderRequest = {
        url: target, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: 15_000, addressPolicy: 'publicHTTPS',
        signal: call.context.cancellationSignal,
      };
      let response: ProviderResponse;
      let tooLarge = false;
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) {
          response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
          tooLarge = true;
        } else throw new ReadFailure('', 'TRANSPORT', 'Graph request failed');
      }
      if (response.status === 429 || response.status === 408 || response.status >= 500)
        throw new ReadFailure('', 'AVAILABILITY', 'Graph is temporarily unavailable', parseRetryAfter(header(response.headers, 'retry-after')));
      if (response.status === 404) throw new ReadFailure('notFound', 'NOT_FOUND', 'To Do list was not found');
      if (response.status !== 200) throw new ReadFailure('providerRejected', 'PROVIDER_REJECTION', 'Graph rejected the request');
      if (tooLarge || response.body.length > this.maxResponseBytes) throw new ReadFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Graph response exceeded the limit');
      let raw: unknown;
      try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
      catch { throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned invalid collection JSON'); }
      if (!isObject(raw) || !Array.isArray(raw.value) || !raw.value.every(isObject) || (raw['@odata.nextLink'] !== undefined && typeof raw['@odata.nextLink'] !== 'string'))
        throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned invalid collection JSON');
      records.push(...raw.value as Array<Record<string, unknown>>);
      if (records.length > maxItems) throw new ReadFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Graph collection exceeded the item limit');
      const next = raw['@odata.nextLink'];
      if (typeof next !== 'string' || !next) return records;
      let parsed: URL;
      try { parsed = new URL(next); } catch { throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned an unsafe next page URL'); }
      if (parsed.protocol !== 'https:' || parsed.host !== 'graph.microsoft.com' || parsed.username || parsed.password || parsed.hash || parsed.pathname !== '/v1.0' + path)
        throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned an unsafe next page URL');
      target = next;
    }
    throw new ReadFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Graph collection exceeded the page limit');
  }
}

/** List the signed-in person's To Do task lists. */
export class ListTaskListsOperation implements Query<ListTaskListsInput, TaskListsPage> {
  /** Bind a Microsoft To Do client. */ constructor(private readonly client: MicrosoftTodoClient) {}
  /** Return the manifest-generated Query contract. */ definition(): QueryDefinition { return ListTaskListsDefinition; }
  /** Classify Graph list responses using the Go branches. */
  async invoke(call: Call, _input: ListTaskListsInput): Promise<QueryAttempt<TaskListsPage>> {
    void _input;
    try {
      const records = await this.client.readCollection(call, '/me/todo/lists');
      const lists: TaskList[] = records.map(record => ({ id: record.id as string, displayName: record.displayName as string }));
      if (lists.some(item => typeof item.id !== 'string' || !item.id || typeof item.displayName !== 'string' || !item.displayName))
        throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned an invalid task list');
      return newQueryBranch('listed', { lists });
    } catch (error) {
      if (!(error instanceof ReadFailure)) throw error;
      return failed('listTaskLists', error.branch === 'notFound' ? 'providerRejected' : error.branch, error, { lists: [] });
    }
  }
}

/** List tasks in a selected To Do list. */
export class ListTasksOperation implements Query<ListTasksInput, TasksPage> {
  /** Bind a Microsoft To Do client. */ constructor(private readonly client: MicrosoftTodoClient) {}
  /** Return the manifest-generated Query contract. */ definition(): QueryDefinition { return ListTasksDefinition; }
  /** Classify Graph task responses using the Go branches. */
  async invoke(call: Call, input: ListTasksInput): Promise<QueryAttempt<TasksPage>> {
    if (!input || typeof input.listId !== 'string' || !input.listId.trim() || /[\r\n]/.test(input.listId))
      return failed('listTasks', 'defect', new ReadFailure('defect', 'VALIDATION', 'To Do list ID is required'), { tasks: [] });
    try {
      const records = await this.client.readCollection(call, `/me/todo/lists/${encodeURIComponent(input.listId)}/tasks`);
      const tasks: Task[] = records.map(record => ({ id: record.id as string, title: record.title as string, status: record.status == null ? '' : record.status as string }));
      if (tasks.some(item => typeof item.id !== 'string' || !item.id || typeof item.title !== 'string' || !item.title || typeof item.status !== 'string'))
        throw new ReadFailure('invalidResponse', 'PROTOCOL', 'Graph returned an invalid task');
      return newQueryBranch('listed', { tasks });
    } catch (error) {
      if (!(error instanceof ReadFailure)) throw error;
      return failed('listTasks', error.branch, error, { tasks: [] });
    }
  }
}

function failed<T>(operation: string, branch: string, result: ReadFailure, empty: T): QueryAttempt<T> {
  const failure: Failure = { kind: result.kind, provider: 'microsoft-todo', operation, message: result.message };
  return branch ? newQueryBranch(branch, empty, {}, failure) : newQueryRetry(failure, result.retryAfterMs);
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
