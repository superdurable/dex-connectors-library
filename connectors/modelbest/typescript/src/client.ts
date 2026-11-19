// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ListModelsOutput } from './generated/models.js';

const endpoint = 'https://api.modelbest.cn/v1/models';
const emptyOutput: ListModelsOutput = { total: 0, modelIDs: [] };
/** Optional HTTP sender for deterministic provider tests. */
export interface ModelBestClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the model catalog with private credentials resolved per call. */
export class ModelBestClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting ModelBest. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ModelBestClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ModelBest configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('ModelBest request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only model-list Query. */ listModels(): ListModelsOperation { return new ListModelsOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!/^[!-~]+$/.test(apiKey) || apiKey.length > 1024) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: endpoint, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
/** Classify ModelBest model-list results without retaining secrets or metadata. */
export class ListModelsOperation implements Query<ListModelsInput, ListModelsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: ModelBestClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListModelsDefinition; }
  /** Read and validate the model catalog. */
  async invoke(call: Call, _input: ListModelsInput): Promise<QueryAttempt<ListModelsOutput>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Array.isArray(document.data)) return branch('invalidResponse', 'PROTOCOL');
    const models = document.data;
    const modelIDs: string[] = [];
    for (const model of models) {
      if (!isRecord(model) || typeof model.id !== 'string' || !model.id || model.id.length > 256 || /[\r\n\0]/.test(model.id)) return branch('invalidResponse', 'PROTOCOL');
      if (modelIDs.length < 50) modelIDs.push(model.id);
    }
    return newQueryBranch('found', { total: models.length, modelIDs });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'modelbest', operation: 'listModels', kind, message: 'ModelBest provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListModelsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListModelsOutput> { return newQueryRetry(failure(kind), delay); }
