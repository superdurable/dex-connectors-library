// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type MutationAttempt, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError, DispatchNotAllowedError, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import type { GetValuesInput, GetValuesOutput, FindRowInput, FindRowOutput, UpsertRowInput, UpsertRowOutput, Credentials } from './generated/models.js';
import { GetValuesDefinition, FindRowDefinition, UpsertRowDefinition } from './generated/definitions.js';
import { goPathEscape, type SpreadsheetClient, type SpreadsheetExchange } from './client.js';

const emptyValues = (): GetValuesOutput => ({ range: '', majorDimension: '', values: [] });
const emptyRow = (): FindRowOutput => ({});
const emptyUpsert = (): UpsertRowOutput => ({ action: '', rowNumber: 0, updatedRange: '' });
const quoteSheet = (sheetName: string): string => `'${sheetName.replaceAll("'", "''")}'`;
const isBlank = (value: string): boolean => !value.trim();

/** Read a bounded A1 range from Google Sheets. */
export class GetValuesOperation {
  /** Bind a validated client. */ constructor(private readonly client: SpreadsheetClient) {}
  /** Return the manifest-owned query definition. */ definition() { return GetValuesDefinition; }
  /** Invoke one read and classify its provider outcome. */
  async invoke(call: Call, input: GetValuesInput): Promise<QueryAttempt<GetValuesOutput>> {
    const operation = 'getValues';
    if (isBlank(input.spreadsheetId) || isBlank(input.range)) return newQueryBranch('defect', emptyValues(), {}, this.client.failure(operation, 'VALIDATION', 'spreadsheet ID and range are required'));
    const selected = await this.client.resolve(call);
    if (!selected.credential) return newQueryBranch('defect', emptyValues(), {}, selected.failure);
    const { exchange } = await this.client.request(call, selected.credential, 'GET', input.spreadsheetId, `/values/${goPathEscape(input.range)}`, { majorDimension: 'ROWS', valueRenderOption: 'FORMATTED_VALUE' });
    if (exchange.isTooLarge) return newQueryBranch('invalidResponse', emptyValues(), this.client.receipt(call, exchange), this.client.failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (exchange.error) return newQueryRetry(this.client.failure(operation, 'AVAILABILITY', 'provider is unavailable'));
    const receipt = this.client.receipt(call, exchange);
    if (exchange.status === 404) return newQueryBranch('notFound', emptyValues(), receipt, this.client.failure(operation, 'NOT_FOUND', 'spreadsheet or range was not found'));
    const retry = retryStatus(exchange);
    if (retry.kind === 'invalid') return newQueryRetry(this.client.failure(operation, 'PROTOCOL', 'provider returned an invalid Retry-After header'));
    if (retry.kind === 'retry') return newQueryRetry(this.client.failure(operation, statusKind(exchange.status), 'provider temporarily rejected the query'), retry.delay);
    if (exchange.status < 200 || exchange.status >= 300) return newQueryBranch('providerRejected', emptyValues(), receipt, this.client.failure(operation, statusKind(exchange.status), 'provider rejected the query'));
    const decoded = this.client.decodeValues(exchange, operation);
    if (!decoded.value) return newQueryBranch('invalidResponse', emptyValues(), receipt, decoded.failure);
    return newQueryBranch('read', decoded.value, receipt);
  }
}

/** Find a unique row under a named header. */
export class FindRowOperation {
  /** Bind a validated client. */ constructor(private readonly client: SpreadsheetClient) {}
  /** Return the manifest-owned query definition. */ definition() { return FindRowDefinition; }
  /** Invoke one lookup and classify its provider outcome. */
  async invoke(call: Call, input: FindRowInput): Promise<QueryAttempt<FindRowOutput>> {
    const operation = 'findRow';
    if ([input.spreadsheetId, input.sheetName, input.keyColumn, input.keyValue].some(isBlank)) return newQueryBranch('defect', emptyRow(), {}, this.client.failure(operation, 'VALIDATION', 'spreadsheet ID, sheet name, key column, and key value are required'));
    const selected = await this.client.resolve(call);
    if (!selected.credential) return newQueryBranch('defect', emptyRow(), {}, selected.failure);
    const { exchange } = await this.client.request(call, selected.credential, 'GET', input.spreadsheetId, `/values/${goPathEscape(quoteSheet(input.sheetName))}`, { majorDimension: 'ROWS', valueRenderOption: 'FORMATTED_VALUE' });
    if (exchange.isTooLarge) return newQueryBranch('invalidResponse', emptyRow(), this.client.receipt(call, exchange), this.client.failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (exchange.error) return newQueryRetry(this.client.failure(operation, 'AVAILABILITY', 'provider is unavailable'));
    const receipt = this.client.receipt(call, exchange);
    if (exchange.status === 404) return newQueryBranch('notFound', emptyRow(), receipt, this.client.failure(operation, 'NOT_FOUND', 'spreadsheet or sheet was not found'));
    const retry = retryStatus(exchange);
    if (retry.kind === 'invalid') return newQueryRetry(this.client.failure(operation, 'PROTOCOL', 'provider returned an invalid Retry-After header'));
    if (retry.kind === 'retry') return newQueryRetry(this.client.failure(operation, statusKind(exchange.status), 'provider temporarily rejected the query'), retry.delay);
    if (exchange.status < 200 || exchange.status >= 300) return newQueryBranch('providerRejected', emptyRow(), receipt, this.client.failure(operation, statusKind(exchange.status), 'provider rejected the query'));
    const decoded = this.client.decodeValues(exchange, operation);
    if (!decoded.value) return newQueryBranch('invalidResponse', emptyRow(), receipt, decoded.failure);
    const found = findRow(decoded.value.values, input.keyColumn, input.keyValue, operation, this.client);
    return newQueryBranch(found.branch, found.value, receipt, found.failure);
  }
}

/** Upsert a stable keyed row with a fenced append. */
export class UpsertRowOperation {
  /** Bind a validated client. */ constructor(private readonly client: SpreadsheetClient) {}
  /** Return the manifest-owned mutation definition. */ definition() { return UpsertRowDefinition; }
  /** The Dex Call ID correlates safe retries and receipts. */ idempotencyKey(callId: string): string { return callId; }
  /** Read the key and replace or append one row. */
  async invoke(call: Call, input: UpsertRowInput): Promise<MutationAttempt<UpsertRowOutput>> {
    const operation = 'upsertRow';
    if ([input.spreadsheetId, input.sheetName, input.keyColumn, input.keyValue].some(isBlank) || !input.values || Object.keys(input.values).length === 0) {
      const message = [input.spreadsheetId, input.sheetName, input.keyColumn, input.keyValue].some(isBlank) ? 'spreadsheet ID, sheet name, key column, and key value are required' : 'values are required';
      return newMutationBranch('defect', emptyUpsert(), {}, this.client.failure(operation, 'VALIDATION', message));
    }
    let fence: Fence;
    try { fence = newFence(call, 30_000); }
    catch { return newMutationBranch('defect', emptyUpsert(), {}, this.client.failure(operation, 'LOCAL_DEFECT', 'single-dispatch fence could not be constructed')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return this.uncertain(call, undefined, 'prior dispatch checkpoint cannot be trusted');
    const selected = await this.client.resolve(call);
    if (!selected.credential) {
      if (selected.reauthorization) {
        if (decision.kind === 'sendAllowed') {
          try { await fence.recordNotSent(); } catch { return newMutationRetry(this.client.failure(operation, 'AVAILABILITY', 'credential checkpoint failed')); }
          return newMutationBranch('defect', emptyUpsert(), {}, selected.failure);
        }
        return newMutationUncertain(emptyUpsert(), this.client.failure(operation, 'AUTHENTICATION', 'prior append cannot be checked; reconnect the connection'), this.client.receipt(call, undefined, decision.marker?.providerId));
      }
      return this.retryBeforeWrite(fence, decision.kind, selected.failure!);
    }
    const credential = selected.credential;
    const lookup = await this.client.request(call, credential, 'GET', input.spreadsheetId, `/values/${goPathEscape(quoteSheet(input.sheetName))}`, { majorDimension: 'ROWS', valueRenderOption: 'FORMATTED_VALUE' });
    const exchange = lookup.exchange;
    if (exchange.isTooLarge) {
      if (decision.kind === 'readBackRequired') return this.uncertain(call, exchange, 'read-back response is oversized');
      return newMutationBranch('invalidResponse', emptyUpsert(), this.client.receipt(call, exchange), this.client.failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    }
    if (exchange.error) return this.retryBeforeWrite(fence, decision.kind, this.client.failure(operation, 'AVAILABILITY', 'sheet lookup is unavailable'));
    const retry = retryStatus(exchange);
    if (retry.kind === 'invalid') return this.retryBeforeWrite(fence, decision.kind, this.client.failure(operation, 'PROTOCOL', 'provider returned an invalid Retry-After header'));
    if (retry.kind === 'retry') return this.retryBeforeWrite(fence, decision.kind, this.client.failure(operation, statusKind(exchange.status), 'sheet lookup is temporarily unavailable'), retry.delay);
    if (exchange.status < 200 || exchange.status >= 300) {
      if (decision.kind === 'readBackRequired') return this.uncertain(call, exchange, 'read-back was rejected');
      return newMutationBranch('providerRejected', emptyUpsert(), this.client.receipt(call, exchange), this.client.failure(operation, statusKind(exchange.status), 'provider rejected the pre-write query'));
    }
    const decoded = this.client.decodeValues(exchange, operation);
    if (!decoded.value) {
      if (decision.kind === 'readBackRequired') return this.uncertain(call, exchange, 'read-back response is invalid');
      return newMutationBranch('invalidResponse', emptyUpsert(), this.client.receipt(call, exchange), decoded.failure);
    }
    const rows = decoded.value.values;
    const found = findRow(rows, input.keyColumn, input.keyValue, operation, this.client);
    if (found.branch === 'conflict') return newMutationBranch('conflict', emptyUpsert(), this.client.receipt(call, exchange), found.failure);
    if (found.branch === 'defect') {
      if (decision.kind === 'readBackRequired' && decision.marker?.state === 'claimed') return this.uncertain(call, exchange, 'prior append cannot be checked against the row key');
      return newMutationBranch('defect', emptyUpsert(), this.client.receipt(call, exchange), found.failure);
    }
    const built = buildRow(rows, input, this.client);
    if (!built.row) {
      if (decision.kind === 'readBackRequired' && decision.marker?.state === 'claimed') return this.uncertain(call, exchange, 'prior append cannot be checked against the row schema');
      return newMutationBranch('defect', emptyUpsert(), this.client.receipt(call, exchange), built.failure);
    }
    const row = built.row;
    if (found.branch === 'found' && rowMatches(found.value, input)) {
      const action = decision.marker?.state === 'claimed' || decision.marker?.state === 'canResend' ? 'inserted' : 'updated';
      return newMutationBranch('upserted', upsertOutput(action, found.value.rowNumber!, input.sheetName, row.length), this.client.receipt(call, exchange));
    }
    if (decision.kind === 'readBackRequired' && (decision.marker?.state === 'claimed' || found.branch === 'notFound')) return this.uncertain(call, exchange, 'prior append cannot be confirmed by the row key');
    if (found.branch === 'found') return this.replace(call, lookup.credential, input, row, found.value.rowNumber!, fence, decision.kind);
    return this.append(call, lookup.credential, input, row, fence);
  }

  private async replace(call: Call, credential: Credentials, input: UpsertRowInput, row: string[], rowNumber: number, fence: Fence, decision: string): Promise<MutationAttempt<UpsertRowOutput>> {
    const targetRange = `${quoteSheet(input.sheetName)}!A${rowNumber}`;
    const payload = { range: targetRange, majorDimension: 'ROWS', values: [row] };
    let response = await this.client.request(call, credential, 'PUT', input.spreadsheetId, `/values/${goPathEscape(targetRange)}`, { valueInputOption: 'RAW' }, payload, true);
    let needsReauthorization = true;
    if (response.exchange.status === 401) {
      const refreshed = await this.client.refreshAfterRejection(call, credential);
      if (refreshed.credential) response = await this.client.request(call, refreshed.credential, 'PUT', input.spreadsheetId, `/values/${goPathEscape(targetRange)}`, { valueInputOption: 'RAW' }, payload, true);
      else needsReauthorization = refreshed.reauthorization;
    }
    const exchange = response.exchange;
    if (exchange.status === 0) return this.retryBeforeWrite(fence, decision, this.client.failure('upsertRow', 'TRANSPORT', 'row replacement response is unavailable'));
    if ([409, 429].includes(exchange.status) || exchange.status >= 500) {
      const retry = retryStatus(exchange, true);
      if (retry.kind === 'invalid') return this.retryBeforeWrite(fence, decision, this.client.failure('upsertRow', 'PROTOCOL', 'provider returned an invalid Retry-After header'));
      return this.retryBeforeWrite(fence, decision, this.client.failure('upsertRow', statusKind(exchange.status), 'row replacement is temporarily unavailable'), retry.delay);
    }
    const receipt = this.client.receipt(call, exchange, `${input.spreadsheetId}#${input.sheetName}!${rowNumber}`);
    if (exchange.status === 401) return newMutationBranch('providerRejected', emptyUpsert(), receipt, this.client.failure('upsertRow', 'AUTHENTICATION', rejectedMessage(needsReauthorization)));
    if (exchange.status < 200 || exchange.status >= 300) return newMutationBranch('providerRejected', emptyUpsert(), receipt, this.client.failure('upsertRow', statusKind(exchange.status), 'provider rejected the row replacement'));
    let updatedRange = upsertOutput('updated', rowNumber, input.sheetName, row.length).updatedRange;
    if (!exchange.error) { try { const response = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as { updatedRange?: unknown }; if (typeof response.updatedRange === 'string' && response.updatedRange) updatedRange = response.updatedRange; } catch { /* Accepted absolute replacement remains successful. */ } }
    return newMutationBranch('upserted', { action: 'updated', rowNumber, updatedRange }, receipt);
  }

  private async append(call: Call, credential: Credentials, input: UpsertRowInput, row: string[], fence: Fence): Promise<MutationAttempt<UpsertRowOutput>> {
    const targetRange = quoteSheet(input.sheetName);
    let response: { exchange: SpreadsheetExchange; credential: Credentials };
    try { response = await fence.dispatch(signal => this.client.request(call, credential, 'POST', input.spreadsheetId, `/values/${goPathEscape(targetRange)}:append`, { insertDataOption: 'INSERT_ROWS', valueInputOption: 'RAW' }, { range: targetRange, majorDimension: 'ROWS', values: [row] }, true, signal)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(this.client.failure('upsertRow', 'AVAILABILITY', 'append was not sent before the attempt deadline'));
      if (error instanceof DispatchNotAllowedError) return this.uncertain(call, undefined, 'single-dispatch permission is unavailable');
      return this.uncertain(call, undefined, 'append cannot be confirmed by the row key');
    }
    const exchange = response.exchange;
    if (exchange.status === 401) {
      const refreshed = await this.client.refreshAfterRejection(call, credential);
      if (refreshed.credential) return this.retryAfterRejectedAppend(fence, exchange);
      return newMutationBranch('providerRejected', emptyUpsert(), this.client.receipt(call, exchange), this.client.failure('upsertRow', 'AUTHENTICATION', rejectedMessage(refreshed.reauthorization)));
    }
    if ([409, 429, 503].includes(exchange.status)) return this.retryAfterRejectedAppend(fence, exchange);
    if (exchange.status === 0) return exchange.isProvablyNotSent ? this.retryAfterRejectedAppend(fence, exchange) : this.reconcile(call, credential, input, row);
    if (exchange.status >= 500) return this.reconcile(call, credential, input, row);
    if (exchange.status < 200 || exchange.status >= 300) return newMutationBranch('providerRejected', emptyUpsert(), this.client.receipt(call, exchange), this.client.failure('upsertRow', statusKind(exchange.status), 'provider rejected the append'));
    if (!exchange.error) {
      try {
        const payload = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as { updates?: { updatedRange?: unknown } };
        const updatedRange = payload.updates?.updatedRange;
        const rowNumber = typeof updatedRange === 'string' ? firstRowNumber(updatedRange) : 0;
        if (rowNumber && typeof updatedRange === 'string') {
          try { await fence.recordProviderId(updatedRange); } catch { /* The accepted response proves success. */ }
          return newMutationBranch('upserted', { action: 'inserted', rowNumber, updatedRange }, this.client.receipt(call, exchange, updatedRange));
        }
      } catch { /* Read-back confirms a successful append without a usable response. */ }
    }
    return this.reconcile(call, credential, input, row);
  }

  private async reconcile(call: Call, credential: Credentials, input: UpsertRowInput, row: string[]): Promise<MutationAttempt<UpsertRowOutput>> {
    const { exchange } = await this.client.request(call, credential, 'GET', input.spreadsheetId, `/values/${goPathEscape(quoteSheet(input.sheetName))}`, { majorDimension: 'ROWS', valueRenderOption: 'FORMATTED_VALUE' });
    if (exchange.error) return newMutationRetry(this.client.failure('upsertRow', 'AVAILABILITY', 'append read-back is unavailable'));
    const retry = retryStatus(exchange);
    if (retry.kind === 'invalid') return newMutationRetry(this.client.failure('upsertRow', 'PROTOCOL', 'read-back returned an invalid Retry-After header'));
    if (retry.kind === 'retry') return newMutationRetry(this.client.failure('upsertRow', statusKind(exchange.status), 'append read-back is temporarily unavailable'), retry.delay);
    if (exchange.status < 200 || exchange.status >= 300) return this.uncertain(call, exchange, 'append read-back was rejected');
    const decoded = this.client.decodeValues(exchange, 'upsertRow');
    if (!decoded.value) return this.uncertain(call, exchange, 'append read-back is invalid');
    const found = findRow(decoded.value.values, input.keyColumn, input.keyValue, 'upsertRow', this.client);
    if (found.branch === 'conflict') return newMutationBranch('conflict', emptyUpsert(), this.client.receipt(call, exchange), found.failure);
    if (found.branch !== 'found' || !rowMatches(found.value, input)) return this.uncertain(call, exchange, 'append cannot be confirmed by the row key');
    return newMutationBranch('upserted', upsertOutput('inserted', found.value.rowNumber!, input.sheetName, row.length), this.client.receipt(call, exchange));
  }

  private async retryBeforeWrite(fence: Fence, decision: string, failure: Failure, delay = 0): Promise<MutationAttempt<UpsertRowOutput>> {
    if (decision === 'sendAllowed') { try { await fence.recordNotSent(); } catch { failure = this.client.failure('upsertRow', 'AVAILABILITY', 'pre-send checkpoint failed; retry remains fail-closed'); } }
    return newMutationRetry(failure, delay);
  }

  private async retryAfterRejectedAppend(fence: Fence, exchange: SpreadsheetExchange): Promise<MutationAttempt<UpsertRowOutput>> {
    try { await fence.recordCanResend(); } catch { return newMutationRetry(this.client.failure('upsertRow', 'AVAILABILITY', 'append retry checkpoint failed')); }
    const retry = retryStatus(exchange, true);
    if (retry.kind === 'invalid') return newMutationRetry(this.client.failure('upsertRow', 'PROTOCOL', 'provider returned an invalid Retry-After header'));
    return newMutationRetry(this.client.failure('upsertRow', exchange.status ? statusKind(exchange.status) : 'TRANSPORT', 'append was not processed'), retry.delay);
  }

  private uncertain(call: Call, exchange: SpreadsheetExchange | undefined, message: string): MutationAttempt<UpsertRowOutput> { return newMutationUncertain(emptyUpsert(), this.client.failure('upsertRow', 'TRANSPORT', message), this.client.receipt(call, exchange)); }
}

function rejectedMessage(needsReauthorization: boolean): string { return needsReauthorization ? 'provider rejected the write credential; reconnect the connection' : 'provider rejected the write credential; credential refresh is temporarily unavailable'; }
function statusKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : status === 429 ? 'RATE_LIMIT' : status >= 500 ? 'AVAILABILITY' : 'PROVIDER_REJECTION'; }
function retryStatus(exchange: SpreadsheetExchange, force = false): { kind: 'none' | 'retry' | 'invalid'; delay: number } {
  if (!force && exchange.status !== 429 && exchange.status < 500) return { kind: 'none', delay: 0 };
  const value = exchange.headers['retry-after'];
  const header = Array.isArray(value) ? value[0] ?? '' : value ?? '';
  if (!header) return { kind: 'retry', delay: 0 };
  if (!/^[+]?[0-9]+$/.test(header) || Number(header) > 9_223_372_036 || !Number.isSafeInteger(Number(header))) return { kind: 'invalid', delay: 0 };
  return { kind: 'retry', delay: Number(header) * 1000 };
}
function findRow(rows: string[][], keyColumn: string, keyValue: string, operation: string, client: SpreadsheetClient): { branch: string; value: FindRowOutput; failure?: Failure } {
  if (!rows.length) return { branch: 'defect', value: {}, failure: client.failure(operation, 'VALIDATION', 'sheet must contain a header row') };
  const positions = rows[0]!.flatMap((header, index) => header === keyColumn ? [index] : []);
  if (positions.length > 1) return { branch: 'conflict', value: {}, failure: client.failure(operation, 'CONFLICT', 'key column header is duplicated') };
  if (!positions.length) return { branch: 'defect', value: {}, failure: client.failure(operation, 'VALIDATION', 'key column is missing') };
  const column = positions[0]!;
  const matches = rows.slice(1).flatMap((row, index) => column < row.length && row[column] === keyValue ? [{ rowNumber: index + 2, row }] : []);
  if (!matches.length) return { branch: 'notFound', value: {} };
  if (matches.length > 1) return { branch: 'conflict', value: { conflictingRows: matches.map(match => match.rowNumber) }, failure: client.failure(operation, 'CONFLICT', 'multiple rows contain the stable key') };
  const match = matches[0]!;
  return { branch: 'found', value: { rowNumber: match.rowNumber, values: Object.fromEntries(rows[0]!.flatMap((header, index) => index < match.row.length ? [[header, match.row[index]!]] : [])) } };
}
function buildRow(rows: string[][], input: UpsertRowInput, client: SpreadsheetClient): { row?: string[]; failure?: Failure } {
  if (!rows.length) return { failure: client.failure('upsertRow', 'VALIDATION', 'sheet must contain a header row') };
  const values = { ...input.values } as Record<string, string>;
  if (values[input.keyColumn] !== undefined && values[input.keyColumn] !== input.keyValue) return { failure: client.failure('upsertRow', 'VALIDATION', 'key column value conflicts with stable key') };
  values[input.keyColumn] = input.keyValue;
  const headers = rows[0]!;
  if (headers.some(header => !header) || new Set(headers).size !== headers.length) return { failure: client.failure('upsertRow', 'VALIDATION', 'headers must be non-empty and unique') };
  const unknown = Object.keys(values).filter(key => !headers.includes(key)).sort();
  if (unknown.length) return { failure: client.failure('upsertRow', 'VALIDATION', `values contain unknown columns: ${unknown.join(', ')}`) };
  return { row: headers.map(header => values[header] ?? '') };
}
function rowMatches(found: FindRowOutput, input: UpsertRowInput): boolean { return found.values?.[input.keyColumn] === input.keyValue && Object.entries(input.values ?? {}).every(([column, value]) => found.values?.[column] === value); }
function upsertOutput(action: string, rowNumber: number, sheetName: string, columnCount: number): UpsertRowOutput {
  let column = ''; for (let position = columnCount; position > 0; position = Math.floor((position - 1) / 26)) column = String.fromCharCode(65 + (position - 1) % 26) + column;
  return { action, rowNumber, updatedRange: `${quoteSheet(sheetName)}!A${rowNumber}:${column}${rowNumber}` };
}
function firstRowNumber(updatedRange: string): number { const separator = updatedRange.lastIndexOf('!'); if (separator < 0) return 0; const digits = /[0-9]+/.exec(updatedRange.slice(separator + 1)); const row = Number(digits?.[0] ?? 0); return Number.isSafeInteger(row) && row > 0 ? row : 0; }
