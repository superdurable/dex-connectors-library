// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createPrivateKey, sign, type KeyObject } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryPaymentStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryPaymentStatusInput, PaymentStatusResult } from './generated/models.js';

/** Test seams for transport, clock, and conformance signature replay. */
export interface LianlianpayClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; sign?: (body: Uint8Array, key: KeyObject) => string; }
const emptyPayment: PaymentStatusResult = { merchantOrderId: '', status: '' };

/** Sign payment-status reads without persisting merchant credentials. */
export class LianlianpayClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly sign: (body: Uint8Array, key: KeyObject) => string;
  /** Validate local HTTPS endpoint and request limits. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: LianlianpayClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('LianLian Pay configuration and credentials are required');
    const url = new URL(config.baseUrl || 'https://openapi.lianlianpay.com');
    if (url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/' || url.search || url.hash) throw new TypeError('LianLian Pay HTTPS origin is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('LianLian Pay request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.sign = options.sign ?? signPaymentRequest;
  }
  /** Bind the read-only payment status Query. */ queryPaymentStatus(): QueryPaymentStatusOperation { return new QueryPaymentStatusOperation(this); }
  /** Send one signed merchant order query. */
  async fetchPayment(call: Call, merchantOrderId: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let merchantId = '', privateKeyPEM = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      merchantId = credentials.merchantId;
      privateKeyPEM = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!/^\d{16,18}$/.test(merchantId) || privateKeyPEM.length > 16_384) return { error: 'credential', retryAfterMs: 0 };
    let key: KeyObject;
    try { key = createPrivateKey(privateKeyPEM); if (key.asymmetricKeyType !== 'rsa') throw new TypeError('RSA required'); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const body = new TextEncoder().encode(JSON.stringify({ mch_id: merchantId, txn_seqno: merchantOrderId }));
    let signature: string;
    try { signature = this.sign(body, key); } catch { return { error: 'credential', retryAfterMs: 0 }; }
    const formatter = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });
    const pieces = Object.fromEntries(formatter.formatToParts(this.now()).filter(piece => piece.type !== 'literal').map(piece => [piece.type, piece.value]));
    const timestamp = `${pieces.year}${pieces.month}${pieces.day}${pieces.hour}${pieces.minute}${pieces.second}`;
    const request: ProviderRequest = { url: this.baseUrl + '/query/mch/v1/accp/txn/order-query', method: 'POST', headers: { 'Signature-Type': 'RSA', 'Signature-Data': signature, mch_id: merchantId, timestamp, 'Content-Type': 'application/json;charset=utf-8', Accept: 'application/json' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** MD5-of-lowercase-MD5 text signed with RSA PKCS#1 v1.5, as required by LianLian. */
export function signPaymentRequest(body: Uint8Array, key: KeyObject): string {
  const firstHex = createHash('md5').update(body).digest('hex');
  return sign('md5', Buffer.from(firstHex), key).toString('base64');
}

/** Classify LianLian Pay order responses with the Go Query contract. */
export class QueryPaymentStatusOperation implements Query<QueryPaymentStatusInput, PaymentStatusResult> {
  /** Bind the provider client. */ constructor(private readonly client: LianlianpayClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryPaymentStatusDefinition; }
  /** Invoke one payment-status read. */
  async invoke(call: Call, input: QueryPaymentStatusInput): Promise<QueryAttempt<PaymentStatusResult>> {
    const merchantOrderId = input?.merchantOrderId;
    if (typeof merchantOrderId !== 'string' || !/^[!-~]{1,32}$/.test(merchantOrderId)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchPayment(call, merchantOrderId);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || typeof wire.ret_code !== 'string' || !wire.ret_code) return branch('invalidResponse', 'PROTOCOL');
    if (wire.ret_code !== '0000') return branch('providerRejected', 'PROVIDER_REJECTION');
    if (wire.txn_seqno !== merchantOrderId || !['SUCCESS', 'FAILURE', 'PROCESSING', 'CLOSED', 'CANCEL'].includes(wire.txn_status as string)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { merchantOrderId, status: wire.txn_status as string });
  }
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'lianlianpay', operation: 'queryPaymentStatus', kind, message: 'LianLian Pay order query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<PaymentStatusResult> { return newQueryBranch(name, emptyPayment, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<PaymentStatusResult> { return newQueryRetry(failure(kind), delay); }
