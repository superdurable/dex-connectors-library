// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryOrderStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryOrderStatusInput, OrderStatus } from './generated/models.js';

const productionOrigin = 'https://newopen.imdada.cn';
const endpointPath = '/api/order/status/query';
const emptyStatus = (): OrderStatus => ({ orderID: '', statusCode: 0, statusMessage: '' });
/** Injectable provider HTTP sender. */
export type DadaRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Transport and clock dependencies. */
export interface DadaClientOptions {
  /** SDK sender when omitted. */ send?: DadaRequestSender;
  /** System clock when omitted. */ now?: () => Date;
}
/** Dada signed order status client. */
export class DadaClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: DadaRequestSender;
  private readonly now: () => Date;
  /** Validate the provider origin and bounded request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DadaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dada configuration and credentials are required');
    this.baseUrl = (config.baseUrl || productionOrigin).replace(/\/$/, '');
    const parsed = new URL(this.baseUrl);
    if ((parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(parsed.hostname))) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/') throw new TypeError('Dada API URL must be a trusted host root');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Dada request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the queryOrderStatus Query. */ queryOrderStatus(): QueryOrderStatusOperation { return new QueryOrderStatusOperation(this); }
  /** Sign and send one status request. */
  async query(call: Call, input: QueryOrderStatusInput): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    if (!validOrderID(input?.orderID)) return { branch: 'defect', failure: failure('VALIDATION', 'Dada order ID is invalid') };
    let appKey: string; let appSecret: string; let sourceID: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credentials.appKey; appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : ''; sourceID = credentials.sourceID;
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'Dada credentials are unavailable or invalid') }; }
    if (!validOpaqueCredential(appKey) || !validOpaqueCredential(appSecret) || !validSourceID(sourceID)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'Dada credentials are unavailable or invalid') };
    const body = JSON.stringify({ order_id: input.orderID });
    const timestamp = Math.floor(this.now().getTime() / 1000).toString();
    const signature = signRequest(appSecret, appKey, body, sourceID, timestamp);
    const envelope = { app_key: appKey, body, format: 'json', signature, source_id: sourceID, timestamp, v: '1.0' };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${endpointPath}`, method: 'POST',
      headers: { 'Content-Type': 'application/json; charset=utf-8', Accept: 'application/json' },
      body: new TextEncoder().encode(JSON.stringify(envelope)),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return statusRetry(error.status, error.headers ?? {}, this.now());
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Dada response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('TRANSPORT', 'Dada request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return statusRetry(response.status, response.headers, this.now());
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'Dada response exceeded the configured limit') };
    return { response };
  }
}
/** Read the current delivery status without rider personal data. */
export class QueryOrderStatusOperation implements Query<QueryOrderStatusInput, OrderStatus> {
  /** Bind the provider client. */ constructor(private readonly client: DadaClient) {}
  /** Generated Query contract. */ definition(): QueryDefinition { return QueryOrderStatusDefinition; }
  /** Classify the signed provider response. */
  async invoke(call: Call, input: QueryOrderStatusInput): Promise<QueryAttempt<OrderStatus>> {
    const exchange = await this.client.query(call, input);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyStatus(), {}, exchange.failure);
    const response = exchange.response;
    if (response.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Dada rejected HTTP ${response.status}`);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Dada returned invalid status JSON'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return terminal('invalidResponse', 'PROTOCOL', 'Dada returned invalid status JSON');
    const fields = wire as Record<string, unknown>;
    if (fields.status === 'fail' && typeof fields.code === 'number' && Number.isSafeInteger(fields.code)) return terminal('providerRejected', 'PROVIDER_REJECTION', `Dada rejected code ${fields.code}`);
    const result = fields.result;
    if (fields.status !== 'success' || fields.code !== 0 || !result || typeof result !== 'object' || Array.isArray(result)) return terminal('invalidResponse', 'PROTOCOL', 'Dada returned incomplete or inconsistent order status');
    const status = result as Record<string, unknown>;
    if (status.orderId !== input.orderID || typeof status.statusCode !== 'number' || !Number.isSafeInteger(status.statusCode) || status.statusCode === 0 || typeof status.statusMsg !== 'string' || !status.statusMsg) return terminal('invalidResponse', 'PROTOCOL', 'Dada returned incomplete or inconsistent order status');
    return newQueryBranch('found', { orderID: input.orderID, statusCode: status.statusCode, statusMessage: status.statusMsg });
  }
}
/** Sign a Dada API envelope using the official uppercase MD5 algorithm. */
export function signRequest(appSecret: string, appKey: string, body: string, sourceID: string, timestamp: string): string {
  const sortedFields = `app_key${appKey}body${body}formatjsonsource_id${sourceID}timestamp${timestamp}v1.0`;
  return createHash('md5').update(appSecret + sortedFields + appSecret).digest('hex').toUpperCase();
}
function validOpaqueCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 4096 && /^[!-~]+$/.test(value); }
function validSourceID(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 20 && /^[0-9]+$/.test(value); }
function validOrderID(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 128 && Buffer.from(value).toString('utf8') === value && value.trim() === value && ![...value].some(character => character.codePointAt(0)! < 0x20 || character.codePointAt(0)! === 0x7f); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'dada', operation: 'queryOrderStatus', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<OrderStatus> { return newQueryBranch(branch, emptyStatus(), {}, failure(kind, message)); }
function statusRetry(status: number, headers: ProviderResponse['headers'], now: Date): { retry: true; failure: Failure; retryAfterMs: number } {
  const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
  return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', now),
    failure: status === 429 ? failure('RATE_LIMIT', 'Dada rate limited the request') : failure('AVAILABILITY', 'Dada is temporarily unavailable') };
}
