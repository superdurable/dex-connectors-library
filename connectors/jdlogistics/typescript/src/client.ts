// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { QueryWaybillTraceDefinition } from './generated/definitions.js';
import type { Config, Credentials, QueryWaybillTraceInput, WaybillTrace } from './generated/models.js';

const queryPath = '/ecap/v1/orders/trace/query';
const emptyTrace = (): WaybillTrace => ({ waybillNo: '', events: [] });

/** Inject provider sender and signing clock for tests. */
export interface JDLogisticsClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing timestamp source. */ now?: () => Date;
}

/** Query privacy-minimized JD Logistics tracking scans. */
export class JDLogisticsClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the provider gateway settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JDLogisticsClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JD Logistics configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.jdl.com').replace(/\/$/, '');
    const url = new URL(this.baseURL);
    if (url.pathname !== '/' || url.search || url.hash) throw new TypeError('JD Logistics gateway must be an origin');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('JD Logistics request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the queryWaybillTrace Query. */ queryWaybillTrace(): QueryWaybillTraceOperation { return new QueryWaybillTraceOperation(this); }

  /** Sign and send one bounded waybill query. */
  async get(call: Call, input: QueryWaybillTraceInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appKey: string;
    let appSecret: string;
    let accessToken: string;
    let customerCode: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credentials.appKey;
      appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
      customerCode = credentials.customerCode;
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (![appKey, appSecret, accessToken, customerCode].every(validCredential) || customerCode.length > 32) return { error: 'credential', retryAfterMs: 0 };
    const body = JSON.stringify([{ waybillCode: input.waybillNo, orderOrigin: 1, customerCode }]);
    const timestamp = new Date(this.now().getTime() + 8 * 60 * 60 * 1000).toISOString().slice(0, 19).replace('T', ' ');
    const signed = `${appSecret}access_token${accessToken}app_key${appKey}method${queryPath}param_json${body}timestamp${timestamp}v2.0${appSecret}`;
    const sign = createHash('md5').update(signed).digest('hex').toUpperCase();
    const query = new URLSearchParams({ app_key: appKey, access_token: accessToken, timestamp, v: '2.0', 'LOP-DN': 'ECAP', sign });
    const request: ProviderRequest = { url: `${this.baseURL}${queryPath}?${query.toString()}`, method: 'POST', headers: { 'Content-Type': 'application/json;charset=UTF-8', Accept: 'application/json' }, body: Buffer.from(body), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Return one authorized waybill's tracking scans without personal data. */
export class QueryWaybillTraceOperation implements Query<QueryWaybillTraceInput, WaybillTrace> {
  /** Bind the client. */ constructor(private readonly client: JDLogisticsClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return QueryWaybillTraceDefinition; }
  /** Classify the provider result according to the Go connector. */
  async invoke(call: Call, input: QueryWaybillTraceInput): Promise<QueryAttempt<WaybillTrace>> {
    if (!input || !validWaybill(input.waybillNo)) return branch('defect', 'VALIDATION', 'JD Logistics waybill input is invalid');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'JD Logistics credentials are unavailable or invalid');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT', 'JD Logistics request failed');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', 'JD Logistics rate limited the query', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', 'JD Logistics is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'JD Logistics response exceeded the configured limit');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `JD Logistics rejected HTTP ${status}`);
    const text = new TextDecoder('utf-8', { fatal: true });
    let raw: unknown;
    try { raw = JSON.parse(text.decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL', 'JD Logistics returned invalid tracking JSON'); }
    if (!isObject(raw) || typeof raw.success !== 'boolean' || !Number.isInteger(raw.code)) return branch('invalidResponse', 'PROTOCOL', 'JD Logistics returned incomplete status');
    const code = raw.code as number;
    if (!raw.success || code !== 0) return code >= 4000 && code < 5000 ? retry('AVAILABILITY', 'JD Logistics reported a transient service error') : branch('providerRejected', 'PROVIDER_REJECTION', 'JD Logistics rejected the query');
    if (!isObject(raw.data) || !Array.isArray(raw.data.traceDetails) || raw.data.traceDetails.length > 1000) return branch('invalidResponse', 'PROTOCOL', 'JD Logistics returned incomplete or excessive scans');
    const events: WaybillTrace['events'] = [];
    for (const scan of raw.data.traceDetails) {
      if (!isObject(scan) || (scan.waybillCode && scan.waybillCode !== input.waybillNo) || typeof scan.state !== 'string' || !scan.state || typeof scan.operationTime !== 'string' || !scan.operationTime) return branch('invalidResponse', 'PROTOCOL', 'JD Logistics returned incomplete scan');
      events.push({ state: scan.state, categoryName: typeof scan.categoryName === 'string' ? scan.categoryName : '', operationTime: scan.operationTime });
    }
    return newQueryBranch('found', { waybillNo: input.waybillNo, events });
  }
}

function validCredential(value: string): boolean { return typeof value === 'string' && value.length > 0 && value.length <= 4096 && /^[\x21-\x7e]+$/.test(value); }
function validWaybill(value: string): boolean { return typeof value === 'string' && value.length > 0 && value.length <= 100 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'jdlogistics', operation: 'queryWaybillTrace', kind, message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<WaybillTrace> { return newQueryBranch(name, emptyTrace(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<WaybillTrace> { return newQueryRetry(failure(kind, message), delay); }
