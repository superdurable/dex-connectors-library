// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryDeviceDetailDefinition } from './generated/definitions.js';
import type { Config, Credentials, DeviceDetail, QueryDeviceDetailInput } from './generated/models.js';

const emptyOutput: DeviceDetail = { productId: '', deviceName: '', status: 0, description: '', createTime: '', lastOnlineTime: '' };
/** Optional OneNET transport and signing clock for deterministic tests. */
export interface OnenetClientOptions { /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; /** Signature clock. */ now?: () => Date; }
/** Read OneNET device details with HMAC authorization. */
export class OnenetClient {
  private readonly baseUrl: string; private readonly timeoutMs: number; private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>; private readonly now: () => Date;
  /** Validate connection settings without contacting OneNET. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OnenetClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OneNET configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://iot-api.heclouds.com';
    if (!/^https:\/\/[^/?#]+$/.test(this.baseUrl)) throw new TypeError('OneNET base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 15_000; this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('OneNET request limits are invalid');
    this.send = options.send ?? sendProviderRequest; this.now = options.now ?? (() => new Date());
  }
  /** Bind the device-detail Query. */ queryDeviceDetail(): QueryDeviceDetailOperation { return new QueryDeviceDetailOperation(this); }
  /** Resolve credentials and dispatch one authorized request. */
  async request(call: Call, input: QueryDeviceDetailInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const userId = credentials.userId; const productId = credentials.productId;
    const accessKey = credentials.accessKey instanceof SecretString ? credentials.accessKey.reveal() : '';
    if (typeof userId !== 'string' || !/^\d{1,32}$/.test(userId) || typeof productId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(productId) || !input || typeof input.deviceName !== 'string' || !input.deviceName || [...input.deviceName].length > 256 || input.deviceName.trim() !== input.deviceName || /\p{Cc}/u.test(input.deviceName) || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(accessKey)) return { error: 'credential', retryAfterMs: 0 };
    const key = Buffer.from(accessKey, 'base64');
    if (!key.length) return { error: 'credential', retryAfterMs: 0 };
    const expiry = Math.floor(this.now().getTime() / 1000 + 3600).toString();
    const resource = 'userid/' + userId;
    const signature = createHmac('sha256', key).update(`${expiry}\nsha256\n${resource}\n2022-05-01`).digest('base64');
    const authorization = `version=2022-05-01&res=${encodeURIComponent(resource)}&et=${expiry}&method=sha256&sign=${encodeURIComponent(signature)}`;
    const query = new URLSearchParams({ product_id: productId, device_name: input.deviceName });
    const request: ProviderRequest = { url: `${this.baseUrl}/device/detail?${query.toString()}`, method: 'GET', headers: { Accept: 'application/json', Authorization: authorization }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify OneNET device responses without exposing credentials. */
export class QueryDeviceDetailOperation implements Query<QueryDeviceDetailInput, DeviceDetail> {
  /** Bind one validated client. */ constructor(private readonly client: OnenetClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryDeviceDetailDefinition; }
  /** Read one device and validate its metadata. */
  async invoke(call: Call, input: QueryDeviceDetailInput): Promise<QueryAttempt<DeviceDetail>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = wire.data;
    if (!isRecord(data) || typeof data.pid !== 'string' || !data.pid || typeof data.name !== 'string' || !data.name || !Number.isInteger(data.status) || (data.status as number) < 0 || (data.status as number) > 2) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { productId: data.pid, deviceName: data.name, status: data.status as number, description: typeof data.desc === 'string' ? data.desc : '', createTime: typeof data.create_time === 'string' ? data.create_time : '', lastOnlineTime: typeof data.last_time === 'string' ? data.last_time : '' });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'onenet', operation: 'queryDeviceDetail', kind, message: 'OneNET provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<DeviceDetail> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<DeviceDetail> { return newQueryRetry(failure(kind), delay); }
