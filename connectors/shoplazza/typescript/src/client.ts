// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetShopDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetShopInput, GetShopOutput } from './generated/models.js';

const emptyOutput: GetShopOutput = { id: 0, name: '', domain: '', currency: '' };
/** Optional provider transport for deterministic tests. */
export interface ShoplazzaClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read provider data through one credentialed Query. */
export class ShoplazzaClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connector settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ShoplazzaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Shoplazza configuration and credential source are required');
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.myshoplaza\.com$/.test(config.shopDomain)) throw new TypeError('Shoplazza shopDomain must be lowercase');
    this.baseUrl = `https://${config.shopDomain}`;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Shoplazza request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only provider Query. */ getShop(): GetShopOperation { return new GetShopOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge' | 'input'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!token || token.length > 2048 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams(); for (const field of ['id', 'name', 'domain', 'currency']) query.append('fields', field);
    const request: ProviderRequest = { url: `${this.baseUrl}/openapi/2027-01/shop?${query}`, method: 'GET', headers: { Accept: 'application/json', 'Access-Token': token }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
export class GetShopOperation implements Query<GetShopInput, GetShopOutput> {
  /** Bind one validated client. */ constructor(private readonly client: ShoplazzaClient) {}
  /** Return the generated operation contract. */ definition(): QueryDefinition { return GetShopDefinition; }
  /** Perform one safe read and classify its outcome. */
  async invoke(call: Call, input: GetShopInput): Promise<QueryAttempt<GetShopOutput>> {
    void input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    const data = isRecord(document) ? document.data : undefined;
    if (!isRecord(data) || !Number.isSafeInteger(data.id) || (data.id as number) <= 0 || typeof data.name !== 'string' || !data.name.trim() || typeof data.domain !== 'string' || !data.domain.trim() || typeof data.currency !== 'string' || data.currency.length !== 3) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { id: data.id as number, name: data.name, domain: data.domain, currency: data.currency });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'shoplazza', operation: 'getShop', kind, message: 'Shoplazza provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetShopOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetShopOutput> { return newQueryRetry(failure(kind), delay); }
