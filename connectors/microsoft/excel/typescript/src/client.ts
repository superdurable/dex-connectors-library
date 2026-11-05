// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type CredentialRefreshDriver, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { AppendTableRowsBranch, AppendTableRowsDefinition, GetTableRowsBranch, GetTableRowsDefinition, GetValuesBranch, GetValuesDefinition, UpdateValuesBranch, UpdateValuesDefinition } from './generated/definitions.js';
import type { AppendTableRowsInput, AppendTableRowsOutput, Config, CredentialSource, Credentials, GetTableRowsInput, GetTableRowsOutput, GetValuesInput, GetValuesOutput, UpdateValuesInput, UpdateValuesOutput } from './generated/models.js';
import { ExcelCredentialRefreshDriver } from './refresh.js';
import { classifyGraphError, errorTokens, excelFailure, GraphClient, pathEscape, rangePath, statusFailureKind, tablePath, type ExcelTransport, type GraphResult, type ReadFailure } from './graph.js';
import { decodeColumns, decodeRange, MissingValuesError, validateRange, validateTableLocation, validateTableText, validateUpdate, wireCell, type TableColumn } from './excel_values.js';

const emptyRows = (): GetTableRowsOutput => ({ columns: [], rows: [] });
const emptyValues = (): GetValuesOutput => ({ address: '', rowCount: 0, columnCount: 0, values: [], text: [] });
const emptyUpdate = (): UpdateValuesOutput => ({ address: '', rowCount: 0, columnCount: 0 });
const emptyAppend = (): AppendTableRowsOutput => ({ appendedKeys: [], alreadyPresentKeys: [] });

/** Optional provider transport and deterministic clock for local verification. */
export interface ExcelClientOptions {
  /** Caller-owned HTTP sender. */ transport?: ExcelTransport;
  /** Test-only local Graph endpoint ending in /v1.0. */ endpoint?: string;
  /** Clock for receipts and Retry-After dates. */ now?: () => Date;
  /** Host refresh driver, or the generated manifest implementation. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
}

/** Immutable Microsoft Graph client with four operation factories. */
export class ExcelClient {
  readonly graph: GraphClient;
  /** Validate configuration and inject a renewable host credential provider. */
  constructor(config: Config, credentials: CredentialSource, options: ExcelClientOptions = {}) {
    const now = options.now ?? (() => new Date());
    this.graph = new GraphClient(config, credentials, options.refreshDriver ?? new ExcelCredentialRefreshDriver(now),
      options.transport, options.endpoint, now);
  }
  /** Read an Excel table as column-keyed rows. */
  getTableRows(): Query<GetTableRowsInput, GetTableRowsOutput> { return new GetTableRowsOperation(this.graph); }
  /** Read one bounded Excel range. */
  getValues(): Query<GetValuesInput, GetValuesOutput> { return new GetValuesOperation(this.graph); }
  /** Write literal values to one bounded Excel range. */
  updateValues(): Mutation<UpdateValuesInput, UpdateValuesOutput> { return new UpdateValuesOperation(this.graph); }
  /** Append table rows once after key-column read-back. */
  appendTableRows(): Mutation<AppendTableRowsInput, AppendTableRowsOutput> { return new AppendTableRowsOperation(this.graph); }
}

/** Read one bounded range from Graph. */
export class GetValuesOperation implements Query<GetValuesInput, GetValuesOutput> {
  constructor(readonly graph: GraphClient) {}
  definition() { return GetValuesDefinition; }
  async invoke(call: Call, input: GetValuesInput): Promise<QueryAttempt<GetValuesOutput>> {
    let address: string; let rows: number; let columns: number;
    try { const range = validateRange(input, this.graph.maxCells); address = range.address; rows = range.rowCount; columns = range.columnCount; }
    catch (error) { return newQueryBranch(GetValuesBranch.Defect, emptyValues(), zeroReceipt(), excelFailure('getValues', 'VALIDATION', message(error))); }
    let credentials: Credentials;
    try { credentials = await this.graph.credentials(call); }
    catch { return newQueryBranch(GetValuesBranch.Defect, emptyValues(), zeroReceipt(), excelFailure('getValues', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    const response = await this.graph.request(call, credentials, 'GET', `${rangePath(input.driveId, input.workbookId, input.worksheet, address)}?$select=address,rowCount,columnCount,values,text`);
    const failure = this.graph.readFailure(call, 'getValues', response, GetValuesBranch);
    if (failure) return queryFailure(failure, emptyValues());
    const receipt = this.graph.receipt(call, response);
    try {
      const range = decodeRange(response.body, true);
      if (range.rowCount !== rows || range.columnCount !== columns) throw new TypeError();
      return newQueryBranch(GetValuesBranch.Read, { address: range.address, rowCount: rows, columnCount: columns, values: range.values, text: range.text }, receipt);
    } catch { return newQueryBranch(GetValuesBranch.InvalidResponse, emptyValues(), receipt, excelFailure('getValues', 'PROTOCOL', 'provider returned an invalid range response')); }
  }
}

/** Read one table's columns and data rows. */
export class GetTableRowsOperation implements Query<GetTableRowsInput, GetTableRowsOutput> {
  constructor(readonly graph: GraphClient) {}
  definition() { return GetTableRowsDefinition; }
  async invoke(call: Call, input: GetTableRowsInput): Promise<QueryAttempt<GetTableRowsOutput>> {
    try { validateTableLocation(input); }
    catch (error) { return newQueryBranch(GetTableRowsBranch.Defect, emptyRows(), zeroReceipt(), excelFailure('getTableRows', 'VALIDATION', message(error))); }
    let credentials: Credentials;
    try { credentials = await this.graph.credentials(call); }
    catch { return newQueryBranch(GetTableRowsBranch.Defect, emptyRows(), zeroReceipt(), excelFailure('getTableRows', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    const table = tablePath(input.driveId, input.workbookId, input.table);
    const columnsResponse = await this.graph.request(call, credentials, 'GET', `${table}/columns?$select=id,index,name`);
    credentials = columnsResponse.credentials;
    const columnsFailure = this.graph.readFailure(call, 'getTableRows', columnsResponse, GetTableRowsBranch);
    if (columnsFailure) return queryFailure(columnsFailure, emptyRows());
    let columns: TableColumn[];
    try { columns = decodeColumns(columnsResponse.body); }
    catch { return newQueryBranch(GetTableRowsBranch.InvalidResponse, emptyRows(), this.graph.receipt(call, columnsResponse), excelFailure('getTableRows', 'PROTOCOL', 'provider returned an invalid table columns response')); }
    const dataResponse = await this.graph.request(call, credentials, 'GET', `${table}/dataBodyRange?$select=address,rowCount,columnCount,values`);
    const dataFailure = this.graph.readFailure(call, 'getTableRows', dataResponse, GetTableRowsBranch);
    if (dataFailure) return queryFailure(dataFailure, emptyRows());
    const receipt = this.graph.receipt(call, dataResponse);
    try {
      const range = decodeRange(dataResponse.body, false);
      if (range.rowCount * range.columnCount > this.graph.maxCells) return newQueryBranch(GetTableRowsBranch.TooLarge, emptyRows(), receipt, excelFailure('getTableRows', 'RESPONSE_TOO_LARGE', 'table holds more cells than the configured cell limit'));
      if (range.columnCount !== columns.length) return newQueryBranch(GetTableRowsBranch.InvalidResponse, emptyRows(), receipt, excelFailure('getTableRows', 'PROTOCOL', 'table columns changed while the table was read; read it again'));
      const rows = range.values.map((cells, index) => ({ index, values: Object.fromEntries(columns.map((column, position) => [column.name, cells[position] ?? null])) }));
      return newQueryBranch(GetTableRowsBranch.Read, { columns: columns.map(column => column.name), rows }, receipt);
    } catch (error) {
      if (error instanceof MissingValuesError) return newQueryBranch(GetTableRowsBranch.TooLarge, emptyRows(), receipt, excelFailure('getTableRows', 'RESPONSE_TOO_LARGE', 'table holds more cells than the configured cell limit'));
      return newQueryBranch(GetTableRowsBranch.InvalidResponse, emptyRows(), receipt, excelFailure('getTableRows', 'PROTOCOL', 'provider returned an invalid table data response'));
    }
  }
}

/** Absolute range write; retrying writes the same values again. */
export class UpdateValuesOperation implements Mutation<UpdateValuesInput, UpdateValuesOutput> {
  constructor(readonly graph: GraphClient) {}
  definition() { return UpdateValuesDefinition; }
  idempotencyKey(callId: string): string { return callId; }
  async invoke(call: Call, input: UpdateValuesInput): Promise<MutationAttempt<UpdateValuesOutput>> {
    let validated: ReturnType<typeof validateUpdate>;
    try { validated = validateUpdate(input, this.graph.maxCells); }
    catch (error) { return newMutationBranch(UpdateValuesBranch.Defect, emptyUpdate(), zeroReceipt(), excelFailure('updateValues', 'VALIDATION', message(error))); }
    let credentials: Credentials;
    try { credentials = await this.graph.credentials(call); }
    catch { return newMutationBranch(UpdateValuesBranch.Defect, emptyUpdate(), zeroReceipt(), excelFailure('updateValues', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    const response = await this.graph.request(call, credentials, 'PATCH', rangePath(input.driveId, input.workbookId, input.worksheet, validated.range.address), { values: validated.values });
    const receipt = this.graph.receipt(call, response);
    if (response.transportFailure) return newMutationRetry(excelFailure('updateValues', 'TRANSPORT', 'provider is unavailable; the same values are written again'));
    if (response.status >= 200 && response.status < 300) {
      let address = validated.range.address;
      try { const parsed = JSON.parse(new TextDecoder().decode(response.body)) as { address?: unknown }; if (typeof parsed.address === 'string' && parsed.address) address = parsed.address; } catch { /* Go retains the requested address. */ }
      return newMutationBranch(UpdateValuesBranch.Updated, { address, rowCount: validated.range.rowCount, columnCount: validated.range.columnCount }, receipt);
    }
    const tokens = errorTokens(response.body); const kind = statusFailureKind(response.status, tokens);
    switch (classifyGraphError(response.status, tokens)) {
      case 'retry': return newMutationRetry(excelFailure('updateValues', kind, 'provider temporarily rejected the write'), this.graph.retryAfter(response.headers));
      case 'notFound': return newMutationBranch(UpdateValuesBranch.NotFound, emptyUpdate(), receipt, excelFailure('updateValues', 'NOT_FOUND', 'workbook or worksheet was not found or is not visible to the connection'));
      default: return newMutationBranch(UpdateValuesBranch.ProviderRejected, emptyUpdate(), receipt, excelFailure('updateValues', kind, 'provider rejected the write'));
    }
  }
}

/** Key-checked table append with a persisted single-dispatch marker. */
export class AppendTableRowsOperation implements Mutation<AppendTableRowsInput, AppendTableRowsOutput> {
  constructor(readonly graph: GraphClient) {}
  definition() { return AppendTableRowsDefinition; }
  idempotencyKey(callId: string): string { return callId; }
  async invoke(call: Call, input: AppendTableRowsInput): Promise<MutationAttempt<AppendTableRowsOutput>> {
    let keys: Array<string | number | boolean>;
    try { keys = validateAppendInput(input, this.graph.maxCells); }
    catch (error) { return newMutationBranch(AppendTableRowsBranch.Defect, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'VALIDATION', message(error))); }
    let credentials: Credentials;
    try { credentials = await this.graph.credentials(call); }
    catch { return newMutationBranch(AppendTableRowsBranch.Defect, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    const table = tablePath(input.driveId, input.workbookId, input.table);
    const columnsResponse = await this.graph.request(call, credentials, 'GET', `${table}/columns?$select=id,index,name`);
    credentials = columnsResponse.credentials;
    const columnsFailure = this.graph.readFailure(call, 'appendTableRows', columnsResponse, AppendTableRowsBranch);
    if (columnsFailure) return mutationFailure(columnsFailure, emptyAppend());
    let columns: TableColumn[];
    try { columns = decodeColumns(columnsResponse.body); }
    catch { return newMutationBranch(AppendTableRowsBranch.InvalidResponse, emptyAppend(), this.graph.receipt(call, columnsResponse), excelFailure('appendTableRows', 'PROTOCOL', 'provider returned an invalid table columns response')); }
    const keyColumn = columns.find(column => column.name === input.keyColumn);
    if (!keyColumn || input.rows!.some(row => Object.keys(row!).some(name => !columns.some(column => column.name === name))))
      return newMutationBranch(AppendTableRowsBranch.ColumnsMismatch, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'VALIDATION', "the table has no column with the key column's name or with a name a row uses"));
    if (input.rows!.length * columns.length > this.graph.maxCells)
      return newMutationBranch(AppendTableRowsBranch.Defect, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'VALIDATION', 'rows times table columns exceed the configured cell limit'));
    const keyResponse = await this.graph.request(call, credentials, 'GET', `${table}/columns/${pathEscape(keyColumn.id)}/dataBodyRange?$select=address,rowCount,columnCount,values`);
    credentials = keyResponse.credentials;
    const keyFailure = this.graph.readFailure(call, 'appendTableRows', keyResponse, AppendTableRowsBranch);
    if (keyFailure) return mutationFailure(keyFailure, emptyAppend());
    const lookupReceipt = this.graph.receipt(call, keyResponse);
    let stored: Array<string | number | boolean | null>;
    try { const range = decodeRange(keyResponse.body, false); if (range.columnCount !== 1) throw new TypeError(); stored = range.values.map(row => row[0] ?? null); }
    catch { return newMutationBranch(AppendTableRowsBranch.InvalidResponse, emptyAppend(), lookupReceipt, excelFailure('appendTableRows', 'PROTOCOL', 'provider returned an invalid key column response before the append was sent')); }
    const missingIndexes: number[] = []; const presentKeys: typeof keys = []; const missingKeys: typeof keys = [];
    keys.forEach((key, index) => {
      if (stored.some(value => matchesStoredKey(key, value))) presentKeys.push(key);
      else { missingIndexes.push(index); missingKeys.push(key); }
    });
    let earlierDispatch = true;
    try { earlierDispatch = call.context.getLastHeartbeatValue() !== undefined; } catch { /* An unreadable marker forbids a repeat. */ }
    if (!missingIndexes.length) return newMutationBranch(AppendTableRowsBranch.Appended, { appendedKeys: [], alreadyPresentKeys: presentKeys, ...(earlierDispatch ? { isFromEarlierAttempt: true } : {}) }, lookupReceipt);
    const body = { values: missingIndexes.map(index => columns.map(column => wireCell(input.rows![index]![column.name] ?? ''))) };
    if (earlierDispatch) return newMutationUncertain({ appendedKeys: [], alreadyPresentKeys: presentKeys }, excelFailure('appendTableRows', 'TRANSPORT', 'an earlier attempt of this Step sent the append, and the key column does not show every row yet'), lookupReceipt);
    try { await call.context.recordHeartbeat({ microsoftExcelDispatchedCallId: call.id }); }
    catch { return newMutationRetry(excelFailure('appendTableRows', 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
    const response = await this.graph.request(call, credentials, 'POST', `${table}/rows/add`, body);
    return this.appendResponse(call, response, missingKeys, presentKeys);
  }

  private async appendResponse(call: Call, response: GraphResult, missingKeys: Array<string | number | boolean>, presentKeys: Array<string | number | boolean>): Promise<MutationAttempt<AppendTableRowsOutput>> {
    const receipt = this.graph.receipt(call, response);
    if (response.transportFailure) return newMutationRetry(excelFailure('appendTableRows', 'TRANSPORT', 'append outcome is unknown; the next attempt checks the key column instead of sending again'), 10_000);
    if (response.status >= 200 && response.status < 300) return newMutationBranch(AppendTableRowsBranch.Appended, { appendedKeys: missingKeys, alreadyPresentKeys: presentKeys }, receipt);
    const tokens = errorTokens(response.body); const kind = statusFailureKind(response.status, tokens);
    if (!isAppendProvablyRefused(response.status)) return newMutationRetry(excelFailure('appendTableRows', kind, 'append outcome is unknown; the next attempt checks the key column instead of sending again'), 10_000);
    try { await call.context.recordHeartbeat(undefined); } catch { /* An uncleared marker prevents a duplicate append. */ }
    switch (classifyGraphError(response.status, tokens)) {
      case 'retry': return newMutationRetry(excelFailure('appendTableRows', kind, 'provider temporarily refused the append; nothing was written'), this.graph.retryAfter(response.headers));
      case 'notFound': return newMutationBranch(AppendTableRowsBranch.NotFound, emptyAppend(), receipt, excelFailure('appendTableRows', 'NOT_FOUND', 'workbook or table was not found or is not visible to the connection'));
      default: return newMutationBranch(AppendTableRowsBranch.ProviderRejected, emptyAppend(), receipt, excelFailure('appendTableRows', kind, 'provider rejected the append; nothing was written'));
    }
  }
}

function message(error: unknown): string { return error instanceof Error ? error.message : 'invalid Microsoft Excel input'; }
function queryFailure<T>(failure: ReadFailure, empty: T): QueryAttempt<T> { return failure.branch ? newQueryBranch(failure.branch, empty, failure.receipt, failure.failure) : newQueryRetry(failure.failure, failure.retryAfterMs); }
function mutationFailure<T>(failure: ReadFailure, empty: T): MutationAttempt<T> { return failure.branch ? newMutationBranch(failure.branch, empty, failure.receipt, failure.failure) : newMutationRetry(failure.failure, failure.retryAfterMs); }
function matchesStoredKey(key: string | number | boolean, stored: unknown): boolean { return typeof key === 'string' ? stored === key || stored === `'${key}` : stored === key; }
function isAppendProvablyRefused(status: number): boolean { return status >= 400 && status < 500 && status !== 408 || status === 501 || status === 503; }
function validateAppendInput(input: AppendTableRowsInput, maxCells: number): Array<string | number | boolean> {
  validateTableLocation(input); validateTableText(input.keyColumn, 'column');
  if (!Array.isArray(input.rows) || input.rows.length < 1 || input.rows.length > maxCells) throw new TypeError('rows must hold at least one row and at most the configured cell limit');
  const keys: Array<string | number | boolean> = []; const seen = new Set<string>();
  for (const [index, row] of input.rows.entries()) {
    const key = row?.[input.keyColumn];
    if (key === '' || !['string', 'number', 'boolean'].includes(typeof key) || typeof key === 'number' && !Number.isFinite(key))
      throw new TypeError(`row ${index + 1} must set the key column to non-empty text, a finite number, or a Boolean`);
    const identity = `${typeof key}:${String(key)}`;
    if (seen.has(identity)) throw new TypeError(`row ${index + 1} repeats the key of an earlier row`);
    seen.add(identity); keys.push(key as string | number | boolean);
    for (const [name, value] of Object.entries(row!)) { validateTableText(name, 'column'); wireCell(value); }
  }
  return keys;
}
