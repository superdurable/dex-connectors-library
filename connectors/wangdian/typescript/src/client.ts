// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListShopsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListShopsInput, ListShopsOutput, ShopSummary } from './generated/models.js';

const emptyOutput: ListShopsOutput = { shops: [], total: 0, nextPage: 0 };
/** Send a bounded Wangdian HTTP request. */
export type ShopRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned request transport and clock. */
export interface WangdianClientOptions { send?: ShopRequestSender; now?: () => Date }
/** Resolve Wangdian credentials for each shop Query. */
export class WangdianClient {
  readonly endpoint: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: ShopRequestSender;
  readonly now: () => Date;
  /** Validate the documented production origin and local settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WangdianClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Wangdian configuration and credentials are required');
    const baseUrl = config.baseUrl ?? 'https://openapi.huice.com';
    const url = new URL(baseUrl);
    if (url.protocol !== 'https:' || url.hostname !== 'openapi.huice.com' || url.port || url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw new TypeError('Wangdian requires its documented HTTPS production origin');
    this.endpoint = `${url.origin}/openapi/shop.php`;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Wangdian configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the shop-list Query. */ listShops(): ListShopsOperation { return new ListShopsOperation(this); }
}
/** Read one bounded page of ERP shops. */
export class ListShopsOperation implements Query<ListShopsInput, ListShopsOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: WangdianClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListShopsDefinition; }
  /** Sign and classify one shop-list request. */
  async invoke(call: Call, input: ListShopsInput): Promise<QueryAttempt<ListShopsOutput>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const sid = credentials.sid instanceof SecretString ? credentials.sid.reveal() : '';
    const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
    const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (![sid, appKey, appSecret].every(isValidCredential)) return branch('defect', 'AUTHENTICATION');
    const pageSize = input?.pageSize || 30;
    if (!input || !Number.isSafeInteger(input.page) || input.page < 0 || input.page > 1_000_000 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const params: Record<string, string> = { sid, appkey: appKey, timestamp: String(Math.floor(this.client.now().getTime() / 1000)), page_no: String(input.page), page_size: String(pageSize) };
    const signed = Object.keys(params).sort().map(key => `${String(Buffer.byteLength(key)).padStart(2, '0')}-${key}:${String(Buffer.byteLength(params[key] ?? '')).padStart(4, '0')}-${params[key]}`).join(';') + appSecret;
    const sign = createHash('md5').update(signed).digest('hex');
    const form = new URLSearchParams();
    for (const key of Object.keys(params).sort()) form.set(key, params[key] ?? '');
    form.set('sign', sign);
    let response: ProviderResponse;
    try { response = await this.client.send({ url: this.client.endpoint, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 || response.status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.code as number;
    if ([1012, 1013].includes(code)) return retry('RATE_LIMIT');
    if ([1020, 1030, 1040, 1060, 1200, 1510, 1520, 1530].includes(code)) return retry('AVAILABILITY');
    if (code !== 0) return branch('providerRejected', [1090, 1500, 1600, 1610, 2080, 2090, 2100].includes(code) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    if (!Array.isArray(wire.shoplist) || !Number.isSafeInteger(wire.total_count) || (wire.total_count as number) < 0 || wire.shoplist.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const shops: ShopSummary[] = [];
    for (const item of wire.shoplist) {
      if (!isRecord(item)) return branch('invalidResponse', 'PROTOCOL');
      const id = integerText(item.shop_id);
      const stateText = integerText(item.auth_state);
      const state = Number(stateText);
      if (!id || id.length > 30 || !stateText || !Number.isInteger(state) || state < 0 || state > 3 || typeof item.shop_no !== 'string' || !item.shop_no || item.shop_no.length > 50 || typeof item.shop_name !== 'string' || !item.shop_name || item.shop_name.length > 128 || !Number.isSafeInteger(item.platform_id) || (item.platform_id as number) < 0) return branch('invalidResponse', 'PROTOCOL');
      shops.push({ id, number: item.shop_no, name: item.shop_name, platformID: item.platform_id as number, authorizationState: state });
    }
    const total = wire.total_count as number;
    let nextPage = 0;
    if ((input.page + 1) * pageSize < total) { if (shops.length === 0 || input.page === 1_000_000) return branch('invalidResponse', 'PROTOCOL'); nextPage = input.page + 1; }
    return newQueryBranch('listed', { shops, total, nextPage });
  }
}
function integerText(value: unknown): string { if (typeof value === 'string' && /^\d+$/.test(value)) return value; if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return String(value); return ''; }
function isValidCredential(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[\x21-\x7e]+$/.test(value); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListShopsOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'wangdian', operation: 'listShops', kind, message: 'Wangdian shop query failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<ListShopsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListShopsOutput> { return newQueryRetry(failure(kind), delayMs); }
