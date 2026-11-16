// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';

/** One failure answered instead of the next batchGet request. */
export interface ScriptedResponse { status: number; retryAfter?: string }
/** One recorded batchGet request. */
export interface RecordedRequest { spreadsheetId: string; ranges: string[]; authorization: string }

function trimTrailingEmptyCells(cells: readonly string[]): string[] {
  let end = cells.length;
  while (end > 0 && cells[end - 1] === '') end--;
  return cells.slice(0, end);
}

/** Go-compatible compact JSON with sorted keys, as encoding/json writes a map. */
function encodeSorted(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(encodeSorted).join(',')}]`;
  if (value && typeof value === 'object') return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${encodeSorted((value as Record<string, unknown>)[key])}`).join(',')}}`;
  return JSON.stringify(value);
}

/**
 * Serves one mutable tab through values:batchGet, and values.get for example read-backs, on a loopback HTTP
 * server, as the Go connector's sheetsfake does: rows[0] is the header, and trailing empty rows and cells are omitted. Close it with close().
 */
export class SheetsFake {
  readonly #server: Server;
  #rows: string[][];
  #scripted: ScriptedResponse[] = [];
  #requests: RecordedRequest[] = [];
  #completed = 0;
  #reads: string[] = [];
  readonly #deferred = new Map<number, string[][]>();
  #url = '';

  private constructor(readonly tab: string, rows: string[][]) {
    this.#rows = rows.map(row => [...row]);
    this.#server = createServer((request, response) => this.#handle(request, response));
  }

  /** Start serving tab with rows on 127.0.0.1. */
  static async start(tab: string, ...rows: string[][]): Promise<SheetsFake> {
    const sheet = new SheetsFake(tab, rows);
    sheet.#server.listen(0, '127.0.0.1');
    await once(sheet.#server, 'listening');
    const address = sheet.#server.address();
    if (!address || typeof address === 'string') throw new TypeError('Sheets fake port unavailable');
    sheet.#url = `http://127.0.0.1:${address.port}/v4`;
    return sheet;
  }

  /** The Sheets API base URL to configure as the connection endpoint. */ get url(): string { return this.#url; }
  /** Add rows at the bottom of the tab. */ appendRows(...rows: string[][]): void { this.#rows.push(...rows.map(row => [...row])); }
  /** Append rows right after the count-th answered request, such as after a seed. */
  appendRowsAfterRequests(count: number, ...rows: string[][]): void { this.#deferred.set(count, [...(this.#deferred.get(count) ?? []), ...rows]); }
  /** Remove the 1-based row, shifting later rows up. */ deleteRow(rowNumber: number): void { this.#rows.splice(rowNumber - 1, 1); }
  /** Replace the cells of the 1-based row. */ replaceRow(rowNumber: number, cells: string[]): void { this.#rows[rowNumber - 1] = [...cells]; }
  /** Keep only the first count rows. */ truncateRows(count: number): void { this.#rows = this.#rows.slice(0, count); }
  /** Answer the next requests with responses, in order. */ script(...responses: ScriptedResponse[]): void { this.#scripted.push(...responses); }
  /** Every recorded request. */ requests(): RecordedRequest[] { return [...this.#requests]; }
  /** The range of every values.get request in order; those reads are not counted, recorded by requests(), or scripted. */
  readRanges(): string[] { return [...this.#reads]; }
  /** How many batchGet requests were answered. */ completedRequests(): number { return this.#completed; }
  /** Stop serving. */ async close(): Promise<void> { this.#server.closeAllConnections(); await new Promise<void>(resolve => this.#server.close(() => resolve())); }

  #handle(request: IncomingMessage, response: ServerResponse): void {
    if (this.#answerValuesGet(request, response)) return;
    try { this.#answer(request, response); } finally {
      this.#completed++;
      this.#rows.push(...(this.#deferred.get(this.#completed) ?? []));
      this.#deferred.delete(this.#completed);
    }
  }

  #answer(request: IncomingMessage, response: ServerResponse): void {
    const url = new URL(request.url ?? '/', 'http://fake');
    const match = /^\/v4\/spreadsheets\/([^/]+)\/values:batchGet$/.exec(url.pathname);
    if (request.method !== 'GET' || !match) { response.writeHead(404).end(); return; }
    const spreadsheetId = decodeURIComponent(match[1]!);
    const ranges = url.searchParams.getAll('ranges');
    this.#requests.push({ spreadsheetId, ranges, authorization: request.headers.authorization ?? '' });
    const scripted = this.#scripted.shift();
    if (scripted) {
      response.writeHead(scripted.status, { 'Content-Type': 'application/json', ...(scripted.retryAfter ? { 'Retry-After': scripted.retryAfter } : {}) });
      response.end(`{"error":{"code":${scripted.status}}}`);
      return;
    }
    const valueRanges: Record<string, unknown>[] = [];
    for (const valueRange of ranges) {
      const bounds = this.#rowBounds(valueRange);
      if (!bounds) { response.writeHead(400).end('{"error":{"code":400}}'); return; }
      const rows = this.#rowsIn(bounds[0], bounds[1]);
      valueRanges.push({ range: valueRange, majorDimension: 'ROWS', ...(rows.length ? { values: rows } : {}) });
    }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(`${encodeSorted({ spreadsheetId, valueRanges })}\n`);
  }

  /** Answer GET /v4/spreadsheets/{id}/values/{range}, the example's read-back, and report whether it did. */
  #answerValuesGet(request: IncomingMessage, response: ServerResponse): boolean {
    const url = new URL(request.url ?? '/', 'http://fake');
    const match = /^\/v4\/spreadsheets\/[^/]+\/values\/([^/]+)$/.exec(url.pathname);
    if (request.method !== 'GET' || !match) return false;
    const valueRange = decodeURIComponent(match[1]!);
    this.#reads.push(valueRange);
    const bounds = this.#rowBounds(valueRange);
    if (!bounds) { response.writeHead(400).end('{"error":{"code":400}}'); return true; }
    const rows = this.#rowsIn(bounds[0], bounds[1]);
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(encodeSorted({ range: valueRange, majorDimension: 'ROWS', ...(rows.length ? { values: rows } : {}) }));
    return true;
  }

  #rowBounds(valueRange: string): [number, number] | undefined {
    const quotedTab = `'${this.tab.replaceAll("'", "''")}'!`;
    if (!valueRange.startsWith(quotedTab)) return undefined;
    const bounds = /^([0-9]+):([0-9]+)$/.exec(valueRange.slice(quotedTab.length));
    if (!bounds) return undefined;
    const first = Number(bounds[1]), last = Number(bounds[2]);
    return first >= 1 && last >= first ? [first, last] : undefined;
  }

  #rowsIn(firstRow: number, lastRow: number): string[][] {
    let end = Math.min(lastRow, this.#rows.length);
    while (end >= firstRow && trimTrailingEmptyCells(this.#rows[end - 1]!).length === 0) end--;
    const rows: string[][] = [];
    for (let rowNumber = firstRow; rowNumber <= end; rowNumber++) rows.push(trimTrailingEmptyCells(this.#rows[rowNumber - 1]!));
    return rows;
  }
}
