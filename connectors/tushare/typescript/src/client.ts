// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTradingDayDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTradingDayInput, GetTradingDayOutput } from './generated/models.js';

const fields = ['exchange', 'cal_date', 'is_open', 'pretrade_date'];
const emptyOutput: GetTradingDayOutput = { exchange: '', date: '', isOpen: false, previousTradingDate: '' };
/** Send one bounded provider request. */
export type TradingDayRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional caller-owned HTTP transport. */
export interface TushareClientOptions { send?: TradingDayRequestSender }
/** Resolve credentials for trading-calendar Queries. */
export class TushareClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: TradingDayRequestSender;
  /** Validate local settings without contacting Tushare. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TushareClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Tushare configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Tushare configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the trading-day Query. */ getTradingDay(): GetTradingDayOperation { return new GetTradingDayOperation(this); }
}
/** Read one securities exchange trading date. */
export class GetTradingDayOperation implements Query<GetTradingDayInput, GetTradingDayOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: TushareClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetTradingDayDefinition; }
  /** Send and classify one provider request. */
  async invoke(call: Call, input: GetTradingDayInput): Promise<QueryAttempt<GetTradingDayOutput>> {
    if (!input || !['SSE', 'SZSE', 'BSE'].includes(input.exchange) || !isValidDate(input.date)) return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const token = credentials.token instanceof SecretString ? credentials.token.reveal() : '';
    if (!token || token.length > 1024 || !/^[\x21-\x7e]+$/.test(token)) return branch('defect', 'AUTHENTICATION');
    const requestBody = JSON.stringify({ api_name: 'trade_cal', token, params: { exchange: input.exchange, start_date: input.date, end_date: input.date }, fields: fields.join(',') });
    let response: ProviderResponse;
    try { response = await this.client.send({ url: 'https://api.tushare.pro', method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode(requestBody), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) { if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE'); if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {}); return retry('AVAILABILITY'); }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (response.body.byteLength > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 2002) return branch('providerRejected', 'AUTHORIZATION');
    if (wire.code === 429) return retry('RATE_LIMIT');
    if (typeof wire.code === 'number' && wire.code >= 500 && wire.code <= 599) return retry('AVAILABILITY');
    if (wire.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = wire.data;
    if (!isRecord(data) || !Array.isArray(data.fields) || data.fields.length !== 4 || fields.some((field, index) => (data.fields as unknown[])[index] !== field) || !Array.isArray(data.items)) return branch('invalidResponse', 'PROTOCOL');
    if (data.items.length === 0) return branch('noData', 'NOT_FOUND');
    if (data.items.length !== 1 || !Array.isArray(data.items[0]) || data.items[0].length !== 4) return branch('invalidResponse', 'PROTOCOL');
    const [exchange, date, isOpen, previousTradingDate] = data.items[0];
    if (exchange !== input.exchange || date !== input.date || (isOpen !== 0 && isOpen !== 1) || typeof previousTradingDate !== 'string' || (previousTradingDate !== '' && previousTradingDate.length !== 8)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { exchange, date, isOpen: isOpen === 1, previousTradingDate });
  }
}
function isValidDate(value: unknown): value is string { if (typeof value !== 'string' || !/^\d{8}$/.test(value)) return false; const year = Number(value.slice(0, 4)); const month = Number(value.slice(4, 6)); const day = Number(value.slice(6, 8)); const date = new Date(Date.UTC(year, month - 1, day)); return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day; }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<GetTradingDayOutput> { const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); if (status === 429) return retry('RATE_LIMIT', delay); if (status === 408 || status >= 500) return retry('AVAILABILITY', delay); return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tushare', operation: 'getTradingDay', kind, message: 'Tushare trading-day query failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<GetTradingDayOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetTradingDayOutput> { return newQueryRetry(failure(kind), delayMs); }
