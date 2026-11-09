// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetWorkerDefinition } from './generated/definitions.js';
import type { GetWorkerInput, WorkerSummary } from './generated/models.js';
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
      return branch('defect', 'VALIDATION', 'worker WID is invalid');
    const exchange = await this.client.get(call, `/workers/${encodeURIComponent(input.workerId)}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const result = document;
    if (!result || result.id !== input.workerId || typeof result.descriptor !== 'string' || !result.descriptor)
      return branch('invalidResponse', 'PROTOCOL', 'Workday returned invalid worker JSON');
    return newQueryBranch('found', { id: result.id, descriptor: result.descriptor });
  }
}

function classifyRead(exchange: WorkdayExchange): QueryAttempt<WorkerSummary> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Workday connection needs authorization');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Workday worker request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Workday rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Workday is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404) return branch('notFound', 'NOT_FOUND', 'Workday worker was not found');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Workday worker response exceeded the configured limit');
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Workday rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<WorkerSummary> {
  return newQueryBranch(branchId, emptyWorker(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<WorkerSummary> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'workday', operation: 'getWorker', message };
}
