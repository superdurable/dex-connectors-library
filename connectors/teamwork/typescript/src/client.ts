// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTaskDefinition } from './generated/definitions.js';
import type { Config, Credentials, TaskSummary, GetTaskInput } from './generated/models.js';

const emptyTask = (): TaskSummary => ({ id: '', status: '', name: '' });

/** Injectable provider sender for tests and local applications. */
export interface TeamworkClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Teamwork REST API client for task metadata. */
export class TeamworkClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Teamwork. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TeamworkClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Teamwork configuration and credentials are required');
    if (!config.baseUrl) throw new TypeError('Teamwork site URL is required');
    this.baseURL = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Teamwork request limits must be positive integers');
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
      url: `${this.baseURL}/projects/api/v3/tasks/${encodeURIComponent(taskID)}.json`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiToken}:`).toString('base64')}` },
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

/** Read one task's ID, name, and status. */
export class GetTaskOperation implements Query<GetTaskInput, TaskSummary> {
  /** Bind the provider client. */ constructor(private readonly client: TeamworkClient) {}
  /** Return the generated Query task. */ definition(): QueryDefinition { return GetTaskDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<TaskSummary>> {
    if (!isValidTaskID(input?.taskId))
      return terminal('defect', 'VALIDATION', 'task ID must be a positive decimal integer');
    const exchange = await this.client.get(call, input.taskId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Teamwork credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Teamwork request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Teamwork rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Teamwork is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Teamwork did not expose the task');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Teamwork rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Teamwork response exceeded the configured limit');
    const taskSummary = decodeTask(exchange.body, input.taskId);
    if (!taskSummary) return terminal('invalidResponse', 'PROTOCOL', 'Teamwork returned invalid task JSON');
    return newQueryBranch('found', taskSummary);
  }
}

function isValidTaskID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9007199254740991n;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'teamwork', operation: 'getTask', kind, message }; }
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
  if (!isObject(raw.task) || typeof raw.task.id !== 'number' || !Number.isSafeInteger(raw.task.id) ||
    String(raw.task.id) !== taskID || typeof raw.task.status !== 'string' || !raw.task.status.trim() ||
    typeof raw.task.name !== 'string' || !raw.task.name.trim()) return undefined;
  return { id: taskID, status: raw.task.status, name: raw.task.name };
}
