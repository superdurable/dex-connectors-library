// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetJobRunDefinition } from './generated/definitions.js';
import type { Config, Credentials, JobRunStatus, GetJobRunInput } from './generated/models.js';

const emptyJobRun = (): JobRunStatus => ({ id: '', status: '', resultState: '' });

/** Injectable provider sender for tests and local applications. */
export interface DatabricksClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Databricks REST API client for job_run metadata. */
export class DatabricksClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Databricks. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DatabricksClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Databricks configuration and credentials are required');
    if (!config.baseUrl) throw new TypeError('Databricks workspace URL is required');
    this.baseURL = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Databricks request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getJobRun Query. */ getJobRun(): GetJobRunOperation { return new GetJobRunOperation(this); }

  /** Resolve the API token and send one bounded job_run lookup. */
  async get(call: Call, runID: string): Promise<Exchange> {
    let apiToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiToken = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiToken || !isHeaderSafeCredential(apiToken)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/api/2.2/jobs/runs/get?run_id=${encodeURIComponent(runID)}`, method: 'GET',
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

/** Read one job_run's ID, status, and result state. */
export class GetJobRunOperation implements Query<GetJobRunInput, JobRunStatus> {
  /** Bind the provider client. */ constructor(private readonly client: DatabricksClient) {}
  /** Return the generated Query job_run. */ definition(): QueryDefinition { return GetJobRunDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetJobRunInput): Promise<QueryAttempt<JobRunStatus>> {
    if (!isValidRunID(input?.runId))
      return terminal('defect', 'VALIDATION', 'job_run ID must be a positive decimal integer');
    const exchange = await this.client.get(call, input.runId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Databricks credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Databricks request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Databricks rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Databricks is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Databricks did not expose the job_run');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Databricks rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Databricks response exceeded the configured limit');
    const jobRunStatus = decodeJobRun(exchange.body, input.runId);
    if (!jobRunStatus) return terminal('invalidResponse', 'PROTOCOL', 'Databricks returned invalid job_run JSON');
    return newQueryBranch('found', jobRunStatus);
  }
}

function isValidRunID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9007199254740991n;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'databricks', operation: 'getJobRun', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<JobRunStatus> { return newQueryBranch(branch, emptyJobRun(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<JobRunStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeJobRun(body: Uint8Array, runID: string): JobRunStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw)) return undefined;
  if (typeof raw.run_id !== 'number' || !Number.isSafeInteger(raw.run_id) || String(raw.run_id) !== runID ||
    !isObject(raw.status) || typeof raw.status.state !== 'string' || !raw.status.state.trim()) return undefined;
  const resultState = isObject(raw.state) ? raw.state.result_state ?? '' : '';
  if (typeof resultState !== 'string') return undefined;
  return { id: runID, status: raw.status.state, resultState };
}
