// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListWarehousesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListWarehousesInput, WarehousePage } from './generated/models.js';

const empty: WarehousePage = { warehouses: [] };
/** Optional provider transport and clock for deterministic verification. */
export interface WinitClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for the Winit UTC+8 signature. */ now?: () => Date;
}
/** List authorized overseas warehouses with current seller and application credentials. */
export class WinitClient {
  private readonly baseUrl: string;
  private readonly platform: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate settings without contacting Winit. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WinitClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Winit configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://openapi.winit.com.cn').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/') throw new TypeError('Winit endpoint must be an HTTPS origin');
    this.platform = config.platform;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(this.platform) || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Winit platform or request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the warehouse catalog Query. */ listWarehouses(): ListWarehousesOperation { return new ListWarehousesOperation(this); }
  /** Sign and read one bounded warehouse catalog. */
  async request(call: Call): Promise<QueryAttempt<WarehousePage>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    const clientId = credentials.clientId instanceof SecretString ? credentials.clientId.reveal() : '';
    const clientSecret = credentials.clientSecret instanceof SecretString ? credentials.clientSecret.reveal() : '';
    if (![appKey, token, clientId, clientSecret].every(validSecret)) return branch('defect', 'AUTHENTICATION');
    const timestamp = new Date(this.now().getTime() + 8 * 3600_000).toISOString().slice(0, 19).replace('T', ' ');
    const parameters = { action: 'queryWarehouse', app_key: appKey, format: 'json', platform: this.platform, sign_method: 'md5', timestamp, version: '1.0' };
    const body = Buffer.from(JSON.stringify({ action: parameters.action, app_key: parameters.app_key, client_id: clientId, client_sign: sign(parameters, clientSecret), format: parameters.format, platform: parameters.platform, sign: sign(parameters, token), sign_method: parameters.sign_method, timestamp: parameters.timestamp, version: parameters.version }));
    const request: ProviderRequest = { url: `${this.baseUrl}/openapi/service`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 400 || response.status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || typeof data.code !== 'number' || !Number.isInteger(data.code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(data.data) || data.data.length > 200) return branch('invalidResponse', 'PROTOCOL');
    const warehouses: NonNullable<WarehousePage['warehouses']> = [];
    for (const item of data.data) {
      if (!isRecord(item) || typeof item.warehouseID !== 'number' || !Number.isSafeInteger(item.warehouseID) || item.warehouseID <= 0 || typeof item.warehouseCode !== 'string' || !item.warehouseCode || typeof item.warehouseName !== 'string' || !item.warehouseName) return branch('invalidResponse', 'PROTOCOL');
      warehouses.push({ id: item.warehouseID, code: item.warehouseCode, name: item.warehouseName });
    }
    return newQueryBranch('listed', { warehouses });
  }
}
/** Read warehouse IDs and codes without order or customer data. */
export class ListWarehousesOperation implements Query<ListWarehousesInput, WarehousePage> {
  /** Bind one validated client. */ constructor(private readonly client: WinitClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListWarehousesDefinition; }
  /** Invoke one safe provider read. */ invoke(call: Call): Promise<QueryAttempt<WarehousePage>> { return this.client.request(call); }
}
function sign(parameters: Record<string, string>, secret: string): string { const content = Object.entries(parameters).filter(([,value]) => value).sort(([left],[right]) => left.localeCompare(right, 'en')).map(([key,value]) => `${key}${value}`).join(''); return createHash('md5').update(`${secret}${content}${secret}`).digest('hex').toUpperCase(); }
function validSecret(value: string): boolean { return Boolean(value) && value.length <= 2048 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'winit', operation: 'listWarehouses', kind, message: 'Winit warehouse request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<WarehousePage> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<WarehousePage> { return newQueryRetry(failure(kind), delay); }
