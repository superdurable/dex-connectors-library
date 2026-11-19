// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetDailyPushReportDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetDailyPushReportInput, GetDailyPushReportOutput } from './generated/models.js';

/** Test seams for the token exchange and clock. */
export interface GetuiClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; }
const emptyReport: GetDailyPushReportOutput = { date: '', requestCount: 0, eligibleMessageCount: 0, sentCount: 0, receivedCount: 0, displayedCount: 0, clickedCount: 0 };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };

/** Cache short-lived Getui tokens in Worker memory, scoped to credentials. */
export class GetuiClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly tokens = new Map<string, { value: string; expiresAt: number }>();
  /** Validate local limits without contacting Getui. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GetuiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Getui configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Getui request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the historical daily report Query. */ getDailyPushReport(): GetDailyPushReportOperation { return new GetDailyPushReportOperation(this); }
  /** Return the current wall clock for report-date validation. */ currentTime(): Date { return this.now(); }
  /** Resolve current credentials for a single report attempt. */
  async resolveCredentials(call: Call): Promise<{ appId: string; appKey: string; masterSecret: string } | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId;
      const appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
      const masterSecret = credentials.masterSecret instanceof SecretString ? credentials.masterSecret.reveal() : '';
      if (!/^[A-Za-z0-9_-]{1,128}$/.test(appId) || !validOpaque(appKey) || !validOpaque(masterSecret)) return undefined;
      return { appId, appKey, masterSecret };
    } catch { return undefined; }
  }
  /** Exchange credentials for a token or reuse a valid in-memory token. */
  async tokenFor(call: Call, credentials: { appId: string; appKey: string; masterSecret: string }): Promise<{ token?: string; attempt?: QueryAttempt<GetDailyPushReportOutput> }> {
    const cacheKey = createHash('sha256').update(`${credentials.appId}\0${credentials.appKey}\0${credentials.masterSecret}`).digest('hex');
    const cached = this.tokens.get(cacheKey);
    if (cached && this.now().getTime() + 60_000 < cached.expiresAt) return { token: cached.value };
    const timestamp = String(this.now().getTime());
    const signature = createHash('sha256').update(credentials.appKey + timestamp + credentials.masterSecret).digest('hex');
    const request = this.request(call, `https://restapi.getui.com/v2/${credentials.appId}/auth`, 'POST', { Accept: 'application/json', 'Content-Type': 'application/json;charset=utf-8' }, JSON.stringify({ sign: signature, timestamp, appkey: credentials.appKey }));
    const exchange = await this.exchange(request);
    const classified = classifyExchange(exchange);
    if (classified) return { attempt: classified };
    const wire = decode(exchange.response?.body);
    if (!isObject(wire) || !Number.isInteger(wire.code)) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    if (wire.code !== 0) return { attempt: branch('providerRejected', 'AUTHENTICATION') };
    if (!isObject(wire.data) || !validOpaque(wire.data.token) || typeof wire.data.expire_time !== 'string' || !/^\d+$/.test(wire.data.expire_time)) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    const expiresAt = Number(wire.data.expire_time);
    if (!Number.isSafeInteger(expiresAt) || this.now().getTime() + 60_000 >= expiresAt) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    const token = wire.data.token as string;
    this.tokens.set(cacheKey, { value: token, expiresAt });
    return { token };
  }
  /** Forget a token after the provider reports expiration. */ forgetToken(credentials: { appId: string; appKey: string; masterSecret: string }): void {
    this.tokens.delete(createHash('sha256').update(`${credentials.appId}\0${credentials.appKey}\0${credentials.masterSecret}`).digest('hex'));
  }
  /** Request a daily report with an acquired token. */
  async report(call: Call, appId: string, date: string, token: string): Promise<Exchange> {
    return this.exchange(this.request(call, `https://restapi.getui.com/v2/${appId}/report/push/date/${date}`, 'GET', { Accept: 'application/json', token }));
  }
  private request(call: Call, url: string, method: string, headers: Record<string, string>, body?: string): ProviderRequest {
    return { url, method, headers, ...(body === undefined ? {} : { body: new TextEncoder().encode(body) }), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
  }
  private async exchange(request: ProviderRequest): Promise<Exchange> {
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify Getui auth and report responses with the Go Query contract. */
export class GetDailyPushReportOperation implements Query<GetDailyPushReportInput, GetDailyPushReportOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GetuiClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetDailyPushReportDefinition; }
  /** Read totals for one prior calendar day in China time. */
  async invoke(call: Call, input: GetDailyPushReportInput): Promise<QueryAttempt<GetDailyPushReportOutput>> {
    const date = input?.date;
    const today = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(this.client.currentTime());
    if (typeof date !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(date) || Number.isNaN(Date.parse(date)) || new Date(date).toISOString().slice(0, 10) !== date || date >= today) return branch('defect', 'VALIDATION');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const tokenResult = await this.client.tokenFor(call, credentials);
    if (tokenResult.attempt) return tokenResult.attempt;
    const exchange = await this.client.report(call, credentials.appId, date, tokenResult.token ?? '');
    const classified = classifyExchange(exchange);
    if (classified) return classified;
    const wire = decode(exchange.response?.body);
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 10001) { this.client.forgetToken(credentials); return retry('AUTHENTICATION'); }
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isObject(wire.data) || Object.keys(wire.data).length !== 1 || !isObject(wire.data[date]) || !isObject(wire.data[date].total)) return branch('invalidResponse', 'PROTOCOL');
    const total = wire.data[date].total;
    const fields = ['request_num', 'msg_num', 'target_num', 'receive_num', 'display_num', 'click_num'] as const;
    if (fields.some(field => !Number.isSafeInteger(total[field]) || (total[field] as number) < (field === 'display_num' ? -1 : 0))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { date, requestCount: total.request_num as number, eligibleMessageCount: total.msg_num as number, sentCount: total.target_num as number, receivedCount: total.receive_num as number, displayedCount: total.display_num as number, clickedCount: total.click_num as number });
  }
}
function classifyExchange(exchange: Exchange): QueryAttempt<GetDailyPushReportOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function validOpaque(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 1024 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function decode(body: Uint8Array | undefined): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'getui', operation: 'getDailyPushReport', kind, message: 'Getui daily report query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetDailyPushReportOutput> { return newQueryBranch(name, emptyReport, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetDailyPushReportOutput> { return newQueryRetry(failure(kind), delay); }
