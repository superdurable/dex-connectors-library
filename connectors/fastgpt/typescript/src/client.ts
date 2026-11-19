// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetDatasetDetailsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetDatasetDetailsInput, GetDatasetDetailsOutput } from './generated/models.js';

/** Optional provider transport for local tests. */
export interface FastGPTClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read FastGPT knowledge base metadata with per-call credentials. */
export class FastGPTClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate settings without contacting FastGPT. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FastGPTClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('FastGPT configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://cloud.fastgpt.cn');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('FastGPT request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the knowledge base detail Query. */ getDatasetDetails(): GetDatasetDetailsOperation { return new GetDatasetDetailsOperation(this); }

  /** Send one bounded detail request. */
  async get(call: Call, id: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 512 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseURL}/api/core/dataset/detail?id=${encodeURIComponent(id)}`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Return one FastGPT knowledge base's metadata. */
export class GetDatasetDetailsOperation implements Query<GetDatasetDetailsInput, GetDatasetDetailsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FastGPTClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetDatasetDetailsDefinition; }
  /** Classify one provider result using the Go contract. */
  async invoke(call: Call, input: GetDatasetDetailsInput): Promise<QueryAttempt<GetDatasetDetailsOutput>> {
    const id = input?.id;
    if (typeof id !== 'string' || !id || Buffer.byteLength(id) > 128 || id.trim() !== id || /[/?#&]/.test(id)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.get(call, id);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if ((wire.code as number) >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (wire.code !== 200) return branch('providerRejected', wire.code === 401 ? 'AUTHENTICATION' : wire.code === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    const detail = wire.data;
    if (!isObject(detail) || detail._id !== id || !valid(detail.name, 512) || !valid(detail.type, 128) || !valid(detail.status, 128)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { id, name: detail.name, type: detail.type, status: detail.status });
  }
}

function valid(value: unknown, max: number): value is string { return typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= max; }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fastgpt', operation: 'getDatasetDetails', kind, message: 'FastGPT knowledge base query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetDatasetDetailsOutput> { return newQueryBranch(name, { id: '', name: '', type: '', status: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetDatasetDetailsOutput> { return newQueryRetry(failure(kind), delay); }
