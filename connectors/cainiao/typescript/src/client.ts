// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryWaybillTraceDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, QueryWaybillTraceInput, WaybillTrace, WaybillEvent } from './generated/models.js';

/** Dependencies for approved transports and deterministic signatures. */
export interface CainiaoClientOptions {
  /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; the system clock is used by default. */ now?: () => Date;
}

/** Alibaba TOP client for Cainiao electronic waybill events. */
export class CainiaoClient {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the TOP router URL and request limits without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: CainiaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Cainiao configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://eco.taobao.com/router/rest');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || !['/', '/router/rest'].includes(url.pathname))
      throw new TypeError('Cainiao TOP URL must be a router endpoint without query parameters');
    this.endpoint = url.toString().replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Cainiao request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the queryWaybillTrace Query. */ queryWaybillTrace(): QueryWaybillTraceOperation { return new QueryWaybillTraceOperation(this); }

  /** Resolve credentials and send one signed TOP request. */
  async requestTrace(call: Call, input: QueryWaybillTraceInput): Promise<{ response?: ProviderResponse; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean }> {
    let appKey: string;
    let appSecret: string;
    let sessionKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credentials.appKey;
      appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      sessionKey = credentials.sessionKey instanceof SecretString ? credentials.sessionKey.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (![appKey, appSecret, sessionKey].every(isOpaqueCredential)) return { credentialFailed: true };
    const zoned = new Date(this.now().getTime() + 8 * 3_600_000);
    const timestamp = zoned.toISOString().slice(0, 19).replace('T', ' ');
    const fields: Record<string, string> = {
      app_key: appKey, method: 'cainiao.waybill.logisticstrace.get', format: 'json', session: sessionKey,
      sign_method: 'hmac', timestamp, v: '2.0', cp_code: input.carrierCode, waybill_code: input.waybillCode,
    };
    const sorted = Object.keys(fields).sort();
    const canonical = sorted.map(key => key + fields[key]).join('');
    fields.sign = createHmac('md5', appSecret).update(canonical).digest('hex').toUpperCase();
    const body = new URLSearchParams(Object.keys(fields).sort().map(key => [key, fields[key] ?? ''])).toString();
    const request: ProviderRequest = {
      url: this.endpoint, method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded; charset=utf-8', Accept: 'application/json' },
      body: new TextEncoder().encode(body), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }

  /** Interpret Retry-After with the injected signing clock. */
  retryDelay(response: ProviderResponse): number {
    const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now()) || 0;
  }
}

/** Read one electronic waybill's logistics events. */
export class QueryWaybillTraceOperation implements Query<QueryWaybillTraceInput, WaybillTrace> {
  /** Bind the client. */ constructor(private readonly client: CainiaoClient) {}
  /** Return the generated Query definition. */ definition() { return QueryWaybillTraceDefinition; }
  /** Classify one TOP response without persisting credentials. */
  async invoke(call: Call, input: QueryWaybillTraceInput): Promise<QueryAttempt<WaybillTrace>> {
    if (!validWaybillField(input.carrierCode, 32) || !validWaybillField(input.waybillCode, 128))
      return terminal('defect', 'VALIDATION', 'Cainiao waybill input is invalid');
    const result = await this.client.requestTrace(call, input);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'Cainiao credentials are unavailable or invalid');
    if (result.transportFailed || !result.response) return newQueryRetry(failure('TRANSPORT', 'Cainiao request failed'));
    const response = result.response;
    const retryAfter = this.client.retryDelay(response);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Cainiao rate limited the query'), retryAfter);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Cainiao is temporarily unavailable'), retryAfter);
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Cainiao response exceeded the configured limit');
    if (response.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Cainiao rejected HTTP ${response.status}`);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned invalid trace JSON'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned invalid trace JSON');
    const fields = value as Record<string, unknown>;
    const hasError = fields.error_response != null;
    const hasSuccess = fields.cainiao_waybill_logisticstrace_get_response != null;
    if (hasError) {
      if (hasSuccess) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned conflicting outcomes');
      const error = fields.error_response;
      if (!error || typeof error !== 'object' || Array.isArray(error)) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete error');
      const code = (error as Record<string, unknown>).code;
      if (typeof code !== 'number' && typeof code !== 'string') return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete error');
      return terminal('providerRejected', 'PROVIDER_REJECTION', `Cainiao rejected code ${code}`);
    }
    const success = fields.cainiao_waybill_logisticstrace_get_response;
    if (!success || typeof success !== 'object' || Array.isArray(success)) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete trace');
    const list = (success as Record<string, unknown>).logistics_detail_dto_list;
    if (!list || typeof list !== 'object' || Array.isArray(list)) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete trace');
    const events = (list as Record<string, unknown>).logistics_detail_dto;
    if (!Array.isArray(events) || events.length > 1000) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete trace');
    const parsed: WaybillEvent[] = [];
    for (const event of events) {
      if (!event || typeof event !== 'object' || Array.isArray(event)) return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete event');
      const fields = event as Record<string, unknown>;
      if (typeof fields.action !== 'string' || !fields.action || typeof fields.description !== 'string' || !fields.description || typeof fields.time !== 'string' || !fields.time)
        return terminal('invalidResponse', 'PROTOCOL', 'Cainiao returned incomplete event');
      parsed.push({ action: fields.action, description: fields.description, time: fields.time });
    }
    return newQueryBranch('found', { carrierCode: input.carrierCode, waybillCode: input.waybillCode, events: parsed }, zeroReceipt());
  }
}

function isOpaqueCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 4096 && /^[!-~]+$/.test(value); }
function validWaybillField(value: unknown, maxBytes: number): value is string { return typeof value === 'string' && Buffer.byteLength(value) > 0 && Buffer.byteLength(value) <= maxBytes && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'cainiao', operation: 'queryWaybillTrace', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<WaybillTrace> { return newQueryBranch(branch, { carrierCode: '', waybillCode: '', events: [] }, zeroReceipt(), failure(kind, message)); }
