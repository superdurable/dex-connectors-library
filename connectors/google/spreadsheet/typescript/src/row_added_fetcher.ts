// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { logTriggerRecord, withTriggerLoggerAttributes, type TriggerBindingRef, type TriggerContext, type TriggerEvent, type TriggerLogAttributes, type TriggerLogger, type TriggerLogLevel } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { goFormatValue, type SpreadsheetClient, type SpreadsheetExchange } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import { decodeRowAddedCursorToken, encodeRowAddedCursorToken, hashRowCells, headerKeyedValues, rowAddedEventId, withoutTrailingEmptyCells, type RowAddedCursorToken } from './row_added_cursor.js';
import { goTrimSpace, validateRowAddedTriggerConfiguration, type RowAddedEvent, type RowAddedTriggerConfiguration } from './row_added_models.js';

const maxScanWindows = 100;
const minScanWindow = 100;
const maxRetryAfterSeconds = 9_223_372_036;

/** A poll response exceeded maxResponseBytes; a scan halves its window and reads it again. */
class ResponseTooLargeError extends Error {}

interface TabScan { lastRow: number; lastRowCells: string[]; matchingRows: number[]; cursorRowCells: string[] }

/** Reads one tab's rows after the cursor row through values:batchGet, at behavior parity with the Go connector. */
export class RowAddedFetcher implements ChangeFetcher<TriggerEvent<RowAddedEvent>> {
  readonly #client: SpreadsheetClient;
  readonly #binding: TriggerBindingRef;
  readonly #spreadsheetId: string;
  readonly #sheetName: string;
  readonly #logger: TriggerLogger;

  /** Bind one binding's tab; throws for a blank spreadsheet ID or tab title. */
  constructor(client: SpreadsheetClient, binding: TriggerBindingRef, configuration: RowAddedTriggerConfiguration, logger?: TriggerLogger) {
    validateRowAddedTriggerConfiguration(configuration);
    this.#client = client;
    this.#binding = binding;
    this.#spreadsheetId = goTrimSpace(configuration.spreadsheetId);
    this.#sheetName = configuration.sheetName;
    this.#logger = withTriggerLoggerAttributes(logger, { connector: ConnectorID, connection: binding.connection.name, trigger: binding.trigger.triggerName, binding: binding.name });
  }

  /** Position the cursor after the tab's current last row, so existing rows are not delivered. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const scan = await this.#scanTab(context, undefined);
    return { token: encodeRowAddedCursorToken({ lastRow: scan.lastRow, rowHash: hashRowCells(scan.lastRowCells) }) };
  }

  /** Read the header row and up to pollPageSize rows after the cursor row, realigning a moved cursor first. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<RowAddedEvent>>> {
    let token: RowAddedCursorToken;
    try { token = decodeRowAddedCursorToken(cursor.token); } catch (error) { throw markCursorInvalid(error); }
    let { header, window } = await this.#readWindow(context, token.lastRow);
    if (!windowStartsAtCursorRow(window, token)) {
      token = await this.#realignCursor(context, token);
      ({ header, window } = await this.#readWindow(context, token.lastRow));
      if (!windowStartsAtCursorRow(window, token)) throw new Error('Google Sheets rows moved during the rowAdded poll');
    }
    const occurredAt = this.#client.now();
    const changes: TriggerEvent<RowAddedEvent>[] = [];
    for (let offset = 1; offset < window.length; offset++) {
      const cells = withoutTrailingEmptyCells(window[offset]!);
      if (cells.length === 0) continue;
      const rowNumber = token.lastRow + offset;
      changes.push({
        id: rowAddedEventId(this.#spreadsheetId, this.#sheetName, rowNumber, cells), occurredAt,
        payload: { spreadsheetId: this.#spreadsheetId, sheetName: this.#sheetName, rowNumber, values: headerKeyedValues(header, cells), cells },
      });
    }
    if (window.length > 1) token = { lastRow: token.lastRow + window.length - 1, rowHash: hashRowCells(window.at(-1)) };
    return { changes, next: { token: encodeRowAddedCursorToken(token) }, hasMorePages: window.length === this.#client.pollPageSize + 1 };
  }

  /**
   * Find the cursor row's cells nearest its old number after rows above it moved. Without a match the row was
   * edited in place and keeps its number; when the tab now ends above it, the cursor is invalid.
   */
  async #realignCursor(context: TriggerContext, token: RowAddedCursorToken): Promise<RowAddedCursorToken> {
    const scan = await this.#scanTab(context, token);
    let nearest = 0;
    for (const rowNumber of scan.matchingRows) {
      if (nearest === 0 || Math.abs(rowNumber - token.lastRow) < Math.abs(nearest - token.lastRow)) nearest = rowNumber;
    }
    if (nearest !== 0) {
      this.#log(context, 'info', 'sheet rows moved; cursor realigned', { from_row: token.lastRow, to_row: nearest });
      return { lastRow: nearest, rowHash: token.rowHash };
    }
    if (token.lastRow <= scan.lastRow) {
      this.#log(context, 'info', 'last processed row was edited; keeping its position', { row: token.lastRow });
      return { lastRow: token.lastRow, rowHash: hashRowCells(scan.cursorRowCells) };
    }
    throw markCursorInvalid(new Error(`Google Sheets tab now ends at row ${scan.lastRow}, above the last processed row ${token.lastRow}`));
  }

  /** Read the tab from row 1 in maxRows windows until one ends early, halving a window whose response is too large. */
  async #scanTab(context: TriggerContext, cursor: RowAddedCursorToken | undefined): Promise<TabScan> {
    const scan: TabScan = { lastRow: 1, lastRowCells: [], matchingRows: [], cursorRowCells: [] };
    let windowSize = this.#client.maxRows;
    const minWindowSize = Math.min(windowSize, minScanWindow);
    const maxScanRows = maxScanWindows * windowSize;
    for (let firstRow = 1; firstRow <= maxScanRows;) {
      let ranges: string[][][];
      try { ranges = await this.#readRanges(context, [this.#rowRange(firstRow, firstRow + windowSize - 1)]); } catch (error) {
        if (!(error instanceof ResponseTooLargeError) || windowSize <= minWindowSize) throw error;
        windowSize = Math.max(Math.floor(windowSize / 2), minWindowSize);
        this.#log(context, 'debug', 'sheet scan window exceeds maxResponseBytes; halving it', { window_rows: windowSize });
        continue;
      }
      const rows = ranges[0]!;
      rows.forEach((row, offset) => {
        const rowNumber = firstRow + offset;
        if (cursor && rowNumber > 1 && hashRowCells(row) === cursor.rowHash) scan.matchingRows.push(rowNumber);
        if (rowNumber === cursor?.lastRow) scan.cursorRowCells = row;
      });
      if (rows.length > 0) { scan.lastRow = firstRow + rows.length - 1; scan.lastRowCells = rows.at(-1)!; }
      if (rows.length < windowSize) return scan;
      firstRow += windowSize;
    }
    throw new Error(`Google Sheets tab has more than ${maxScanRows} rows to scan`);
  }

  /** Return the header row and the rows from lastRow through lastRow+pollPageSize. */
  async #readWindow(context: TriggerContext, lastRow: number): Promise<{ header: string[]; window: string[][] }> {
    const ranges = await this.#readRanges(context, [this.#rowRange(1, 1), this.#rowRange(lastRow, lastRow + this.#client.pollPageSize)]);
    return { header: ranges[0]![0] ?? [], window: ranges[1]! };
  }

  /** Make one values:batchGet request for whole-row ranges and return each range's rows. */
  async #readRanges(context: TriggerContext, ranges: string[]): Promise<string[][][]> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.#binding.connection, this.#binding.trigger, context.signal);
    const resolved = await this.#client.resolve(call);
    if (!resolved.credential) throw new Error(`Google Sheets rowAdded poll ${resolved.failure?.message ?? 'credentials are unavailable'}`);
    const query = { ranges, majorDimension: 'ROWS', valueRenderOption: 'FORMATTED_VALUE' };
    const { exchange } = await this.#client.request(call, resolved.credential, 'GET', this.#spreadsheetId, '/values:batchGet', query, undefined, false, context.signal);
    if (exchange.isTooLarge) throw new ResponseTooLargeError('Google Sheets rowAdded poll provider response exceeds configured limit');
    if (exchange.error !== undefined) throw new Error('Google Sheets rowAdded poll provider request failed');
    if (exchange.status === 429) {
      // An unreadable Retry-After falls back to the runner's exponential backoff.
      throw markRateLimited(new Error('Google Sheets rate limited the rowAdded poll with HTTP 429'), retryAfterMilliseconds(exchange));
    }
    if (exchange.status < 200 || exchange.status >= 300) throw new Error(`Google Sheets rejected the rowAdded poll with HTTP ${exchange.status}`);
    const valueRanges = decodeValueRanges(exchange.body);
    if (!valueRanges || valueRanges.length !== ranges.length) throw new Error('Google Sheets returned an invalid rowAdded poll response');
    const maxRows = Math.max(this.#client.maxRows, this.#client.pollPageSize + 1);
    if (valueRanges.some(rows => rows.length > maxRows)) throw new Error('Google Sheets returned more rows than the rowAdded poll requested');
    return valueRanges;
  }

  /** Return the A1 whole-row range of the polled tab, such as 'Leads'!5:105. */
  #rowRange(firstRow: number, lastRow: number): string { return `'${this.#sheetName.replaceAll("'", "''")}'!${firstRow}:${lastRow}`; }

  /** Write one record with the binding identity; never cell values or the tab title. */
  #log(context: TriggerContext, level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes): void { logTriggerRecord(this.#logger, context, level, message, attributes); }
}

/** Decode valueRanges as Go's json.Unmarshal into [][]any does; undefined for an invalid shape. */
function decodeValueRanges(body: Uint8Array): string[][][] | undefined {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')); } catch { return undefined; }
  if (!document || typeof document !== 'object' || Array.isArray(document)) return undefined;
  const listed = (document as Record<string, unknown>).valueRanges;
  if (listed == null) return [];
  if (!Array.isArray(listed)) return undefined;
  const decoded: string[][][] = [];
  for (const valueRange of listed) {
    if (valueRange === null) { decoded.push([]); continue; }
    if (typeof valueRange !== 'object' || Array.isArray(valueRange)) return undefined;
    const fields = valueRange as Record<string, unknown>;
    if ((fields.range != null && typeof fields.range !== 'string') || (fields.majorDimension != null && typeof fields.majorDimension !== 'string')) return undefined;
    if (fields.values == null) { decoded.push([]); continue; }
    if (!Array.isArray(fields.values) || fields.values.some(row => row !== null && !Array.isArray(row))) return undefined;
    decoded.push((fields.values as Array<unknown[] | null>).map(row => (row ?? []).map(goFormatValue)));
  }
  return decoded;
}

function retryAfterMilliseconds(exchange: SpreadsheetExchange): number {
  const value = exchange.headers['retry-after'];
  const header = (Array.isArray(value) ? value[0] : value) ?? '';
  if (!/^[+]?[0-9]+$/.test(header) || Number(header) > maxRetryAfterSeconds) return 0;
  return Number(header) * 1000;
}

/** Report whether the window's first row still holds the cursor row's cells; row 1 never moves. */
function windowStartsAtCursorRow(window: string[][], token: RowAddedCursorToken): boolean {
  return token.lastRow === 1 || hashRowCells(window[0] ?? []) === token.rowHash;
}
