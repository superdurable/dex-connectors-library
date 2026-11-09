// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTaskDefinition } from './generated/definitions.js';
import type { Config, Credentials, TaskSummary, GetTaskInput } from './generated/models.js';

const defaultBaseURL = 'https://www.meistertask.com/api';
const emptyTask = (): TaskSummary => ({ id: '', status: '', name: '' });

/** Injectable provider sender for tests and local applications. */
export interface MeisterTaskClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated MeisterTask REST API client for task metadata. */
export class MeisterTaskClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting MeisterTask. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MeisterTaskClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('MeisterTask configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('MeisterTask request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getTask Query. */ getTask(): GetTaskOperation { return new GetTaskOperation(this); }

  /** Resolve the API token and send one bounded task lookup. */
  async get(call: Call, taskID: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/tasks/${encodeURIComponent(taskID)}`, method: 'GET',
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

/** Read one task's ID, status, and name. */
export class GetTaskOperation implements Query<GetTaskInput, TaskSummary> {
  /** Bind the provider client. */ constructor(private readonly client: MeisterTaskClient) {}
  /** Return the generated Query task. */ definition(): QueryDefinition { return GetTaskDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<TaskSummary>> {
    if (!isValidTaskID(input?.taskId))
      return terminal('defect', 'VALIDATION', 'task ID must be a positive 32-bit decimal integer');
    const exchange = await this.client.get(call, input.taskId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'MeisterTask credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'MeisterTask request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'MeisterTask rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'MeisterTask is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'MeisterTask did not expose the task');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `MeisterTask rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'MeisterTask response exceeded the configured limit');
    const taskSummary = decodeTask(exchange.body, input.taskId);
    if (!taskSummary) return terminal('invalidResponse', 'PROTOCOL', 'MeisterTask returned invalid task JSON');
    return newQueryBranch('found', taskSummary);
  }
}

function isValidTaskID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,9}$/.test(value) && Number(value) <= 2147483647;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'meistertask', operation: 'getTask', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TaskSummary> { return newQueryBranch(branch, emptyTask(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TaskSummary> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTask(body: Uint8Array, taskID: string): TaskSummary | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (raw.id !== Number(taskID) || !Number.isSafeInteger(raw.status) || Number(raw.status) < 0 ||
    typeof raw.name !== 'string' || !raw.name) return undefined;
  return { id: taskID, status: String(raw.status), name: raw.name };
}
