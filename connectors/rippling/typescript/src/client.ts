// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetWorkerDefinition, ListWorkersDefinition } from './generated/definitions.js';
import type { Config, Credentials, WorkerSummary, GetWorkerInput, ListWorkersInput, WorkerPage } from './generated/models.js';

const defaultBaseURL = 'https://rest.ripplingapis.com';
const emptyWorker = (): WorkerSummary => ({ id: '', status: '', title: '' });

/** Injectable provider sender for tests and local applications. */
export interface RipplingClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Rippling REST API client for worker metadata. */
export class RipplingClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Rippling. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RipplingClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Rippling configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Rippling request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getWorker Query. */ getWorker(): GetWorkerOperation { return new GetWorkerOperation(this); }

  /** Bind a page of workers visible to the configured connection. */
  listWorkers(): ListWorkersOperation { return new ListWorkersOperation(this); }

  /** Resolve the API token and send one bounded worker lookup. */
  async get(call: Call, workerID: string): Promise<Exchange> {
    return this.readWorkers(call, `/workers/${encodeURIComponent(workerID)}/`);
  }

  /** Send a bounded worker-page request with the supplied opaque cursor and limit. */
  async getPage(call: Call, limit: number, cursor: string): Promise<Exchange> {
    const query = new URLSearchParams();
    if (cursor) query.set('cursor', cursor);
    if (limit) query.set('limit', String(limit));
    return this.readWorkers(call, query.size ? `/workers/?${query}` : '/workers/');
  }

  private async readWorkers(call: Call, path: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiToken}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one worker's ID, status, and title. */
export class GetWorkerOperation implements Query<GetWorkerInput, WorkerSummary> {
  /** Bind the provider client. */ constructor(private readonly client: RipplingClient) {}
  /** Return the generated Query worker. */ definition(): QueryDefinition { return GetWorkerDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetWorkerInput): Promise<QueryAttempt<WorkerSummary>> {
    if (!isValidWorkerID(input?.workerId))
      return terminal('getWorker', emptyWorker(), 'defect', 'VALIDATION', 'worker ID must contain 1–64 URL-safe characters');
    const exchange = await this.client.get(call, input.workerId);
    const failed = classifyRead(exchange, 'getWorker', emptyWorker());
    if (failed) return failed;
    const workerSummary = decodeWorker(exchange.body, input.workerId);
    if (!workerSummary) return terminal('getWorker', emptyWorker(), 'invalidResponse', 'PROTOCOL', 'Rippling returned invalid worker JSON');
    return newQueryBranch('found', workerSummary);
  }
}

/** Read one page of worker identities visible to a Rippling token. */
export class ListWorkersOperation implements Query<ListWorkersInput, WorkerPage> {
  /** Bind a credential-resolving Rippling client. */
  constructor(private readonly client: RipplingClient) {}

  /** Return the manifest-generated worker page definition. */
  definition(): QueryDefinition { return ListWorkersDefinition; }

  /** Project worker identities and extract the cursor from the provider's next link. */
  async invoke(call: Call, input: ListWorkersInput): Promise<QueryAttempt<WorkerPage>> {
    const empty: WorkerPage = { workers: [], nextCursor: '' };
    const limit = input?.limit ?? 0;
    const cursor = input?.cursor ?? '';
    if (!input || !Number.isSafeInteger(limit) || limit < 0 || limit > 100 ||
        typeof cursor !== 'string' || Buffer.byteLength(cursor) > 2048)
      return terminal('listWorkers', empty, 'defect', 'VALIDATION', 'invalid worker page limit or cursor');
    const exchange = await this.client.getPage(call, limit, cursor);
    const failed = classifyRead(exchange, 'listWorkers', empty);
    if (failed) return failed;
    const page = decodeWorkerPage(exchange.body);
    if (!page) return terminal('listWorkers', empty, 'invalidResponse', 'PROTOCOL', 'Rippling returned invalid worker page JSON');
    return newQueryBranch('found', page);
  }
}

function classifyRead<T>(exchange: Exchange, operation: string, empty: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return terminal(operation, empty, 'defect', 'AUTHENTICATION', 'Rippling credentials are unavailable or invalid');
  if (exchange.error === 'request') return retry(operation, 'TRANSPORT', 'Rippling request failed');
  if (exchange.status === 429) return retry(operation, 'RATE_LIMIT', 'Rippling rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry(operation, 'AVAILABILITY', 'Rippling is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404 && operation === 'getWorker') return terminal(operation, empty, 'notFound', 'NOT_FOUND', 'Rippling did not expose the worker');
  if (exchange.status !== 200) return terminal(operation, empty, 'providerRejected', 'PROVIDER_REJECTION', `Rippling rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return terminal(operation, empty, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Rippling response exceeded the configured limit');
  return undefined;
}

function isValidWorkerID(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'rippling', operation, kind, message }; }
function terminal<T>(operation: string, empty: T, branch: string, kind: Failure['kind'], message: string): QueryAttempt<T> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function retry<T>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<T> { return newQueryRetry(failure(operation, kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseDocument(body: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
}

function decodeWorker(body: Uint8Array, workerId: string): WorkerSummary | undefined {
  const worker = workerSummary(parseDocument(body));
  return worker?.id === workerId ? worker : undefined;
}

function workerSummary(raw: unknown): WorkerSummary | undefined {
  if (!isObject(raw) || !isValidWorkerID(raw.id) || (raw.status != null && typeof raw.status !== 'string') ||
      (raw.title != null && typeof raw.title !== 'string')) return undefined;
  return { id: raw.id, title: raw.title ?? '', status: raw.status ?? '' };
}

function decodeWorkerPage(body: Uint8Array): WorkerPage | undefined {
  const document = parseDocument(body);
  if (!isObject(document) || !Array.isArray(document.results)) return undefined;
  const workers: WorkerSummary[] = [];
  for (const record of document.results) {
    const worker = workerSummary(record);
    if (!worker) return undefined;
    workers.push(worker);
  }
  const link = document.next_link;
  if (link == null || link === '') return { workers, nextCursor: '' };
  if (typeof link !== 'string' || Array.from(link).some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return undefined;
  try {
    const url = new URL(link, 'https://rest.ripplingapis.com');
    if (/%(?![0-9a-f]{2})/i.test(url.pathname)) return undefined;
    for (const pair of url.search.slice(1).split('&')) {
      if (pair.includes(';') || /%(?![0-9a-f]{2})/i.test(pair)) continue;
      const cursor = new URLSearchParams(pair).get('cursor');
      if (cursor !== null) return cursor ? { workers, nextCursor: cursor } : undefined;
    }
  } catch { return undefined; }
  return undefined;
}
