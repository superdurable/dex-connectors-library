// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetNoteTaskStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetNoteTaskStatusInput, GetNoteTaskStatusOutput } from './generated/models.js';

const endpoint = 'https://openapi.biji.com/open/api/v1/resource/note/task/progress';
const emptyOutput = (): GetNoteTaskStatusOutput => ({ taskID: '', status: '', noteID: '' });
/** Injectable provider HTTP sender. */
export type DedaoRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Transport dependency for tests and approved proxies. */
export interface DedaoClientOptions { /** SDK sender when omitted. */ send?: DedaoRequestSender; }
/** Authenticated Get Note task status client. */
export class DedaoClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: DedaoRequestSender;
  /** Validate bounded task request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DedaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dedao configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Dedao request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the getNoteTaskStatus Query. */ getNoteTaskStatus(): GetNoteTaskStatusOperation { return new GetNoteTaskStatusOperation(this); }
  /** Authenticate and dispatch one task status request. */
  async read(call: Call, input: GetNoteTaskStatusInput): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    if (typeof input?.taskID !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(input.taskID)) return { branch: 'defect', failure: failure('VALIDATION', 'Get Note task ID is invalid') };
    let apiKey: string; let clientID: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
      clientID = credentials.clientID instanceof SecretString ? credentials.clientID.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'Get Note credentials are unavailable or invalid') }; }
    if (!validCredential(apiKey) || !validCredential(clientID)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'Get Note credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: endpoint, method: 'POST',
      headers: { Authorization: apiKey, 'X-Client-ID': clientID, 'Content-Type': 'application/json', Accept: 'application/json' },
      body: new TextEncoder().encode(JSON.stringify({ task_id: input.taskID })),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusRetry(error.status, error.headers ?? {});
        if (error.status !== 200) return { branch: 'providerRejected', failure: failure(error.status === 401 ? 'AUTHENTICATION' : error.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Get Note rejected task request with HTTP ${error.status}`) };
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Get Note task response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'Get Note task request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusRetry(response.status, response.headers);
    if (response.status !== 200) return { branch: 'providerRejected', failure: failure(response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Get Note rejected task request with HTTP ${response.status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Get Note task response exceeded the configured limit') };
    return { response };
  }
}
/** Read one asynchronous Get Note task's processing state. */
export class GetNoteTaskStatusOperation implements Query<GetNoteTaskStatusInput, GetNoteTaskStatusOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DedaoClient) {}
  /** Generated Query contract. */ definition(): QueryDefinition { return GetNoteTaskStatusDefinition; }
  /** Classify the task state without reading note content. */
  async invoke(call: Call, input: GetNoteTaskStatusInput): Promise<QueryAttempt<GetNoteTaskStatusOutput>> {
    const exchange = await this.client.read(call, input);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyOutput(), {}, exchange.failure);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(exchange.response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned invalid task data'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned invalid task data');
    const fields = wire as Record<string, unknown>;
    if (typeof fields.success !== 'boolean') return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned invalid task data');
    if (!fields.success) return terminal('providerRejected', 'PROVIDER_REJECTION', 'Get Note rejected the task request');
    if (!fields.data || typeof fields.data !== 'object' || Array.isArray(fields.data)) return terminal('invalidResponse', 'PROTOCOL', 'Get Note omitted task data');
    const data = fields.data as Record<string, unknown>;
    if (!['pending', 'processing', 'success', 'failed'].includes(data.status as string)) return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned unknown task status');
    if (data.note_id !== undefined && data.note_id !== null && typeof data.note_id !== 'string') return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned invalid note ID');
    const noteID = data.note_id ?? '';
    if (typeof noteID !== 'string' || Buffer.byteLength(noteID) > 128) return terminal('invalidResponse', 'PROTOCOL', 'Get Note returned invalid note ID');
    return newQueryBranch('found', { taskID: input.taskID, status: data.status as string, noteID });
  }
}
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'dedao', operation: 'getNoteTaskStatus', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetNoteTaskStatusOutput> { return newQueryBranch(branch, emptyOutput(), {}, failure(kind, message)); }
function statusRetry(status: number, headers: ProviderResponse['headers']): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', new Date()),
    failure: status === 429 ? failure('RATE_LIMIT', 'Get Note rate limited task request') : failure('AVAILABILITY', 'Get Note API is temporarily unavailable') };
}
