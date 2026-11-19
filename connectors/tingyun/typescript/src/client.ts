// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListApplicationsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListApplicationsInput, ListApplicationsOutput } from './generated/models.js';

const emptyOutput: ListApplicationsOutput = { applications: [] };
/** Optional provider transport for deterministic tests. */
export interface TingyunClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class TingyunClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TingyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Tingyun configuration and credential source are required');
    const origin = new URL(config.apiHost); if (origin.protocol !== 'https:' || origin.username || origin.password || origin.port || origin.search || origin.hash || (origin.pathname !== '/' && origin.pathname !== '') || !origin.hostname.endsWith('.tingyun.com') || origin.hostname.includes('..')) throw new TypeError('Tingyun API host is invalid'); this.baseUrl = origin.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Tingyun request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ listApplications(): ListApplicationsOperation { return new ListApplicationsOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!key || key.length > 1024 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/app-api/app/list`, method: 'GET', headers: { Accept: 'application/json', Authorization: `token ${key}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify the provider response without storing credentials in Dex state. */
export class ListApplicationsOperation implements Query<ListApplicationsInput, ListApplicationsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: TingyunClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return ListApplicationsDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: ListApplicationsInput): Promise<QueryAttempt<ListApplicationsOutput>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!Array.isArray(document)) return branch('invalidResponse', 'PROTOCOL');
    const applications: NonNullable<ListApplicationsOutput['applications']> = [];
    for (const entry of document) { if (!isRecord(entry) || typeof entry.id !== 'number' || !Number.isSafeInteger(entry.id) || entry.id <= 0 || typeof entry.name !== 'string' || !entry.name) return branch('invalidResponse', 'PROTOCOL'); applications.push({ id: entry.id, name: entry.name }); }
    return newQueryBranch('listed', { applications });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tingyun', operation: 'listApplications', kind, message: 'Tingyun provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListApplicationsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListApplicationsOutput> { return newQueryRetry(failure(kind), delay); }
