// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTaskStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTaskStatusInput, GetTaskStatusOutput } from './generated/models.js';

const taskEndpoint = 'https://api.vidu.cn/ent/v2/tasks/';
const emptyOutput: GetTaskStatusOutput = { taskId: '', state: '', errorCode: '', credits: 0, creationIds: [] };

/** Optional transport dependency for provider tests. */
export interface ShengshuClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read existing Vidu task metadata without returning signed media URLs. */
export class ShengshuClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings and bind the rotating credential source. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: ShengshuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Vidu configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Vidu configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the task-status Query. */
  getTaskStatus(): GetTaskStatusOperation { return new GetTaskStatusOperation(this); }
}

/** Read status and safe metadata for one existing Vidu task. */
export class GetTaskStatusOperation implements Query<GetTaskStatusInput, GetTaskStatusOutput> {
  /** Bind an authenticated Vidu client. */
  constructor(private readonly client: ShengshuClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return GetTaskStatusDefinition; }
  /** Request and classify one task-status response. */
  async invoke(call: Call, input: GetTaskStatusInput): Promise<QueryAttempt<GetTaskStatusOutput>> {
    if (!input || typeof input.taskId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(input.taskId)) return branch('defect', 'VALIDATION', 'taskId is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'API key is unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!apiKey || apiKey.length > 1024 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION', 'API key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${taskEndpoint}${input.taskId}/creations`, method: 'GET', headers: { Authorization: `Token ${apiKey}`, Accept: 'application/json' }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Vidu response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', 'Vidu task-status request failed');
    }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Vidu returned invalid task metadata'); }
    if (!isRecord(document) || document.id !== input.taskId || !['created', 'queueing', 'processing', 'success', 'failed'].includes(String(document.state)) || !Number.isSafeInteger(document.credits) || (document.credits as number) < 0) return branch('invalidResponse', 'PROTOCOL', 'Vidu returned invalid task metadata');
    const creationIds: string[] = [];
    if (document.creations != null && !Array.isArray(document.creations)) return branch('invalidResponse', 'PROTOCOL', 'Vidu returned invalid creation metadata');
    for (const creation of (document.creations as unknown[] | null | undefined) ?? []) {
      if (!isRecord(creation) || typeof creation.id !== 'string' || !creation.id) return branch('invalidResponse', 'PROTOCOL', 'Vidu returned invalid creation metadata');
      creationIds.push(creation.id);
    }
    return newQueryBranch('found', { taskId: input.taskId, state: document.state as string, errorCode: typeof document.err_code === 'string' ? document.err_code : '', credits: document.credits as number, creationIds });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetTaskStatusOutput> {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Vidu rate limited the task-status request', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Vidu task status is temporarily unavailable', retryAfterMs);
  if (status === 404) return branch('notFound', 'NOT_FOUND', 'Vidu did not find the task');
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Vidu rejected the task-status request with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'shengshu', operation: 'getTaskStatus', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetTaskStatusOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetTaskStatusOutput> { return newQueryRetry(failure(kind, message), delayMs); }
