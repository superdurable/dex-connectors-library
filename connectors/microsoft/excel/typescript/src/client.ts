// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type CredentialRefreshDriver, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CheckpointError, DispatchNotAllowedError, InsufficientTimeError, newFence, type DecisionKind, type Fence } from '@superdurable/connectors-sdk/singledispatch';
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
    // The write is repeat-safe, so an unknown outcome is written again.
    if (!definiteRefusalStatuses.has(response.status)) return newMutationRetry(excelFailure('updateValues', kind, 'provider did not confirm the write; the same values are written again'), this.graph.retryAfter(response.headers));
    switch (classifyGraphError(response.status, tokens)) {
      case 'retry': return newMutationRetry(excelFailure('updateValues', kind, 'provider temporarily rejected the write'), this.graph.retryAfter(response.headers));
      case 'notFound': return newMutationBranch(UpdateValuesBranch.NotFound, emptyUpdate(), receipt, excelFailure('updateValues', 'NOT_FOUND', 'workbook or worksheet was not found or is not visible to the connection'));
      default: return newMutationBranch(UpdateValuesBranch.ProviderRejected, emptyUpdate(), receipt, excelFailure('updateValues', kind, 'provider rejected the write'));
    }
  }
}

/**
 * Key-checked table append. A single-dispatch fence counts possible sends; every attempt reads the
 * key column first, resends only missing rows while possible sends stay below the send cap, and
 * selects uncertain at the cap.
 */
export class AppendTableRowsOperation implements Mutation<AppendTableRowsInput, AppendTableRowsOutput> {
  constructor(readonly graph: GraphClient) {}
  definition() { return AppendTableRowsDefinition; }
  idempotencyKey(callId: string): string { return callId; }
  async invoke(call: Call, input: AppendTableRowsInput): Promise<MutationAttempt<AppendTableRowsOutput>> {
    let keys: Array<string | number | boolean>;
    try { keys = validateAppendInput(input, this.graph.maxCells); }
    catch (error) { return newMutationBranch(AppendTableRowsBranch.Defect, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'VALIDATION', message(error))); }
    const attempt = decideAppendAttempt(call);
    if (!(attempt instanceof AppendAttempt)) return attempt;
    let credentials: Credentials;
    try { credentials = await this.graph.credentials(call); }
    catch { return attempt.localFailure(AppendTableRowsBranch.Defect, excelFailure('appendTableRows', 'AUTHENTICATION', 'connection credentials are unavailable'), zeroReceipt()); }
    const table = tablePath(input.driveId, input.workbookId, input.table);
    const columnsResponse = await this.graph.request(call, credentials, 'GET', `${table}/columns?$select=id,index,name`);
    credentials = columnsResponse.credentials;
    const columnsFailure = this.graph.readFailure(call, 'appendTableRows', columnsResponse, AppendTableRowsBranch);
    if (columnsFailure) return attempt.readFailure(columnsFailure);
    let columns: TableColumn[];
    try { columns = decodeColumns(columnsResponse.body); }
    catch { return attempt.localFailure(AppendTableRowsBranch.InvalidResponse, excelFailure('appendTableRows', 'PROTOCOL', 'provider returned an invalid table columns response'), this.graph.receipt(call, columnsResponse)); }
    const keyColumn = columns.find(column => column.name === input.keyColumn);
    if (!keyColumn || input.rows!.some(row => Object.keys(row!).some(name => !columns.some(column => column.name === name))))
      return attempt.localFailure(AppendTableRowsBranch.ColumnsMismatch, excelFailure('appendTableRows', 'VALIDATION', "the table has no column with the key column's name or with a name a row uses"), zeroReceipt());
    if (input.rows!.length * columns.length > this.graph.maxCells)
      return attempt.localFailure(AppendTableRowsBranch.Defect, excelFailure('appendTableRows', 'VALIDATION', 'rows times table columns exceed the configured cell limit'), zeroReceipt());
    const keyResponse = await this.graph.request(call, credentials, 'GET', `${table}/columns/${pathEscape(keyColumn.id)}/dataBodyRange?$select=address,rowCount,columnCount,values`);
    credentials = keyResponse.credentials;
    const keyFailure = this.graph.readFailure(call, 'appendTableRows', keyResponse, AppendTableRowsBranch);
    if (keyFailure) return attempt.readFailure(keyFailure);
    const lookupReceipt = this.graph.receipt(call, keyResponse);
    let stored: Array<string | number | boolean | null>;
    try { const range = decodeRange(keyResponse.body, false); if (range.columnCount !== 1) throw new TypeError(); stored = range.values.map(row => row[0] ?? null); }
    catch { return attempt.localFailure(AppendTableRowsBranch.InvalidResponse, excelFailure('appendTableRows', 'PROTOCOL', 'provider returned an invalid key column response before the append was sent'), lookupReceipt); }
    const missingIndexes: number[] = []; const presentKeys: typeof keys = []; const missingKeys: typeof keys = [];
    keys.forEach((key, index) => {
      if (stored.some(value => matchesStoredKey(key, value))) presentKeys.push(key);
      else { missingIndexes.push(index); missingKeys.push(key); }
    });
    if (!missingIndexes.length) return newMutationBranch(AppendTableRowsBranch.Appended, { appendedKeys: [], alreadyPresentKeys: presentKeys, ...(attempt.hasPossibleEarlierSend ? { isFromEarlierAttempt: true } : {}) }, lookupReceipt);
    const body = { values: missingIndexes.map(index => columns.map(column => wireCell(input.rows![index]![column.name] ?? ''))) };
    if (attempt.isSendForbidden || !attempt.fence)
      return newMutationUncertain({ appendedKeys: [], alreadyPresentKeys: presentKeys }, excelFailure('appendTableRows', 'TRANSPORT', 'the append reached the send cap, and the key column does not show every row yet'), lookupReceipt);
    let response: GraphResult;
    try { response = await attempt.fence.dispatch(() => this.graph.request(call, credentials, 'POST', `${table}/rows/add`, body)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof DispatchNotAllowedError)
        return newMutationRetry(excelFailure('appendTableRows', 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent'));
      return newMutationRetry(excelFailure('appendTableRows', 'TRANSPORT', 'append outcome is unknown; the next attempt checks the key column before any resend'), unknownAppendRetryDelayMs);
    }
    return this.appendResponse(call, attempt, response, missingKeys, presentKeys);
  }

  private async appendResponse(call: Call, attempt: AppendAttempt, response: GraphResult, missingKeys: Array<string | number | boolean>, presentKeys: Array<string | number | boolean>): Promise<MutationAttempt<AppendTableRowsOutput>> {
    const receipt = this.graph.receipt(call, response);
    if (response.transportFailure) return newMutationRetry(excelFailure('appendTableRows', 'TRANSPORT', 'append outcome is unknown; the next attempt checks the key column before any resend'), unknownAppendRetryDelayMs);
    if (response.status >= 200 && response.status < 300) return newMutationBranch(AppendTableRowsBranch.Appended, { appendedKeys: missingKeys, alreadyPresentKeys: presentKeys }, receipt);
    const tokens = errorTokens(response.body); const kind = statusFailureKind(response.status, tokens);
    if (response.status === 429 || response.status === 503) {
      await attempt.recordCanResend();
      return newMutationRetry(excelFailure('appendTableRows', kind, 'provider temporarily refused the append; nothing was written'), this.graph.retryAfter(response.headers));
    }
    if (!isDefiniteAppendRefusal(response)) return newMutationRetry(excelFailure('appendTableRows', kind, 'append outcome is unknown; the next attempt checks the key column before any resend'), unknownAppendRetryDelayMs);
    await attempt.recordCanResend();
    const errorClass = classifyGraphError(response.status, tokens);
    if (errorClass === 'retry') return newMutationRetry(excelFailure('appendTableRows', kind, 'provider temporarily refused the append; nothing was written'), this.graph.retryAfter(response.headers));
    if (attempt.hasPossibleEarlierSend)
      return newMutationUncertain({ appendedKeys: [], alreadyPresentKeys: presentKeys }, excelFailure('appendTableRows', kind, 'provider refused the append after an earlier unconfirmed send, and the key column does not show every row'), receipt);
    if (errorClass === 'notFound') return newMutationBranch(AppendTableRowsBranch.NotFound, emptyAppend(), receipt, excelFailure('appendTableRows', 'NOT_FOUND', 'workbook or table was not found or is not visible to the connection'));
    return newMutationBranch(AppendTableRowsBranch.ProviderRejected, emptyAppend(), receipt, excelFailure('appendTableRows', kind, 'provider rejected the append; nothing was written'));
  }
}

// A queued append gets time to land before the next attempt reads the key column.
const unknownAppendRetryDelayMs = 10_000;
// Only these statuses prove Microsoft refused a write; every other status is an unknown outcome.
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Single-dispatch state of one appendTableRows attempt. */
class AppendAttempt {
  constructor(
    /** Fence of this attempt, or undefined after an older release's marker. */ readonly fence: Fence | undefined,
    /** Fence decision kind, when a fence exists. */ readonly decisionKind: DecisionKind | undefined,
    /** A refusal or local failure cannot prove nothing was appended. */ readonly hasPossibleEarlierSend: boolean,
    /** The send cap is reached, or an older release's marker forbids a resend. */ readonly isSendForbidden: boolean,
  ) {}

  /** Select branch only when no earlier send may have appended rows. */
  localFailure(branch: string, failure: Failure, receipt: ReceiptDraft): MutationAttempt<AppendTableRowsOutput> {
    if (this.hasPossibleEarlierSend) return newMutationUncertain(emptyAppend(), { ...failure, message: `${failure.message}; an earlier attempt may have appended the rows` }, receipt);
    return newMutationBranch(branch, emptyAppend(), receipt, failure);
  }

  /** Keep a pre-send read Retry countable and guard a terminal read after a possible send. */
  async readFailure(failure: ReadFailure): Promise<MutationAttempt<AppendTableRowsOutput>> {
    if (failure.branch) return this.localFailure(failure.branch, failure.failure, failure.receipt);
    if (this.fence && this.decisionKind === 'sendAllowed') {
      try { await this.fence.recordNotSent(); } catch { /* A lost notSent only overcounts sends, so the cap still holds. */ }
    }
    return newMutationRetry(failure.failure, failure.retryAfterMs);
  }

  /** Record that Microsoft did not apply this attempt's append. */
  async recordCanResend(): Promise<void> {
    if (!this.fence) return;
    try { await this.fence.recordCanResend(); } catch { /* A lost canResend counts this send, so the cap still holds. */ }
  }
}

/** Decide before any credential lookup whether this attempt may send. */
function decideAppendAttempt(call: Call): AppendAttempt | MutationAttempt<AppendTableRowsOutput> {
  if (hasLegacyDispatchMarker(call)) return new AppendAttempt(undefined, undefined, true, true);
  let fence: Fence;
  try { fence = newFence(call, AppendTableRowsDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: definitionMaxSends(AppendTableRowsDefinition) }); }
  catch { return newMutationBranch(AppendTableRowsBranch.Defect, emptyAppend(), zeroReceipt(), excelFailure('appendTableRows', 'LOCAL_DEFECT', 'append dispatch fence could not be constructed')); }
  const decision = fence.decide();
  if (decision.kind === 'failClosed') return newMutationRetry(excelFailure('appendTableRows', 'AVAILABILITY', 'the append dispatch checkpoint is unreadable; nothing was sent in this attempt'));
  return new AppendAttempt(fence, decision.kind, (decision.priorSendUpperBound ?? 0) > 0, decision.kind === 'readBackRequired');
}

/** An older release's marker kept no send count, so its append is read back and never resent. */
function hasLegacyDispatchMarker(call: Call): boolean {
  try {
    const marker = call.context.getLastHeartbeatValue() as { microsoftExcelDispatchedCallId?: unknown } | undefined;
    return typeof marker === 'object' && marker !== null && marker.microsoftExcelDispatchedCallId === call.id;
  } catch { return false; }
}

/** Report a refusal status whose body carries no appended table row. */
function isDefiniteAppendRefusal(response: GraphResult): boolean {
  if (!definiteRefusalStatuses.has(response.status)) return false;
  try {
    const document = JSON.parse(new TextDecoder().decode(response.body)) as unknown;
    const index = typeof document === 'object' && document !== null && !Array.isArray(document) ? (document as { index?: unknown }).index : undefined;
    return !(typeof index === 'number' && Number.isInteger(index));
  } catch { return true; }
}

function message(error: unknown): string { return error instanceof Error ? error.message : 'invalid Microsoft Excel input'; }
function queryFailure<T>(failure: ReadFailure, empty: T): QueryAttempt<T> { return failure.branch ? newQueryBranch(failure.branch, empty, failure.receipt, failure.failure) : newQueryRetry(failure.failure, failure.retryAfterMs); }
function matchesStoredKey(key: string | number | boolean, stored: unknown): boolean { return typeof key === 'string' ? stored === key || stored === `'${key}` : stored === key; }
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
