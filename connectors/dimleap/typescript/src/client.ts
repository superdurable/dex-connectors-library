// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ListModelsOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface DimleapClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Dimleap credentials for each model catalog query. */
export class DimleapClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Dimleap. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DimleapClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dimleap configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Dimleap request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the model catalog Query. */ listModels(): ListModelsOperation { return new ListModelsOperation(this); }

  /** Send one bounded model catalog request. */
  async getModels(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: 'https://api.dimleap.cn/v1/models', method: 'GET', headers: { Accept: 'application/json', Authorization: 'Bearer ' + apiKey }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List model IDs without invoking inference. */
export class ListModelsOperation implements Query<ListModelsInput, ListModelsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DimleapClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListModelsDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: ListModelsInput): Promise<QueryAttempt<ListModelsOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getModels(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const catalog = wire as { object?: unknown; data?: unknown };
    if (catalog.object !== 'list' || !Array.isArray(catalog.data)) return branch('invalidResponse', 'PROTOCOL');
    const modelIDs: string[] = [];
    for (const entry of catalog.data) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return branch('invalidResponse', 'PROTOCOL');
      const model = entry as { object?: unknown; id?: unknown };
      if (model.object !== 'model' || typeof model.id !== 'string' || !model.id || model.id.length > 256 || /[\r\n]/.test(model.id) || model.id.includes(String.fromCharCode(0))) return branch('invalidResponse', 'PROTOCOL');
      if (modelIDs.length < 50) modelIDs.push(model.id);
    }
    return newQueryBranch('found', { total: catalog.data.length, modelIDs });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'dimleap', operation: 'listModels', kind, message: 'Dimleap model catalog query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListModelsOutput> { return newQueryBranch(name, { total: 0, modelIDs: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListModelsOutput> { return newQueryRetry(failure(kind), delay); }
