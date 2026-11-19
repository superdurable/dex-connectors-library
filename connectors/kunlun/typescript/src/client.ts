// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTaskStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTaskStatusInput, GetTaskStatusOutput } from './generated/models.js';

const emptyOutput: GetTaskStatusOutput = { taskId: '', status: '', model: '' };
const taskEndpoint = 'https://api.mureka.cn/v1/song/query/';
const validStatuses = new Set(['preparing', 'queued', 'running', 'streaming', 'reviewing', 'succeeded', 'failed', 'timeouted', 'cancelled']);

/** Send a Mureka request through a caller-owned transport. */
export type TaskRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface KunlunClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: TaskRequestSender;
}
/** Resolve Mureka credentials for each song-task query. */
export class KunlunClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: TaskRequestSender;
  /** Validate configuration without contacting Mureka. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: KunlunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Kunlun configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Kunlun timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the task-status Query. */
  getTaskStatus(): GetTaskStatusOperation { return new GetTaskStatusOperation(this); }
}
/** Read one existing Mureka song task. */
export class GetTaskStatusOperation implements Query<GetTaskStatusInput, GetTaskStatusOutput> {
  /** Bind an authenticated Mureka client. */
  constructor(private readonly client: KunlunClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return GetTaskStatusDefinition; }
  /** Send and classify one task-status request. */
  async invoke(call: Call, input: GetTaskStatusInput): Promise<QueryAttempt<GetTaskStatusOutput>> {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(input.taskId ?? '')) return branch('defect', 'VALIDATION', 'taskId is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Mureka API key is unavailable or invalid'); }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 2048 || /\s/.test(key)) return branch('defect', 'AUTHENTICATION', 'Mureka API key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: taskEndpoint + encodeURIComponent(input.taskId), method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${key}` },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Mureka is temporarily unavailable', getRetryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Mureka response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Mureka song-task request failed');
    }
    const delay = getRetryAfter(response.headers);
    if (response.status === 429) {
      let message = '';
      try { const document = JSON.parse(new TextDecoder().decode(response.body)) as { error?: { message?: string } }; message = document.error?.message ?? ''; } catch { /* Quota text is advisory. */ }
      return retry(message.toLowerCase().includes('exceeded your current quota') ? 'QUOTA_EXHAUSTED' : 'RATE_LIMIT', 'Mureka rate limited the song-task request', delay);
    }
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Mureka song task is temporarily unavailable', delay);
    if (response.status === 404) return branch('notFound', 'NOT_FOUND', 'Mureka did not find the song task');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Mureka rejected the song-task request with HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Mureka returned invalid song-task metadata'); }
    if (!isRecord(document) || document.id !== input.taskId || typeof document.status !== 'string' || !validStatuses.has(document.status) || (document.model !== undefined && typeof document.model !== 'string')) return branch('invalidResponse', 'PROTOCOL', 'Mureka returned invalid song-task metadata');
    return newQueryBranch('found', { taskId: document.id, status: document.status, model: document.model ?? '' });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function getRetryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'kunlun', operation: 'getTaskStatus', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GetTaskStatusOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<GetTaskStatusOutput> { return newQueryRetry(failure(kind, message), delayMs); }
