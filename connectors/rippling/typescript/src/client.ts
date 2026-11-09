// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetWorkerDefinition } from './generated/definitions.js';
import type { Config, Credentials, WorkerSummary, GetWorkerInput } from './generated/models.js';

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

  /** Resolve the API token and send one bounded worker lookup. */
  async get(call: Call, workerID: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/workers/${encodeURIComponent(workerID)}/`, method: 'GET',
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
      return terminal('defect', 'VALIDATION', 'worker ID must contain 1–64 URL-safe characters');
    const exchange = await this.client.get(call, input.workerId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Rippling credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Rippling request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Rippling rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Rippling is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Rippling did not expose the worker');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Rippling rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Rippling response exceeded the configured limit');
    const workerSummary = decodeWorker(exchange.body, input.workerId);
    if (!workerSummary) return terminal('invalidResponse', 'PROTOCOL', 'Rippling returned invalid worker JSON');
    return newQueryBranch('found', workerSummary);
  }
}

function isValidWorkerID(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'rippling', operation: 'getWorker', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<WorkerSummary> { return newQueryBranch(branch, emptyWorker(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<WorkerSummary> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeWorker(body: Uint8Array, workerID: string): WorkerSummary | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (raw.id !== workerID || (raw.status != null && typeof raw.status !== 'string') ||
    (raw.title != null && typeof raw.title !== 'string')) return undefined;
  return { id: workerID, status: typeof raw.status === 'string' ? raw.status : '',
    title: typeof raw.title === 'string' ? raw.title : '' };
}
