// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListJobFunctionsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListJobFunctionsInput, ListJobFunctionsOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface MokaClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: ListJobFunctionsOutput = { functions: [] };

/** Resolve Moka credentials for each job-function query. */
export class MokaClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly orgId: string;
  private readonly mode: string;
  /** Validate settings without contacting Moka. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MokaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Moka settings and credentials are required');
    this.orgId = config.orgId;
    this.mode = config.mode || 'social';
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(this.orgId) || !['social', 'campus'].includes(this.mode) || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Moka settings are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only job-function Query. */ listJobFunctions(): ListJobFunctionsOperation { return new ListJobFunctionsOperation(this); }
  /** Send one bounded request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL(`https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/${encodeURIComponent(this.orgId)}`);
    url.searchParams.set('mode', this.mode);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List recruitment job-function metadata. */
export class ListJobFunctionsOperation implements Query<ListJobFunctionsInput, ListJobFunctionsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: MokaClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListJobFunctionsDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, _input: ListJobFunctionsInput): Promise<QueryAttempt<ListJobFunctionsOutput>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire) || !Array.isArray((wire as { jobs?: unknown }).jobs)) return branch('invalidResponse', 'PROTOCOL');
    const jobs = (wire as { jobs: unknown[] }).jobs;
    const functions: NonNullable<ListJobFunctionsOutput['functions']> = [];
    for (const item of jobs) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return branch('invalidResponse', 'PROTOCOL');
      const record = item as Record<string, unknown>;
      if (!Number.isSafeInteger(record.id) || Number(record.id) <= 0 || typeof record.label !== 'string' || !record.label || !Number.isSafeInteger(record.jobCount) || Number(record.jobCount) < 0 || (record.parentId != null && (!Number.isSafeInteger(record.parentId) || Number(record.parentId) < 0))) return branch('invalidResponse', 'PROTOCOL');
      functions.push({ id: Number(record.id), label: record.label, parentId: Number(record.parentId ?? 0), jobCount: Number(record.jobCount) });
    }
    return newQueryBranch('found', { functions });
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'moka', operation: 'listJobFunctions', kind, message: 'Moka job-function query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListJobFunctionsOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListJobFunctionsOutput> { return newQueryRetry(failure(kind), delay); }
