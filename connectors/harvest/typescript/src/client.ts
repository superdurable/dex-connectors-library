// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProjectsDefinition, ListTimeEntriesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProjectsInput, ListTimeEntriesInput, Project, ProjectPage, TimeEntry, TimeEntryPage } from './generated/models.js';

const safeCursor = /^[A-Za-z0-9_-]{1,128}$/;
const accountIdPattern = /^[1-9][0-9]{0,18}$/;
const defaultBaseUrl = 'https://api.harvestapp.com/v2';
const defaultTimeoutMs = 30_000;
const defaultMaxResponseBytes = 4_194_304;
type Sender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Read-only Harvest client with call-time credentials. */
export class Client {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly accountId: string;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: Sender;

  /** Validate configuration and bind a credential source and optional HTTP sender without a provider call. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, send: Sender = sendProviderRequest) {
    if (!credentials) throw new TypeError('Harvest credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('Harvest timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Harvest maxResponseBytes must be positive');
    if (!accountIdPattern.test(config.accountId)) throw new TypeError('Harvest accountId must be a decimal ID');
    this.accountId = config.accountId;
    this.credentials = credentials;
    this.send = send;
  }

  /** Bind the project page Query. */
  listProjects(): ListProjectsOperation { return new ListProjectsOperation(this); }
  /** Bind the time entry page Query. */
  listTimeEntries(): ListTimeEntriesOperation { return new ListTimeEntriesOperation(this); }

  /** Send one GET and classify its provider result. */
  async invokeGet<Output>(call: Call, operation: string, path: string, zero: Output, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return failed(operation, 'defect', zero, 'AUTHENTICATION', 'connection credentials are unavailable'); }
    if (!(credentials.apiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.apiKey.reveal())) {
      return failed(operation, 'defect', zero, 'AUTHENTICATION', 'connection credentials are invalid');
    }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseUrl + path, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${credentials.apiKey.reveal()}`, 'Harvest-Account-Id': this.accountId, 'User-Agent': 'superdurable-harvest-connector' },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return classifyStatus(operation, zero, error.status, error.headers?.['retry-after']);
        return failed(operation, 'invalidResponse', zero, 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit');
      }
      return retry(operation, 'AVAILABILITY', 'provider request failed');
    }
    if (response.status !== 200) return classifyStatus(operation, zero, response.status, response.headers['retry-after']);
    try { return newQueryBranch('listed', decode(response.body)); }
    catch { return failed(operation, 'invalidResponse', zero, 'PROTOCOL', 'provider returned unusable JSON'); }
  }
}

/** Query one Harvest project page. */
export class ListProjectsOperation implements Query<ListProjectsInput, ProjectPage> {
  /** Bind a configured client. */ constructor(private readonly client: Client) {}
  /** Return stable operation metadata. */ definition(): QueryDefinition { return ListProjectsDefinition; }
  /** Validate and read one project page. */
  invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<ProjectPage>> {
    const zero: ProjectPage = { projects: [] };
    const problem = pageProblem(input);
    if (problem) return Promise.resolve(failed('listProjects', 'defect', zero, 'VALIDATION', problem));
    return this.client.invokeGet(call, 'listProjects', pagePath('/projects', input), zero, body => decodePage(body, 'projects', '/projects') as ProjectPage);
  }
}

/** Query one Harvest time entry page. */
export class ListTimeEntriesOperation implements Query<ListTimeEntriesInput, TimeEntryPage> {
  /** Bind a configured client. */ constructor(private readonly client: Client) {}
  /** Return stable operation metadata. */ definition(): QueryDefinition { return ListTimeEntriesDefinition; }
  /** Validate and read one time entry page. */
  invoke(call: Call, input: ListTimeEntriesInput): Promise<QueryAttempt<TimeEntryPage>> {
    const zero: TimeEntryPage = { timeEntries: [] };
    const problem = pageProblem(input);
    if (problem) return Promise.resolve(failed('listTimeEntries', 'defect', zero, 'VALIDATION', problem));
    return this.client.invokeGet(call, 'listTimeEntries', pagePath('/time_entries', input), zero, body => decodePage(body, 'time_entries', '/time_entries') as TimeEntryPage);
  }
}

function failed<Output>(operation: string, branch: string, zero: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, zero, {}, { kind, provider: 'harvest', operation, message });
}
function retry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> {
  return newQueryRetry({ kind, provider: 'harvest', operation, message }, delay);
}
function classifyStatus<Output>(operation: string, zero: Output, status: number, retryHeader: string | string[] | undefined): QueryAttempt<Output> {
  const delay = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '');
  if (status === 429) return retry(operation, 'RATE_LIMIT', 'provider rate limit', delay);
  if (status === 408 || status === 409 || status >= 500) return retry(operation, 'AVAILABILITY', 'provider temporarily unavailable', delay);
  if (status === 401) return failed(operation, 'providerRejected', zero, 'AUTHENTICATION', 'provider rejected the API key');
  if (status === 403) return failed(operation, 'providerRejected', zero, 'AUTHORIZATION', 'provider denied the request');
  return failed(operation, 'providerRejected', zero, 'PROVIDER_REJECTION', `provider rejected HTTP ${status}`);
}
function pageProblem(input: ListProjectsInput | ListTimeEntriesInput): string {
  const size = input.perPage ?? 0;
  if (!Number.isSafeInteger(size) || size < 0 || size > 2000) return 'perPage must be 1 to 2000 or zero';
  if (input.cursor && !safeCursor.test(input.cursor)) return 'cursor is invalid';
  return '';
}
function pagePath(path: string, input: ListProjectsInput | ListTimeEntriesInput): string {
  const parameters = new URLSearchParams();
  if (input.cursor) parameters.set('cursor', input.cursor);
  parameters.set('per_page', String(input.perPage || 100));
  return `${path}?${parameters}`;
}
function decodePage(body: Uint8Array, field: 'projects' | 'time_entries', path: string): ProjectPage | TimeEntryPage {
  const wire: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!isObject(wire) || !Array.isArray(wire[field])) throw new TypeError('missing page collection');
  const records = wire[field] as unknown[];
  const links = wire.links;
  let cursor = '';
  if (links !== undefined && links !== null) {
    if (!isObject(links)) throw new TypeError('invalid links');
    const next = links.next;
    if (next !== undefined && next !== null && next !== '') {
      if (typeof next !== 'string') throw new TypeError('invalid next URL');
      const parsed = new URL(next);
      if (parsed.protocol !== 'https:' || parsed.host !== 'api.harvestapp.com' || parsed.pathname !== `/v2${path}` || parsed.username || parsed.password || parsed.hash) throw new TypeError('unexpected next URL');
      cursor = parsed.searchParams.get('cursor') ?? '';
      if (!safeCursor.test(cursor)) throw new TypeError('invalid cursor');
    }
  }
  if (field === 'projects') {
    const projects: Project[] = records.map((value: unknown) => {
      if (!isObject(value)) throw new TypeError('invalid project');
      return { id: integerOrZero(value.id), name: stringOrEmpty(value.name), isActive: booleanOrFalse(value.is_active) };
    });
    return { projects, ...(cursor ? { nextCursor: cursor } : {}) };
  }
  const timeEntries: TimeEntry[] = records.map((value: unknown) => {
    if (!isObject(value)) throw new TypeError('invalid time entry');
    const project = isObject(value.project) ? value.project : {};
    return { id: integerOrZero(value.id), project: { id: integerOrZero(project.id) }, hours: numberOrZero(value.hours), spentDate: stringOrEmpty(value.spent_date) };
  });
  return { timeEntries, ...(cursor ? { nextCursor: cursor } : {}) };
}
function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function integerOrZero(value: unknown): number { if (value === undefined || value === null) return 0; if (!Number.isSafeInteger(value)) throw new TypeError('invalid integer'); return value as number; }
function numberOrZero(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number') throw new TypeError('invalid number'); return value; }
function stringOrEmpty(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('invalid string'); return value; }
function booleanOrFalse(value: unknown): boolean { if (value === undefined || value === null) return false; if (typeof value !== 'boolean') throw new TypeError('invalid boolean'); return value; }
