// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ModelPage } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface MoxinClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: ModelPage = { models: [] };
/** Resolve Moxin credentials for each model-list query. */
export class MoxinClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings without contacting Moxin. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MoxinClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Moxin settings and credentials are required');
    this.baseUrl = config.baseUrl || 'https://api.kokoniworld.com';
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    const url = new URL(this.baseUrl);
    if (url.protocol !== 'https:' || (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Moxin settings are invalid');
    this.baseUrl = url.origin;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the model-list Query. */ listModels(): ListModelsOperation { return new ListModelsOperation(this); }
  /** Send one bounded request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); token = resolved.platformToken instanceof SecretString ? resolved.platformToken.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 2048 || /[^\x21-\x7e]/.test(token) || /^bearer /i.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/v1/models`, method: 'GET', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}
/** List authorized model identifiers. */
export class ListModelsOperation implements Query<ListModelsInput, ModelPage> {
  /** Bind the provider client. */ constructor(private readonly client: MoxinClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListModelsDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, _input: ListModelsInput): Promise<QueryAttempt<ModelPage>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const record = wire as Record<string, unknown>;
    if (record.object !== 'list' || !Array.isArray(record.data) || record.data.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const models: NonNullable<ModelPage['models']> = [];
    for (const item of record.data) {
      if (!item || typeof item !== 'object' || Array.isArray(item) || typeof item.id !== 'string' || !item.id || item.object !== 'model') return branch('invalidResponse', 'PROTOCOL');
      models.push({ id: item.id });
    }
    return newQueryBranch('listed', { models });
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'moxin', operation: 'listModels', kind, message: 'Moxin model-list query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ModelPage> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ModelPage> { return newQueryRetry(failure(kind), delay); }
