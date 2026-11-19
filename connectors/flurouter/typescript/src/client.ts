// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListModelsInput, ListModelsOutput } from './generated/models.js';

const modelsEndpoint = 'https://flurouter.net/v1/models';

/** Optional provider sender for local tests. */
export interface FlurouterClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read model identifiers visible to the connected FluRouter key. */
export class FlurouterClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local limits without contacting FluRouter. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FlurouterClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('FluRouter configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('FluRouter request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the model-list Query. */ listModels(): ListModelsOperation { return new ListModelsOperation(this); }

  /** Send one bounded model-list request. */
  async list(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey.startsWith('sk-il-') || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: modelsEndpoint, method: 'GET', headers: { Authorization: 'Bearer ' + apiKey, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read the current FluRouter model catalog. */
export class ListModelsOperation implements Query<ListModelsInput, ListModelsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FlurouterClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListModelsDefinition; }
  /** Classify one model-list request using the Go connector contract. */
  async invoke(call: Call): Promise<QueryAttempt<ListModelsOutput>> {
    const exchange = await this.client.list(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || wire.object !== 'list' || !Array.isArray(wire.data) || wire.data.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const modelIDs: string[] = [];
    const seen = new Set<string>();
    for (const model of wire.data) {
      if (!isObject(model) || typeof model.id !== 'string' || !model.id || model.id.length > 256 || /[^\x21-\x7e]/.test(model.id) || model.object !== 'model' || seen.has(model.id)) return branch('invalidResponse', 'PROTOCOL');
      seen.add(model.id);
      modelIDs.push(model.id);
    }
    return newQueryBranch('found', { modelIDs });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'flurouter', operation: 'listModels', kind, message: 'FluRouter model query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListModelsOutput> { return newQueryBranch(name, { modelIDs: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListModelsOutput> { return newQueryRetry(failure(kind), delay); }
