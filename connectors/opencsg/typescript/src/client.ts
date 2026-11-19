// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListGatewayModelsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListGatewayModelsInput, ListGatewayModelsOutput } from './generated/models.js';

/** Optional sender for provider tests. */
export interface OpenCSGClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read OpenCSG provider data with bounded HTTP responses. */
export class OpenCSGClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OpenCSGClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OpenCSG configuration and credentials are required');

    this.baseURL = validateBaseURL('https://ai.space.opencsg.com').replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 15000;
    this.maxResponseBytes = config.maxResponseBytes || 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('invalid request limits');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the listGatewayModels Query. */
  listGatewayModels(): ListGatewayModelsOperation { return new ListGatewayModelsOperation(this); }
  /** Resolve credentials and send one provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const fields = await this.credentials.resolve(call, call.context.cancellationSignal); key = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 512 || key.split('').some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return { error: 'credential', retryAfterMs: 0 };
    const url = 'https://ai.space.opencsg.com/v1/models';
    const method = 'GET'; const headers = { Accept: 'application/json', Authorization: `Bearer ${key}` }; const body = undefined;
    const providerRequest: ProviderRequest = { url, method, headers, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(providerRequest); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify OpenCSG provider replies as Query branches or retries. */
export class ListGatewayModelsOperation implements Query<ListGatewayModelsInput, ListGatewayModelsOutput> {
  /** Bind one validated provider client. */ constructor(private readonly client: OpenCSGClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListGatewayModelsDefinition; }
  /** Execute one read-only provider Query. */
  async invoke(call: Call, input: ListGatewayModelsInput): Promise<QueryAttempt<ListGatewayModelsOutput>> {
    void input;
    const branch = (name: string, kind: Failure['kind']): QueryAttempt<ListGatewayModelsOutput> => newQueryBranch(name, { total: 0, modelIDs: [] }, {}, failure(kind));
    const retry = (kind: Failure['kind'], delay = 0): QueryAttempt<ListGatewayModelsOutput> => newQueryRetry(failure(kind), delay);
    const bad = (): QueryAttempt<ListGatewayModelsOutput> => branch('invalidResponse', 'PROTOCOL');

    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response; const retryAfterMs = exchange.retryAfterMs;
    if (status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return bad(); }
    if (!isRecord(wire) || !Array.isArray(wire.data)) return bad();
    const modelIDs: string[] = [];
    for (const item of wire.data) {
      if (!isRecord(item) || typeof item.id !== 'string' || !item.id || item.id.length > 256 || /[\r\n\0]/.test(item.id)) return bad();
      if (modelIDs.length < 50) modelIDs.push(item.id);
    }
    return newQueryBranch('found', { total: wire.data.length, modelIDs });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'opencsg', operation: 'listGatewayModels', kind, message: 'OpenCSG listGatewayModels failed' }; }
