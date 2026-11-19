// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryOrderStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, OrderStatusResult, QueryOrderStatusInput } from './generated/models.js';

const emptyOutput: OrderStatusResult = { deliveryId: 0, meituanOrderId: '', merchantOrderId: '', status: 0, operatedAt: 0 };
/** Optional transport and clock for deterministic delivery-query tests. */
export interface MeituanClientOptions { /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; /** Signature clock. */ now?: () => Date; }
/** Query Meituan Delivery order status with signed form requests. */
export class MeituanClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate immutable connection settings without provider I/O. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MeituanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Meituan configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://peisongopen.meituan.com';
    if (!/^https:\/\/[^/?#]+$/.test(this.baseUrl)) throw new TypeError('Meituan base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 15_000; this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Meituan request limits are invalid');
    this.send = options.send ?? sendProviderRequest; this.now = options.now ?? (() => new Date());
  }
  /** Bind the order-status Query. */ queryOrderStatus(): QueryOrderStatusOperation { return new QueryOrderStatusOperation(this); }
  /** Resolve credentials and dispatch one signed request. */
  async request(call: Call, input: QueryOrderStatusInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    if (!input || !Number.isSafeInteger(input.deliveryId) || input.deliveryId <= 0 || !validOrderID(input.meituanOrderId)) return { error: 'input', retryAfterMs: 0 };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appKey = credentials.appKey; const secret = credentials.secret instanceof SecretString ? credentials.secret.reveal() : '';
    if (typeof appKey !== 'string' || ![appKey, secret].every(value => !!value && value.length <= 2048 && value.trim() === value)) return { error: 'credential', retryAfterMs: 0 };
    const form = new URLSearchParams({ appkey: appKey, timestamp: Math.floor(this.now().getTime() / 1000).toString(), version: '1.0', delivery_id: String(input.deliveryId), mt_peisong_id: input.meituanOrderId });
    const signatureText = secret + [...form.entries()].sort(([left], [right]) => left.localeCompare(right)).filter(([, value]) => value).map(([key, value]) => key + value).join('');
    form.set('sign', createHash('sha1').update(signatureText).digest('hex'));
    const request: ProviderRequest = { url: this.baseUrl + '/api/order/status/query', method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify one Meituan order-status response. */
export class QueryOrderStatusOperation implements Query<QueryOrderStatusInput, OrderStatusResult> {
  /** Bind one validated client. */ constructor(private readonly client: MeituanClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return QueryOrderStatusDefinition; }
  /** Read and validate the selected delivery order. */
  async invoke(call: Call, input: QueryOrderStatusInput): Promise<QueryAttempt<OrderStatusResult>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 11) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = wire.data;
    if (!isRecord(data) || data.delivery_id !== input.deliveryId || data.mt_peisong_id !== input.meituanOrderId || !validOrderID(data.order_id) || !Number.isInteger(data.status) || ![0, 20, 30, 50, 99].includes(data.status as number) || !Number.isSafeInteger(data.operate_time) || (data.operate_time as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { deliveryId: data.delivery_id as number, meituanOrderId: data.mt_peisong_id as string, merchantOrderId: data.order_id as string, status: data.status as number, operatedAt: data.operate_time as number });
  }
}
function validOrderID(value: unknown): value is string { return typeof value === 'string' && /^[!-~]{1,32}$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'meituan', operation: 'queryOrderStatus', kind, message: 'Meituan provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<OrderStatusResult> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<OrderStatusResult> { return newQueryRetry(failure(kind), delay); }
