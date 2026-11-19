// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryWaybillTraceDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryWaybillTraceInput, WaybillTrace } from './generated/models.js';

const queryPath = '/ecap/v1/orders/trace/query';
type Exchange = { response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number };

/** Optional sender and clock for replaying signed requests. */
export interface JDLogisticsClientOptions {
  /** Sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses wall time. */ now?: () => Date;
}

/** Read bounded JD Logistics tracking scans without personal details. */
export class JDLogisticsClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate gateway and request limits before contacting JD Logistics. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JDLogisticsClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JD Logistics configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.jdl.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('JD Logistics request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the waybill Query. */ queryWaybillTrace(): QueryWaybillTraceOperation { return new QueryWaybillTraceOperation(this); }

  /** Sign and send one waybill request. */
  async query(call: Call, input: QueryWaybillTraceInput): Promise<Exchange> {
    let appKey = '', appSecret = '', accessToken = '', customerCode = '';
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credential.appKey;
      appSecret = credential.appSecret instanceof SecretString ? credential.appSecret.reveal() : '';
      accessToken = credential.accessToken instanceof SecretString ? credential.accessToken.reveal() : '';
      customerCode = credential.customerCode;
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (![appKey, appSecret, accessToken, customerCode].every(validCredential) || customerCode.length > 32) return { error: 'credential', retryAfterMs: 0 };
    const body = JSON.stringify([{ waybillCode: input.waybillNo, orderOrigin: 1, customerCode }]);
    const now = new Date(this.now().getTime() + 8 * 3_600_000);
    const timestamp = `${now.toISOString().slice(0, 10)} ${now.toISOString().slice(11, 19)}`;
    const signed = `${appSecret}access_token${accessToken}app_key${appKey}method${queryPath}param_json${body}timestamp${timestamp}v2.0${appSecret}`;
    const signature = createHash('md5').update(signed).digest('hex').toUpperCase();
    const parameters = new URLSearchParams({ app_key: appKey, access_token: accessToken, timestamp, v: '2.0', 'LOP-DN': 'ECAP', sign: signature });
    const request: ProviderRequest = { url: `${this.baseURL}${queryPath}?${parameters}`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json;charset=UTF-8' }, body: Buffer.from(body), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Query one merchant waybill's ordered tracking scans. */
export class QueryWaybillTraceOperation implements Query<QueryWaybillTraceInput, WaybillTrace> {
  /** Bind a validated gateway client. */ constructor(private readonly client: JDLogisticsClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return QueryWaybillTraceDefinition; }
  /** Classify provider responses and omit personal data. */
  async invoke(call: Call, input: QueryWaybillTraceInput): Promise<QueryAttempt<WaybillTrace>> {
    if (typeof input?.waybillNo !== 'string' || !input.waybillNo || input.waybillNo.length > 100 || !/^[\x21-\x7e]+$/.test(input.waybillNo)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.query(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || typeof wire.success !== 'boolean' || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (!wire.success || wire.code !== 0) return (wire.code as number) >= 4000 && (wire.code as number) < 5000 ? retry('AVAILABILITY') : branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isObject(wire.data) || !Array.isArray(wire.data.traceDetails) || wire.data.traceDetails.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const events: WaybillTrace['events'] = [];
    for (const event of wire.data.traceDetails) {
      if (!isObject(event) || event.waybillCode && event.waybillCode !== input.waybillNo || typeof event.state !== 'string' || !event.state || typeof event.operationTime !== 'string' || !event.operationTime) return branch('invalidResponse', 'PROTOCOL');
      events.push({ state: event.state, categoryName: typeof event.categoryName === 'string' ? event.categoryName : '', operationTime: event.operationTime });
    }
    return newQueryBranch('found', { waybillNo: input.waybillNo, events });
  }
}

function validCredential(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 4096 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'jdlogistics', operation: 'queryWaybillTrace', kind, message: 'JD Logistics waybill query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<WaybillTrace> { return newQueryBranch(name, { waybillNo: '', events: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<WaybillTrace> { return newQueryRetry(failure(kind), delay); }
