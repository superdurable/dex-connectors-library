// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetShopBasicDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetShopBasicInput, ShopBasic } from './generated/models.js';

/** Optional HTTP sender for local provider tests. */
export interface YouzanClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read the shop authorized by a resolved Youzan access token. */
export class YouzanClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the API origin and request bounds without a provider call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: YouzanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Youzan configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://open.youzanyun.com';
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Youzan baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Youzan request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the authorized-shop Query. */ getShopBasic(): GetShopBasicOperation { return new GetShopBasicOperation(this); }

  /** Submit one Youzan shop query with current credentials. */
  async read(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 4096 || !/^[!-~]+$/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const url = this.baseUrl.replace(/\/$/, '') + '/api/youzan.shop.basic.get/3.0.0?' + new URLSearchParams({ access_token: token }).toString();
    const request: ProviderRequest = { url, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' }, body: new TextEncoder().encode('{}'), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify shop data, provider rejection, and temporary failure. */
export class GetShopBasicOperation implements Query<GetShopBasicInput, ShopBasic> {
  /** Bind a validated Youzan client. */ constructor(private readonly client: YouzanClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetShopBasicDefinition; }
  /** Query the current shop and preserve the Go branch contract. */
  async invoke(call: Call): Promise<QueryAttempt<ShopBasic>> {
    const exchange = await this.client.read(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || typeof wire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!wire.success) return wire.code === 161400100 ? retry('AVAILABILITY', exchange.retryAfterMs) : branch('providerRejected', 'PROVIDER_REJECTION');
    if (wire.code !== 200 || !isObject(wire.data)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire.data;
    if (!Number.isSafeInteger(data.sid) || (data.sid as number) <= 0 || typeof data.name !== 'string' || !data.name) return branch('invalidResponse', 'PROTOCOL');
    const output: ShopBasic = { shopID: data.sid as number, name: data.name, role: typeof data.role === 'number' ? data.role : 0, lineOfBusiness: typeof data.line_of_business === 'number' ? data.line_of_business : 0 };
    if (typeof data.logo === 'string' && data.logo) output.logo = data.logo;
    if (typeof data.url === 'string' && data.url) output.url = data.url;
    return newQueryBranch('found', output);
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'youzan', operation: 'getShopBasic', kind, message: 'Youzan shop query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ShopBasic> { return newQueryBranch(name, { shopID: 0, name: '', role: 0, lineOfBusiness: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ShopBasic> { return newQueryRetry(failure(kind), delay); }
