// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it } from 'vitest';
import { SecretString, newTriggerContext, type TriggerBindingRef, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { SpreadsheetClient } from '../src/client.js';
import { RowAddedTriggerDefinition, rowAddedPollSchedule, validateRowAddedTriggerConfiguration, type RowAddedEvent } from '../src/index.js';
import { decodeRowAddedCursorToken, encodeRowAddedCursorToken, hashRowCells, rowAddedEventId } from '../src/row_added_cursor.js';
import { RowAddedFetcher } from '../src/row_added_fetcher.js';
import { goTrimSpace } from '../src/row_added_models.js';
import { SheetsFake } from './sheets_fake.js';

const leadsHeader = ['email', 'name', 'status'];
const now = new Date('2026-11-15T09:00:00Z');
const context = newTriggerContext();
const credential = { accessToken: new SecretString('SENTINEL-ACCESS-TOKEN'), oauthClientId: 'client', oauthClientSecret: new SecretString(''), refreshToken: new SecretString('') };
const credentials = { resolve: async () => credential, resolveWithRefresh: async () => credential };
const binding: TriggerBindingRef = { connection: { provider: 'google', name: 'sheets-leads' }, trigger: RowAddedTriggerDefinition.trigger, name: 'new-leads' };
const sheets: SheetsFake[] = [];
type Page = ChangePage<TriggerEvent<RowAddedEvent>>;

async function newSheet(tab: string, ...rows: string[][]): Promise<SheetsFake> {
  const sheet = await SheetsFake.start(tab, ...rows);
  sheets.push(sheet);
  return sheet;
}

function newFetcher(sheet: SheetsFake, config: { maxRows?: number; maxResponseBytes?: number; pollPageSize?: number } = {}, sheetName = 'Leads 2026'): RowAddedFetcher {
  const client = new SpreadsheetClient({ endpoint: sheet.url, ...config }, credentials, { now: () => now });
  return new RowAddedFetcher(client, binding, { spreadsheetId: ' sheet-1 ', sheetName });
}

function expectCursorRow(cursor: Cursor, lastRow: number, cells: string[] | undefined): void {
  expect(decodeRowAddedCursorToken(cursor.token)).toEqual({ lastRow, rowHash: hashRowCells(cells) });
}

const rowNumbers = (page: Page): number[] => page.changes.map(change => change.payload.rowNumber);
const scannedRanges = (sheet: SheetsFake): string[] => sheet.requests().flatMap(request => request.ranges);

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try { await promise; } catch (error) { return error; }
  throw new Error('expected a rejection');
}

afterEach(async () => { await Promise.all(sheets.splice(0).map(sheet => sheet.close())); });

describe('rowAdded fetcher', () => {
  it('matches Go cursor tokens, row hashes, and event IDs byte for byte', () => {
    const cases = JSON.parse(readFileSync(new URL('./fixtures/go-row-added-cases.json', import.meta.url), 'utf8')) as Array<{ cells: string[]; rowHash: string; token: string; eventId: string }>;
    for (const fixture of cases) {
      expect(hashRowCells(fixture.cells)).toBe(fixture.rowHash);
      expect(encodeRowAddedCursorToken({ lastRow: 7, rowHash: fixture.rowHash })).toBe(fixture.token);
      expect(rowAddedEventId('sheet-1', "Leads 2026 'q'", 12, fixture.cells)).toBe(fixture.eventId);
      expect(decodeRowAddedCursorToken(fixture.token)).toEqual({ lastRow: 7, rowHash: fixture.rowHash });
    }
    const rowHash = cases[0]!.rowHash.toUpperCase();
    expect(decodeRowAddedCursorToken(` {"V":1,"lastRow":3,"LASTROW":2,"rowHash":null,"RowHash":"${rowHash}"}`)).toEqual({ lastRow: 2, rowHash });
  });

  it('seeds after the last row and delivers appended rows in order', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader, ['ada@example.test', 'Ada', 'new']);
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    expectCursorRow(seed, 2, ['ada@example.test', 'Ada', 'new']);
    let page = await fetcher.fetchChanges(context, seed);
    expect(page.changes, 'existing rows are never delivered').toEqual([]);
    expect(page.next).toEqual(seed);

    sheet.appendRows(['grace@example.test', 'Grace'], [], ['', '', '', ''], ['linus@example.test', 'Linus', 'new', 'extra']);
    page = await fetcher.fetchChanges(context, seed);
    expect(rowNumbers(page), 'blank rows are skipped').toEqual([3, 6]);
    expect(page.hasMorePages).toBe(false);
    expectCursorRow(page.next, 6, ['linus@example.test', 'Linus', 'new', 'extra']);
    const first = page.changes[0]!;
    expect(first.payload).toEqual({ spreadsheetId: 'sheet-1', sheetName: 'Leads 2026', rowNumber: 3, values: { email: 'grace@example.test', name: 'Grace', status: '' }, cells: ['grace@example.test', 'Grace'] });
    expect(first.id).toMatch(/^row-3-[0-9a-f]{16}$/);
    expect(first.occurredAt).toEqual(now);
    const refetched = await fetcher.fetchChanges(context, seed);
    expect(refetched.changes.map(change => change.id), 'a refetch after a crash yields the same event IDs').toEqual(page.changes.map(change => change.id));
    expect(sheet.requests().at(-1)).toEqual({ spreadsheetId: 'sheet-1', ranges: ["'Leads 2026'!1:1", "'Leads 2026'!2:102"], authorization: 'Bearer SENTINEL-ACCESS-TOKEN' });
  });

  it('seeds an empty tab at the header row', async () => {
    const sheet = await newSheet('Leads 2026');
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    expectCursorRow(seed, 1, undefined);
    sheet.appendRows(leadsHeader, ['ada@example.test']);
    const page = await fetcher.fetchChanges(context, seed);
    expect(rowNumbers(page), 'a header added after seeding is not delivered as a row').toEqual([2]);
    expect(page.changes[0]!.payload.values).toEqual({ email: 'ada@example.test', name: '', status: '' });
  });

  it('scans the tab in maxRows windows', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader, ...['a', 'b', 'c', 'd', 'e'].map(letter => ['row', letter]));
    expectCursorRow(await newFetcher(sheet, { maxRows: 2 }).seedCursor(context), 6, ['row', 'e']);
    expect(scannedRanges(sheet)).toEqual(["'Leads 2026'!1:2", "'Leads 2026'!3:4", "'Leads 2026'!5:6", "'Leads 2026'!7:8"]);
  });

  it('pages large batches within pollPageSize', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader);
    const fetcher = newFetcher(sheet, { pollPageSize: 2 });
    const seed = await fetcher.seedCursor(context);
    sheet.appendRows(['a'], ['b'], ['c']);
    let page = await fetcher.fetchChanges(context, seed);
    expect([rowNumbers(page), page.hasMorePages]).toEqual([[2, 3], true]);
    page = await fetcher.fetchChanges(context, page.next);
    expect([rowNumbers(page), page.hasMorePages]).toEqual([[4], false]);
  });

  it('realigns after rows above the cursor are deleted or inserted', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader, ['a'], ['b'], ['c']);
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    sheet.deleteRow(2);
    sheet.appendRows(['d']);
    let page = await fetcher.fetchChanges(context, seed);
    expect(rowNumbers(page), 'row c moved from 4 to 3, so only d is new').toEqual([4]);
    expectCursorRow(page.next, 4, ['d']);
    sheet.truncateRows(1);
    sheet.appendRows(['new-1'], ['b'], ['c'], ['d'], ['e']);
    page = await fetcher.fetchChanges(context, page.next);
    expect(rowNumbers(page), 'row d moved down from 4 to 5 after an insert above it').toEqual([6]);
  });

  it('keeps the cursor row when it is edited in place', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader, ['a', 'new']);
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    sheet.replaceRow(2, ['a', 'contacted']);
    sheet.appendRows(['b', 'new']);
    const page = await fetcher.fetchChanges(context, seed);
    expect(rowNumbers(page)).toEqual([3]);
    expectCursorRow(page.next, 3, ['b', 'new']);
  });

  it('reports an invalid cursor when the tab ends above it or for a token Go refuses', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader, ['a'], ['b'], ['c']);
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    sheet.truncateRows(2);
    expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, seed)))).toBe(true);
    const empty = hashRowCells(undefined);
    for (const token of ['', '{}', '{"v":1,"lastRow":0,"rowHash":""}', `{"v":2,"lastRow":2,"rowHash":"${empty}"}`, '{"v":1,"lastRow":2,"rowHash":"zz","extra":1}', `{"v":1,"lastRow":2.0,"rowHash":"${empty}"}`, `{"v":1,"lastRow":2,"rowHash":"${'z'.repeat(64)}"}`, '[1]']) {
      expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, { token }))), token).toBe(true);
    }
  });

  it('classifies provider failures without leaking secrets', async () => {
    const sheet = await newSheet('Leads 2026', leadsHeader);
    const fetcher = newFetcher(sheet);
    const seed = await fetcher.seedCursor(context);
    sheet.script({ status: 429, retryAfter: '7' });
    let error = await rejection(fetcher.fetchChanges(context, seed));
    expect(error).toBeInstanceOf(RateLimitedError);
    expect((error as RateLimitedError).retryAfterMs).toBe(7000);
    sheet.script({ status: 429, retryAfter: 'soon' });
    error = await rejection(fetcher.fetchChanges(context, seed));
    expect((error as RateLimitedError).retryAfterMs, 'an unreadable Retry-After falls back to backoff').toBe(0);
    for (const status of [404, 403, 500]) {
      sheet.script({ status });
      error = await rejection(fetcher.fetchChanges(context, seed));
      expect(isCursorInvalid(error), `HTTP ${status} keeps the cursor`).toBe(false);
      expect(error).not.toBeInstanceOf(RateLimitedError);
      expect(String(error)).not.toMatch(/SENTINEL|Leads/);
    }
  });

  it('validates configuration and poll settings', () => {
    expect(() => validateRowAddedTriggerConfiguration({ spreadsheetId: 'sheet-1', sheetName: '' })).toThrow();
    expect(() => validateRowAddedTriggerConfiguration({ spreadsheetId: ' ', sheetName: 'Leads' })).toThrow();
    validateRowAddedTriggerConfiguration({ spreadsheetId: 'sheet-1', sheetName: 'Leads' });
    for (const config of [{ pollIntervalMs: 500 }, { pollPageSize: 501 }, { pollPageSize: -1 }]) expect(() => new SpreadsheetClient(config, credentials)).toThrow();
    const client = new SpreadsheetClient({}, credentials);
    expect([client.pollIntervalMs, client.pollPageSize]).toEqual([30_000, 100]);
    expect(rowAddedPollSchedule(client)).toEqual({ intervalMs: 30_000, maxBackoffMs: 300_000 });
  });

  it('trims the spreadsheet ID as Go strings.TrimSpace does', async () => {
    expect(goTrimSpace('\u0085\u00a0 sheet-1\u3000\t')).toBe('sheet-1');
    expect(goTrimSpace('\ufeffsheet-1\ufeff')).toBe('\ufeffsheet-1\ufeff');
    expect(() => validateRowAddedTriggerConfiguration({ spreadsheetId: '\u0085', sheetName: 'Leads' })).toThrow();
    const sheet = await newSheet('Leads', leadsHeader, ['a']);
    const client = new SpreadsheetClient({ endpoint: sheet.url }, credentials, { now: () => now });
    const fetcher = new RowAddedFetcher(client, binding, { spreadsheetId: '\u0085sheet-1\u0085', sheetName: 'Leads' });
    const seed = await fetcher.seedCursor(context);
    sheet.appendRows(['b']);
    const page = await fetcher.fetchChanges(context, seed);
    expect(page.changes[0]!.payload.spreadsheetId).toBe('sheet-1');
    expect(page.changes[0]!.id).toBe(rowAddedEventId('sheet-1', 'Leads', 3, ['b']));
    expect(sheet.requests().at(-1)!.spreadsheetId).toBe('sheet-1');
  });

  it('uses the exact tab title including outer spaces', async () => {
    const sheet = await newSheet(' Leads 2026 ', leadsHeader, ['a']);
    const fetcher = newFetcher(sheet, {}, ' Leads 2026 ');
    const seed = await fetcher.seedCursor(context);
    sheet.appendRows(['b']);
    const page = await fetcher.fetchChanges(context, seed);
    expect(rowNumbers(page)).toEqual([3]);
    expect(page.changes[0]!.payload.sheetName).toBe(' Leads 2026 ');
    expect(sheet.requests().at(-1)!.ranges).toEqual(["' Leads 2026 '!1:1", "' Leads 2026 '!2:102"]);
  });

  it('halves scan windows whose response is too large', async () => {
    const rows = [leadsHeader, ...Array.from({ length: 300 }, (_, index) => [`row-${String(index).padStart(3, '0')}-padding-padding`])];
    const sheet = await newSheet('Leads 2026', ...rows);
    // A 400-row window returns all 301 rows, about 10 KB; a 200-row window stays under 7 KB.
    expectCursorRow(await newFetcher(sheet, { maxRows: 400, maxResponseBytes: 7000 }).seedCursor(context), 301, rows[300]);
    expect(scannedRanges(sheet)).toEqual(["'Leads 2026'!1:400", "'Leads 2026'!1:200", "'Leads 2026'!201:400"]);
    const error = await rejection(newFetcher(sheet, { maxRows: 400, maxResponseBytes: 1000 }).seedCursor(context));
    expect(String(error)).toContain('provider response exceeds configured limit');
  });
});
