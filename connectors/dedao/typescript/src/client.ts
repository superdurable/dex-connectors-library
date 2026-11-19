// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetNoteTaskStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetNoteTaskStatusInput, GetNoteTaskStatusOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface DedaoClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Get Note credentials for each task status query. */
export class DedaoClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Get Note. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DedaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Get Note configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Get Note request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the task status Query. */ getNoteTaskStatus(): GetNoteTaskStatusOperation { return new GetNoteTaskStatusOperation(this); }

  /** Send one bounded task status request. */
  async getStatus(call: Call, taskID: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    let clientID = '';
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : '';
      clientID = credential.clientID instanceof SecretString ? credential.clientID.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(apiKey) || !validCredential(clientID)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: 'https://openapi.biji.com/open/api/v1/resource/note/task/progress', method: 'POST', headers: { Accept: 'application/json', Authorization: apiKey, 'X-Client-ID': clientID, 'Content-Type': 'application/json' }, body: new TextEncoder().encode(JSON.stringify({ task_id: taskID })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one asynchronous note task's current status. */
export class GetNoteTaskStatusOperation implements Query<GetNoteTaskStatusInput, GetNoteTaskStatusOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DedaoClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetNoteTaskStatusDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: GetNoteTaskStatusInput): Promise<QueryAttempt<GetNoteTaskStatusOutput>> {
    if (!input || typeof input.taskID !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(input.taskID)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getStatus(call, input.taskID);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire as { success?: unknown; data?: unknown };
    if (typeof result.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!result.success) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!result.data || typeof result.data !== 'object' || Array.isArray(result.data)) return branch('invalidResponse', 'PROTOCOL');
    const data = result.data as { status?: unknown; note_id?: unknown };
    if (!['pending', 'processing', 'success', 'failed'].includes(String(data.status)) || (data.note_id !== undefined && (typeof data.note_id !== 'string' || data.note_id.length > 128))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { taskID: input.taskID, status: data.status as string, noteID: (data.note_id as string | undefined) ?? '' });
  }
}

function validCredential(value: string): boolean { return !!value && value.length <= 1024 && !/[^\x21-\x7e]/.test(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'dedao', operation: 'getNoteTaskStatus', kind, message: 'Get Note task status query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetNoteTaskStatusOutput> { return newQueryBranch(name, { taskID: '', status: '', noteID: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetNoteTaskStatusOutput> { return newQueryRetry(failure(kind), delay); }
