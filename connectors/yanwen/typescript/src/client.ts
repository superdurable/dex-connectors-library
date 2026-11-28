// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListWarehousesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListWarehousesInput, WarehouseList } from './generated/models.js';

const empty: WarehouseList = { warehouses: [] };
/** Optional provider transport and clock for deterministic verification. */
export interface YanwenClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for request signatures. */ now?: () => Date;
}
/** Read authorized Yanwen handover warehouses with the current API token. */
export class YanwenClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate endpoint and bounds without contacting Yanwen. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: YanwenClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Yanwen configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://open.yw56.com.cn').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/') throw new TypeError('Yanwen endpoint must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Yanwen request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the warehouse catalog Query. */ listWarehouses(): ListWarehousesOperation { return new ListWarehousesOperation(this); }
  /** Sign and read one bounded warehouse catalog. */
  async request(call: Call): Promise<QueryAttempt<WarehouseList>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const userID = credentials.userId;
    const token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    if (typeof userID !== 'string' || !userID || userID.length > 64 || userID.trim() !== userID || !/^[!-~]+$/.test(userID) || !token || Buffer.byteLength(token) > 2048 || /^\p{White_Space}|\p{White_Space}$/u.test(token)) return branch('defect', 'AUTHENTICATION');
    const timestamp = String(this.now().getTime());
    const sign = createHash('md5').update(`${token}${userID}{}jsoncommon.warehouse.getlist${timestamp}V1.0${token}`).digest('hex');
    const query = new URLSearchParams({ user_id: userID, format: 'json', method: 'common.warehouse.getlist', timestamp, version: 'V1.0', sign });
    const request: ProviderRequest = { url: `${this.baseUrl}/api/order?${query}`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json; charset=UTF-8' }, body: Buffer.from('{}'), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const delay = retryDelay(error.headers ?? {});
        if (error.status === 429) return retry('RATE_LIMIT', delay);
        if (error.status === 408 || error.status >= 500) return retry('AVAILABILITY', delay);
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      }
      return retry('AVAILABILITY');
    }
    const delay = retryDelay(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(response.status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder().decode(response.body), (_, value: unknown) =>
      isRecord(value) ? Object.fromEntries(Object.entries(value).map(([key, field]) => [key.toLowerCase(), field])) : value) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || (data.data != null && !Array.isArray(data.data))) return branch('invalidResponse', 'PROTOCOL');
    if (typeof data.success !== 'boolean' || typeof data.code !== 'string' || !data.code) return branch('invalidResponse', 'PROTOCOL');
    const warehouses: NonNullable<WarehouseList['warehouses']> = [];
    for (const item of (data.data ?? []) as unknown[]) {
      if (item !== null && !isRecord(item)) return branch('invalidResponse', 'PROTOCOL');
      const code = item?.code ?? '', name = item?.name ?? '', area = item?.area ?? '';
      if (typeof code !== 'string' || typeof name !== 'string' || typeof area !== 'string') return branch('invalidResponse', 'PROTOCOL');
      warehouses.push({ code, name, area });
    }
    if (!data.success || data.code !== '0') return data.code.startsWith('-') ? retry('AVAILABILITY', delay) : branch('providerRejected', 'PROVIDER_REJECTION');
    if (!data.data || warehouses.length > 500 || warehouses.some(row => !row.code || !row.name || !row.area)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { warehouses });
  }
}
/** Read handover warehouse codes, names, and regions. */
export class ListWarehousesOperation implements Query<ListWarehousesInput, WarehouseList> {
  /** Bind one validated client. */ constructor(private readonly client: YanwenClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListWarehousesDefinition; }
  /** Invoke one safe provider read. */ invoke(call: Call): Promise<QueryAttempt<WarehouseList>> { return this.client.request(call); }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function retryDelay(headers: ProviderResponse['headers']): number {
  const value = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
}
function failure(kind: Failure['kind']): Failure { return { provider: 'yanwen', operation: 'listWarehouses', kind, message: 'Yanwen warehouse request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<WarehouseList> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<WarehouseList> { return newQueryRetry(failure(kind), delay); }
