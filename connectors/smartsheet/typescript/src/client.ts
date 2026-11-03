// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, newQueryBranch, newQueryRetry, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { Config, Credentials, GetSheetInput, ListSheetsInput, Sheet, SheetPage } from './generated/models.js';
import { GetSheetDefinition, ListSheetsDefinition } from './generated/definitions.js';

type Sender = (request: ProviderRequest) => Promise<ProviderResponse>;
type Exchange = { status: number; headers: ProviderResponse['headers']; body: Uint8Array; tooLarge: boolean };
const emptyPage: SheetPage = { sheets: [], pageNumber: 0, totalCount: 0 };
const emptySheet: Sheet = { id: 0, name: '', columns: [], rows: [] };

/** Dependencies for a controlled provider transport. */
export interface ClientOptions { /** Provider sender; defaults to the SDK transport. */ send?: Sender }

/** Smartsheet API client with call-time credential resolution. */
export class Client {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: Sender;

  /** Validate configuration without contacting Smartsheet. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ClientOptions = {}) {
    if (!credentials) throw new TypeError('Smartsheet credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.smartsheet.com/2.0');
    this.timeoutMs = config.timeoutMs || 30_000;
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Smartsheet timeout must be positive milliseconds');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Smartsheet maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listSheets Query. */ listSheets(): ListSheetsOperation { return new ListSheetsOperation(this); }
  /** Bind the getSheet Query. */ getSheet(): GetSheetOperation { return new GetSheetOperation(this); }

  /** Send one read and retain the status even when the body exceeds the limit. */
  async exchange(call: Call, path: string): Promise<Exchange | { failure: Failure; branch: string; retryAfterMs: number }> {
    let key: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiKey instanceof SecretString) || !isHeaderSafeCredential(resolved.apiKey.reveal())) throw new TypeError('invalid credential');
      key = resolved.apiKey.reveal();
    } catch {
      return { failure: failure(call, 'AUTHENTICATION', 'connection credentials are invalid'), branch: 'defect', retryAfterMs: 0 };
    }
    try {
      const response = await this.send({ url: `${this.baseUrl}${path}`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${key}` }, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
      return { status: response.status, headers: response.headers, body: response.body, tooLarge: false };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(), tooLarge: true };
      return { failure: failure(call, 'AVAILABILITY', 'provider request failed'), branch: 'retry', retryAfterMs: 0 };
    }
  }
}

/** List one page of accessible Smartsheet sheets. */
export class ListSheetsOperation {
  /** Bind a client. */ constructor(private readonly client: Client) {}
  /** Return the manifest definition. */ definition() { return ListSheetsDefinition; }
  /** Validate and classify a list request. */
  async invoke(call: Call, input: ListSheetsInput): Promise<QueryAttempt<SheetPage>> {
    if (!Number.isInteger(input.page ?? 0) || !Number.isInteger(input.pageSize ?? 0) || (input.page ?? 0) < 0 || (input.pageSize ?? 0) < 0 || (input.pageSize ?? 0) > 1000) return branch(call, 'defect', emptyPage, 'VALIDATION', 'page or pageSize is invalid');
    const page = Math.max(input.page ?? 0, 1), size = input.pageSize || 100;
    const exchange = await this.client.exchange(call, `/sheets?page=${page}&pageSize=${size}`);
    if ('failure' in exchange) return fromFailure(exchange, emptyPage);
    if (exchange.status !== 200) return classifyStatus(call, exchange, emptyPage, 'providerRejected');
    if (exchange.tooLarge) return branch(call, 'invalidResponse', emptyPage, 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit');
    try { return newQueryBranch('listed', decodePage(exchange.body), {}); }
    catch { return branch(call, 'invalidResponse', emptyPage, 'PROTOCOL', 'provider returned unusable JSON'); }
  }
}

/** Read one Smartsheet sheet by numeric ID. */
export class GetSheetOperation {
  /** Bind a client. */ constructor(private readonly client: Client) {}
  /** Return the manifest definition. */ definition() { return GetSheetDefinition; }
  /** Validate and classify one sheet request. */
  async invoke(call: Call, input: GetSheetInput): Promise<QueryAttempt<Sheet>> {
    if (!Number.isSafeInteger(input.sheetId) || input.sheetId <= 0) return branch(call, 'defect', emptySheet, 'VALIDATION', 'sheetId must be positive');
    const exchange = await this.client.exchange(call, `/sheets/${input.sheetId}`);
    if ('failure' in exchange) return fromFailure(exchange, emptySheet);
    if (exchange.status !== 200) return classifyStatus(call, exchange, emptySheet, 'notFound');
    if (exchange.tooLarge) return branch(call, 'invalidResponse', emptySheet, 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit');
    try { return newQueryBranch('found', decodeSheet(exchange.body), {}); }
    catch { return branch(call, 'invalidResponse', emptySheet, 'PROTOCOL', 'provider returned unusable JSON'); }
  }
}

function failure(call: Call, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'smartsheet', operation: call.operation.operationId, message }; }
function branch<T>(call: Call, branchId: string, value: T, kind: Failure['kind'], message: string): QueryAttempt<T> { return newQueryBranch(branchId, value, {}, failure(call, kind, message)); }
function fromFailure<T>(result: { failure: Failure; branch: string; retryAfterMs: number }, zero: T): QueryAttempt<T> { return result.branch === 'retry' ? newQueryRetry(result.failure, result.retryAfterMs) : newQueryBranch(result.branch, zero, {}, result.failure); }
function classifyStatus<T>(call: Call, result: Exchange, zero: T, notFoundBranch: string): QueryAttempt<T> {
  const status = result.status;
  const retryHeader = result.headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '');
  if (status === 429) return newQueryRetry(failure(call, 'RATE_LIMIT', 'provider rate limit'), retryAfterMs);
  if (status === 408 || status === 409 || status >= 500) return newQueryRetry(failure(call, 'AVAILABILITY', 'provider temporarily unavailable'), retryAfterMs);
  if (status === 404 && notFoundBranch !== 'providerRejected') return branch(call, notFoundBranch, zero, 'NOT_FOUND', 'provider resource was not found');
  if (status === 401) return branch(call, 'providerRejected', zero, 'AUTHENTICATION', 'provider rejected the API key');
  if (status === 403) return branch(call, 'providerRejected', zero, 'AUTHORIZATION', 'provider denied the request');
  return branch(call, 'providerRejected', zero, 'PROVIDER_REJECTION', `provider rejected HTTP ${status}`);
}
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('object expected'); return value as Record<string, unknown>; }
function integer(value: unknown): number { if (value === undefined) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('integer expected'); return value; }
function string(value: unknown): string { if (value === undefined) return ''; if (typeof value !== 'string') throw new TypeError('string expected'); return value; }
function array(value: unknown): unknown[] | null { if (value === undefined || value === null) return null; if (!Array.isArray(value)) throw new TypeError('array expected'); return value; }
function decodePage(body: Uint8Array): SheetPage {
  const wire = object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  const entries = array(wire.data);
  const pageNumber = integer(wire.pageNumber), totalCount = integer(wire.totalCount);
  if (!entries || pageNumber < 1 || totalCount < 0) throw new TypeError('incomplete sheet page');
  return { sheets: entries.map(entry => { const item = object(entry); return { id: integer(item.id), name: string(item.name) }; }), pageNumber, totalCount };
}
function decodeSheet(body: Uint8Array): Sheet {
  const wire = object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
  const id = integer(wire.id), name = string(wire.name), columns = array(wire.columns), rows = array(wire.rows);
  if (id <= 0 || !name || !columns || !rows) throw new TypeError('incomplete sheet');
  return { id, name, columns: columns.map(column => { const item = object(column); return { id: integer(item.id), title: string(item.title) }; }), rows: rows.map(row => { const item = object(row); return { id: integer(item.id), cells: array(item.cells) ?? [] }; }) };
}
