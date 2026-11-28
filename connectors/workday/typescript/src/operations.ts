// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetWorkerDefinition, ListWorkersDefinition } from './generated/definitions.js';
import type { GetWorkerInput, ListWorkersInput, WorkerPage, WorkerSummary } from './generated/models.js';
import type { WorkdayClient, WorkdayExchange } from './client.js';

const emptyWorker = (): WorkerSummary => ({ id: '', descriptor: '' });
const workerIDPattern = /^[A-Fa-f0-9]{32}$/;

/** Read one worker by Workday ID. */
export class GetWorkerOperation implements Query<GetWorkerInput, WorkerSummary> {
  /** Bind the provider client. */ constructor(private readonly client: WorkdayClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetWorkerDefinition; }
  /** Project only the stable WID and display name into the Flow. */
  async invoke(call: Call, input: GetWorkerInput): Promise<QueryAttempt<WorkerSummary>> {
    if (!input || typeof input.workerId !== 'string' || !workerIDPattern.test(input.workerId))
      return branch('getWorker', emptyWorker(), 'defect', 'VALIDATION', 'worker WID is invalid');
    const exchange = await this.client.get(call, `/workers/${encodeURIComponent(input.workerId)}`);
    const failed = classifyRead(exchange, 'getWorker', emptyWorker());
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const result = document;
    if (!result || result.id !== input.workerId || typeof result.descriptor !== 'string' || !result.descriptor)
      return branch('getWorker', emptyWorker(), 'invalidResponse', 'PROTOCOL', 'Workday returned invalid worker JSON');
    return newQueryBranch('found', { id: result.id, descriptor: result.descriptor });
  }
}

/** Read one page of workers visible to the tenant connection. */
export class ListWorkersOperation implements Query<ListWorkersInput, WorkerPage> {
  /** Bind the provider client. */
  constructor(private readonly client: WorkdayClient) {}

  /** Return the worker directory Query contract. */
  definition(): QueryDefinition { return ListWorkersDefinition; }

  /** Validate pagination and project each worker to its WID and display name. */
  async invoke(call: Call, input: ListWorkersInput): Promise<QueryAttempt<WorkerPage>> {
    const empty: WorkerPage = { total: 0, data: [] };
    const { limit = 0, offset = 0, search = '' } = input;
    if (!Number.isSafeInteger(limit) || limit < 0 || limit > 100 || !Number.isSafeInteger(offset) || offset < 0
        || typeof search !== 'string' || (search !== '' && Buffer.byteLength(search.trim()) < 3))
      return branch('listWorkers', empty, 'defect', 'VALIDATION', 'Invalid worker page options');
    const parameters = new URLSearchParams();
    if (limit) parameters.set('limit', String(limit));
    if (offset) parameters.set('offset', String(offset));
    if (search) parameters.set('search', search);
    const exchange = await this.client.get(call, '/workers' + (parameters.size ? '?' + parameters.toString() : ''));
    const failed = classifyRead(exchange, 'listWorkers', empty);
    if (failed) return failed;
    const page = readWorkerPage(exchange.body);
    return page ? newQueryBranch('found', page) : branch('listWorkers', empty, 'invalidResponse', 'PROTOCOL', 'Invalid worker page');
  }
}

function readWorkerPage(body: Uint8Array): WorkerPage | undefined {
  let document: unknown;
  try {
    document = JSON.parse(Buffer.from(body).toString('utf8'), (_key: string, value: unknown, context?: { source?: string }) =>
      typeof value === 'number' && context?.source && !/^-?\d+$/.test(context.source) ? NaN : value);
  } catch { return undefined; }
  if (!isObject(document)) return undefined;
  const page = foldFieldNames(document);
  const total = page.total ?? 0;
  if (typeof total !== 'number' || !Number.isSafeInteger(total) || total < 0 || !Array.isArray(page.data)) return undefined;
  const data: WorkerSummary[] = [];
  for (const row of page.data) {
    if (!isObject(row)) return undefined;
    const { id, descriptor } = foldFieldNames(row);
    if (typeof id !== 'string' || !workerIDPattern.test(id) || typeof descriptor !== 'string' || !descriptor) return undefined;
    data.push({ id, descriptor });
  }
  return { total, data };
}

function foldFieldNames(record: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(record).map(([key, value]) => [key.toLowerCase(), value]));
}

function classifyRead<T>(exchange: WorkdayExchange, operation: string, output: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return branch(operation, output, 'defect', 'AUTHENTICATION', 'Workday connection needs authorization');
  if (exchange.error === 'refresh' || exchange.error === 'request') return newQueryRetry(failure(operation, 'TRANSPORT', 'Workday worker request failed'));
  if (exchange.status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'Workday rate limited the request'), exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500)
    return newQueryRetry(failure(operation, 'AVAILABILITY', 'Workday is temporarily unavailable'), exchange.retryAfterMs);
  if (exchange.status === 404 && operation === 'getWorker') return branch(operation, output, 'notFound', 'NOT_FOUND', 'Workday worker was not found');
  if (exchange.status !== 200) return branch(operation, output, 'providerRejected', 'PROVIDER_REJECTION', `Workday rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return branch(operation, output, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Workday worker response was too large');
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch<T>(operation: string, output: T, branchId: string, kind: Failure['kind'], message: string): QueryAttempt<T> {
  return newQueryBranch(branchId, output, {}, failure(operation, kind, message));
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'workday', operation, message };
}
