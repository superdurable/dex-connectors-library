// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetShopBasicDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, GetShopBasicInput, ShopBasic } from './generated/models.js';

/** Provider transport override for deterministic tests. */
export interface YouzanClientOptions { /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Youzan Cloud client for one authorized shop. */
export class YouzanClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the API origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: YouzanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Youzan configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://open.youzanyun.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Youzan API URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Youzan request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getShopBasic Query. */ getShopBasic(): GetShopBasicOperation { return new GetShopBasicOperation(this); }

  /** Resolve the token and send one bounded provider request. */
  async requestShop(call: Call): Promise<{ response?: ProviderResponse; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean }> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (!validToken(token)) return { credentialFailed: true };
    const url = new URL(this.baseURL + '/api/youzan.shop.basic.get/3.0.0');
    url.searchParams.set('access_token', token);
    const request: ProviderRequest = {
      url: url.toString(), method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: new TextEncoder().encode('{}'), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }
}

/** Read the shop authorized by the connection token. */
export class GetShopBasicOperation implements Query<GetShopBasicInput, ShopBasic> {
  /** Bind the client. */ constructor(private readonly client: YouzanClient) {}
  /** Return the generated Query definition. */ definition() { return GetShopBasicDefinition; }
  /** Classify the provider's shop response. */
  async invoke(call: Call, input: GetShopBasicInput): Promise<QueryAttempt<ShopBasic>> {
    void input;
    const result = await this.client.requestShop(call);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'Youzan access token is unavailable or invalid');
    if (result.transportFailed || !result.response) return newQueryRetry(failure('TRANSPORT', 'Youzan request failed'));
    const response = result.response;
    const retryAfter = parseRetryAfter(headerValue(response.headers, 'retry-after')) || 0;
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Youzan rate limited the request'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Youzan is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Youzan response exceeded the configured limit');
    if (response.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', 'Youzan rejected HTTP ' + response.status);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Youzan returned invalid shop JSON'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'Youzan returned invalid shop JSON');
    const wire = value as Record<string, unknown>;
    if (wire.success === false && wire.code === 161400100) return newQueryRetry(failure('AVAILABILITY', 'Youzan reported a system error'), retryAfter);
    if (typeof wire.success !== 'boolean') return terminal('invalidResponse', 'PROTOCOL', 'Youzan omitted success status');
    if (!wire.success) return terminal('providerRejected', 'PROVIDER_REJECTION', 'Youzan rejected code ' + String(wire.code ?? 0));
    if (wire.code !== 200 || !wire.data || typeof wire.data !== 'object' || Array.isArray(wire.data))
      return terminal('invalidResponse', 'PROTOCOL', 'Youzan returned incomplete shop information');
    const data = wire.data as Record<string, unknown>;
    if (!Number.isSafeInteger(data.sid) || (data.sid as number) <= 0 || typeof data.name !== 'string' || !data.name)
      return terminal('invalidResponse', 'PROTOCOL', 'Youzan returned incomplete shop information');
    const shop: ShopBasic = {
      shopID: data.sid as number, name: data.name, role: Number.isSafeInteger(data.role) ? data.role as number : 0,
      lineOfBusiness: Number.isSafeInteger(data.line_of_business) ? data.line_of_business as number : 0,
      ...(typeof data.logo === 'string' && data.logo ? { logo: data.logo } : {}),
      ...(typeof data.url === 'string' && data.url ? { url: data.url } : {}),
    };
    return newQueryBranch('found', shop, zeroReceipt());
  }
}

function validToken(value: string): boolean { return value.length > 0 && value.length <= 4096 && [...value].every(character => character.charCodeAt(0) >= 33 && character.charCodeAt(0) <= 126); }
function headerValue(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'youzan', operation: 'getShopBasic', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ShopBasic> { return newQueryBranch(branch, { shopID: 0, name: '', role: 0, lineOfBusiness: 0 }, zeroReceipt(), failure(kind, message)); }
