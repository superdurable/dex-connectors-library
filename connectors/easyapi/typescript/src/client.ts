// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryShopDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryShopInput, ShopResult } from './generated/models.js';

const emptyResult: ShopResult = { shopId: 0, name: '', apiState: 0, endTime: '' };

/** Optional sender for isolated conformance replay. */
export interface EasyapiClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read one authorized company's invoicing service metadata. */
export class EasyapiClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting EasyAPI. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: EasyapiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('EasyAPI configuration and credentials are required');
    const selectedBaseUrl = config.baseUrl || 'https://fapiao-api.easyapi.com';
    let parsed: URL;
    try { parsed = new URL(selectedBaseUrl); } catch { throw new TypeError('EasyAPI base URL is invalid'); }
    if (!['https:', 'http:'].includes(parsed.protocol) || (parsed.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)) || !parsed.hostname || parsed.username || parsed.password || parsed.search || parsed.hash || selectedBaseUrl.endsWith('/')) throw new TypeError('EasyAPI base URL is invalid');
    this.baseUrl = selectedBaseUrl;
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('EasyAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the company Query. */ queryShop(): QueryShopOperation { return new QueryShopOperation(this); }

  /** Send one bounded shop query. */
  async readShop(call: Call, taxNumber: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appKey = '', appSecret = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
      appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(appKey) || !validCredential(appSecret)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ appKey, appSecret, taxNumber });
    const request: ProviderRequest = { url: this.baseUrl + '/shop/query?' + query.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one company's non-sensitive service configuration. */
export class QueryShopOperation implements Query<QueryShopInput, ShopResult> {
  /** Bind the provider client. */ constructor(private readonly client: EasyapiClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryShopDefinition; }
  /** Classify one company read according to the Go connector contract. */
  async invoke(call: Call, input: QueryShopInput): Promise<QueryAttempt<ShopResult>> {
    const taxNumber = input?.taxNumber;
    if (typeof taxNumber !== 'string' || ![15, 17, 18, 19, 20].includes(taxNumber.length) || !/^[0-9A-Z]+$/.test(taxNumber)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.readShop(call, taxNumber);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 1) return branch('providerRejected', 'PROVIDER_REJECTION');
    const content = wire.content;
    if (!isRecord(content) || !Number.isSafeInteger(content.shopId) || (content.shopId as number) <= 0 || typeof content.name !== 'string' || !content.name || content.name.length > 256 || ![-1, 0, 1].includes(content.apiState as number) || !Number.isInteger(content.apiState) || (content.endTime !== undefined && (typeof content.endTime !== 'string' || content.endTime.length > 32))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { shopId: content.shopId as number, name: content.name, apiState: content.apiState as number, endTime: (content.endTime as string | undefined) ?? '' }, {});
  }
}

function validCredential(value: string): boolean { return !!value && value.length <= 2048 && value.trim() === value; }
function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'easyapi', operation: 'queryShop', kind, message: 'EasyAPI company query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ShopResult> { return newQueryBranch(name, emptyResult, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ShopResult> { return newQueryRetry(failure(kind), delay); }
