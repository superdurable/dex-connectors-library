// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ListModelsOutput } from './generated/models.js';

const emptyOutput: ListModelsOutput = { total: 0, modelIDs: [] };
/** Optional provider transport for deterministic tests. */
export interface ShuntianchuangClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class ShuntianchuangClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ShuntianchuangClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Shuntianchuang configuration and credential source are required');
    this.baseUrl = 'https://api-model.shuntianchuang.cn';
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Shuntianchuang request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ listModels(): ListModelsOperation { return new ListModelsOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!key || key.length > 2048 || !/^[!-~]+$/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/v1/models`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${key}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
export class ListModelsOperation implements Query<ListModelsInput, ListModelsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: ShuntianchuangClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return ListModelsDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: ListModelsInput): Promise<QueryAttempt<ListModelsOutput>> {
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
    if (!isRecord(document) || document.object !== 'list' || !Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL');
    const modelIDs: string[] = [];
    for (const item of document.data) { if (!isRecord(item) || item.object !== 'model' || typeof item.id !== 'string' || !item.id || item.id.length > 256 || (/[\r\n]/.test(item.id) || item.id.includes(String.fromCharCode(0)))) return branch('invalidResponse', 'PROTOCOL'); if (modelIDs.length < 50) modelIDs.push(item.id); }
    return newQueryBranch('found', { total: document.data.length, modelIDs });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'shuntianchuang', operation: 'listModels', kind, message: 'Shuntianchuang provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListModelsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListModelsOutput> { return newQueryRetry(failure(kind), delay); }
