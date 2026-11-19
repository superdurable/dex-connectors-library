// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProvincesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProvincesInput, ListProvincesOutput, Province } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface JisuAPIClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve JisuAPI credentials for each province catalog query. */
export class JisuAPIClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting JisuAPI. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JisuAPIClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JisuAPI configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('JisuAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the province catalog Query. */ listProvinces(): ListProvincesOperation { return new ListProvincesOperation(this); }

  /** Send one bounded province catalog request. */
  async getProvinces(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appkey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); appkey = credential.appkey instanceof SecretString ? credential.appkey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!appkey || appkey.length > 1024 || /[^\x21-\x7e]/.test(appkey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = {
      url: 'https://api.jisuapi.com/area/province?appkey=' + encodeURIComponent(appkey),
      method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs,
      maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}

/** Read province IDs and names without exposing other provider fields. */
export class ListProvincesOperation implements Query<ListProvincesInput, ListProvincesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: JisuAPIClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListProvincesDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: ListProvincesInput): Promise<QueryAttempt<ListProvincesOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getProvinces(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire as { status?: unknown; result?: unknown };
    if (!Number.isInteger(data.status)) return branch('invalidResponse', 'PROTOCOL');
    if (data.status === 104 || data.status === 106) return retry('RATE_LIMIT');
    if (data.status === 107) return retry('AVAILABILITY');
    if (data.status !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(data.result)) return branch('invalidResponse', 'PROTOCOL');
    const provinces: Province[] = [];
    for (const rawProvince of data.result) {
      if (!rawProvince || typeof rawProvince !== 'object' || Array.isArray(rawProvince)) return branch('invalidResponse', 'PROTOCOL');
      const province = rawProvince as { id?: unknown; name?: unknown };
      if (typeof province.id !== 'string' || !province.id || typeof province.name !== 'string' || !province.name) return branch('invalidResponse', 'PROTOCOL');
      provinces.push({ id: province.id, name: province.name });
    }
    return newQueryBranch('listed', { provinces });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'jisuapi', operation: 'listProvinces', kind, message: 'JisuAPI province catalog query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListProvincesOutput> { return newQueryBranch(name, { provinces: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListProvincesOutput> { return newQueryRetry(failure(kind), delay); }
