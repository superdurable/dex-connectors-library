// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTaskDefinition } from './generated/definitions.js';
import type { Config, Credentials, TaskStatus, GetTaskInput } from './generated/models.js';

const defaultBaseURL = 'https://www.wrike.com/api/v4';
const emptyTask = (): TaskStatus => ({ id: '', status: '', title: '' });

/** Injectable provider sender for tests and local applications. */
export interface WrikeClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Wrike REST API client for task metadata. */
export class WrikeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Wrike. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WrikeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Wrike configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Wrike request limits must be positive integers');
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

/** Read one task's ID, title, and status. */
export class GetTaskOperation implements Query<GetTaskInput, TaskStatus> {
  /** Bind the provider client. */ constructor(private readonly client: WrikeClient) {}
  /** Return the generated Query task. */ definition(): QueryDefinition { return GetTaskDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetTaskInput): Promise<QueryAttempt<TaskStatus>> {
    if (!isValidTaskID(input?.taskId))
      return terminal('defect', 'VALIDATION', 'task ID must be 1–256 URL-safe characters');
    const exchange = await this.client.get(call, input.taskId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Wrike credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Wrike request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Wrike rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Wrike is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Wrike did not expose the task');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Wrike rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Wrike response exceeded the configured limit');
    const taskStatus = decodeTask(exchange.body, input.taskId);
    if (!taskStatus) return terminal('invalidResponse', 'PROTOCOL', 'Wrike returned invalid task JSON');
    return newQueryBranch('found', taskStatus);
  }
}

function isValidTaskID(value: unknown): value is string {
  return typeof value === 'string' && /^[a-zA-Z0-9_:.=-]{1,256}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'wrike', operation: 'getTask', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<TaskStatus> { return newQueryBranch(branch, emptyTask(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<TaskStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeTask(body: Uint8Array, taskID: string): TaskStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.kind !== 'tasks' || !Array.isArray(raw.data) || raw.data.length !== 1 || !isObject(raw.data[0])) return undefined;
  const data = raw.data[0];
  if (data.id !== taskID || typeof data.status !== 'string' || !data.status.trim() ||
    typeof data.title !== 'string' || !data.title.trim()) return undefined;
  return { id: taskID, status: data.status, title: data.title };
}
