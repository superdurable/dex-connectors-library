// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Poll one Notion data source's rows by last edited time for the poll Trigger kit, as the Go fetcher does.

import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent, TriggerRef } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { NotionClient } from './client.js';
import {
  DatabaseRowCursorDecodeError, SeenRows, databaseRowCursorVersion, databaseRowLookbackNanoseconds, decodeDatabaseRowCursor,
  encodeDatabaseRowCursor, goZeroTime, type DatabaseRowCursor,
} from './database_row_cursor.js';
import type { DatabaseRowChangedEvent } from './database_row_models.js';
import type { Page } from './generated/models.js';
import { GoTime, parseGoTime, unixEpoch } from './go_time.js';
import { decodePage, validateCursor } from './operations.js';

const operationId = 'databaseRowChanged';
const lastEditedTime = 'last_edited_time';
// Bounds the baseline read of the lookback window.
const maximumSeedPages = 20;
// An empty data source's first query starts at the Unix epoch, not the host clock.
const emptyDataSourceHighWater = unixEpoch.add(databaseRowLookbackNanoseconds);

/** A failed data source query; the kit backs off and retries without moving the cursor. */
export class DatabaseRowPollError extends Error {
  constructor(message: string, options?: ErrorOptions) { super(message, options); this.name = 'DatabaseRowPollError'; }
}

class StaleStartCursorError extends DatabaseRowPollError {}

interface QueriedRow { page: Page; lastEditedTime: GoTime }
interface QueriedRows { rows: QueriedRow[]; nextCursor: string }

/** The ChangeFetcher of one databaseRowChanged binding: one data source's rows by last edited time. */
export class DatabaseRowFetcher implements ChangeFetcher<TriggerEvent<DatabaseRowChangedEvent>> {
  /** Bind the client and the dashed watched data source ID; it sends nothing. */
  constructor(private readonly client: NotionClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, private readonly dataSourceId: string) {}

  /** Read the newest last edited time, then remember every row edited inside the lookback before it. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const newest = await this.query(context, null, '', 1, true);
    const position: DatabaseRowCursor = { version: databaseRowCursorVersion, dataSourceId: this.dataSourceId, highWater: emptyDataSourceHighWater, queryFrom: null, startCursor: '', seen: [] };
    if (newest.rows[0]) position.highWater = newest.rows[0].lastEditedTime;
    const seen = new SeenRows([]);
    const queryFrom = position.highWater.add(-databaseRowLookbackNanoseconds);
    let startCursor = '';
    for (let page = 0; page < maximumSeedPages; page++) {
      const queried = await this.query(context, queryFrom, startCursor, this.client.pollPageSize);
      for (const row of queried.rows) {
        seen.add(row.lastEditedTime, row.page.id);
        position.highWater = laterTime(position.highWater, row.lastEditedTime);
      }
      if (!queried.nextCursor) break;
      startCursor = queried.nextCursor;
    }
    position.seen = seen.groupsWithin(position.highWater);
    return encode(position);
  }

  /**
   * Query rows edited at or after the lookback before the high water, oldest first, and return each row version
   * not yet seen. An undecodable cursor, or one for another data source, is marked invalid so the kit records a
   * new baseline; a start cursor Notion rejects restarts the query instead.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<DatabaseRowChangedEvent>>> {
    let position: DatabaseRowCursor | undefined;
    try { position = decodeDatabaseRowCursor(cursor.token); }
    catch (error) { if (!(error instanceof DatabaseRowCursorDecodeError)) throw error; }
    if (!position || position.version !== databaseRowCursorVersion || position.highWater.unixNanoseconds === goZeroTime.unixNanoseconds) {
      throw markCursorInvalid(new DatabaseRowPollError('Notion databaseRowChanged cursor is not decodable'));
    }
    if (position.dataSourceId !== this.dataSourceId) throw markCursorInvalid(new DatabaseRowPollError('Notion databaseRowChanged cursor belongs to another data source'));
    let queryFrom = position.highWater.add(-databaseRowLookbackNanoseconds);
    if (position.startCursor && position.queryFrom) queryFrom = position.queryFrom;
    let queried: QueriedRows;
    try { queried = await this.query(context, queryFrom, position.startCursor, this.client.pollPageSize); }
    catch (error) {
      if (!(error instanceof StaleStartCursorError)) throw error;
      position.startCursor = ''; position.queryFrom = null;
      return { changes: [], next: encode(position), hasMorePages: true };
    }
    const seen = new SeenRows(position.seen);
    const changes: TriggerEvent<DatabaseRowChangedEvent>[] = [];
    for (const row of queried.rows) {
      if (row.lastEditedTime.isBefore(queryFrom) || seen.contains(row.lastEditedTime, row.page.id)) continue;
      seen.add(row.lastEditedTime, row.page.id);
      position.highWater = laterTime(position.highWater, row.lastEditedTime);
      changes.push(newDatabaseRowChangedTriggerEvent(this.dataSourceId, row.page, row.lastEditedTime));
    }
    position.startCursor = ''; position.queryFrom = null;
    const hasMorePages = queried.nextCursor !== '';
    if (hasMorePages) { position.startCursor = queried.nextCursor; position.queryFrom = queryFrom; }
    position.seen = seen.groupsWithin(position.highWater);
    return { changes, next: encode(position), hasMorePages };
  }

  private async query(context: TriggerContext, editedOnOrAfter: GoTime | null, startCursor: string, pageSize: number, isNewestFirst = false): Promise<QueriedRows> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const resolved = await this.client.resolveToken(call, operationId, context.signal);
    if ('outcome' in resolved) throw new DatabaseRowPollError("Notion databaseRowChanged credentials are unavailable; save the connection's API token again");
    const body = {
      ...(editedOnOrAfter ? { filter: { [lastEditedTime]: { on_or_after: editedOnOrAfter.inUTC().formatRFC3339Nano() }, timestamp: lastEditedTime } } : {}),
      sorts: [{ direction: isNewestFirst ? 'descending' : 'ascending', timestamp: lastEditedTime }],
      ...(startCursor ? { start_cursor: startCursor } : {}),
      page_size: pageSize, result_type: 'page',
    };
    const result = await this.client.exchange(call, operationId, 'databaseRowChanged data source query', 'POST', `/data_sources/${encodeURIComponent(this.dataSourceId)}/query`, body, 'read', context.signal, resolved.token, this.dataSourceId);
    if ('response' in result) return decodeRowsPage(result.response.body, pageSize);
    if (startCursor && result.disposition === 'providerRejected' && result.failure.kind === 'VALIDATION' && result.providerErrorCode === 'validation_error') {
      throw new StaleStartCursorError('Notion rejected the stored query start cursor');
    }
    if (result.disposition === 'retry' && result.failure.kind === 'RATE_LIMIT') throw markRateLimited(new DatabaseRowPollError(result.failure.message), result.retryAfterMs ?? 0);
    if (result.disposition === 'notFound') throw new DatabaseRowPollError('Notion could not find the watched data source; share its database with the connection or pick another data source');
    throw new DatabaseRowPollError(result.failure.message);
  }
}

/** Convert one queried row; its ID is a pure function of the row version, so a refetch yields it again. */
export function newDatabaseRowChangedTriggerEvent(dataSourceId: string, row: Page, rowLastEditedTime: GoTime): TriggerEvent<DatabaseRowChangedEvent> {
  return { id: databaseRowChangeId(dataSourceId, row.id, rowLastEditedTime), occurredAt: rowLastEditedTime.toDate(), payload: { dataSourceId, changeKind: databaseRowChangeKind(row), row } };
}

/** Hash the data source, row, and last edited time into Go's short stable Trigger event ID. */
export function databaseRowChangeId(dataSourceId: string, rowId: string, rowLastEditedTime: GoTime): string {
  const identity = [dataSourceId, rowId, rowLastEditedTime.inUTC().formatRFC3339Nano()].join('\u0000');
  return `notion-row-${createHash('sha256').update(identity).digest().subarray(0, 16).toString('hex')}`;
}

/** Return created when the row's created and last edited times are the same instant, else updated. */
export function databaseRowChangeKind(row: Page): 'created' | 'updated' {
  try { return parseGoTime(row.createdTime ?? '').unixNanoseconds === parseGoTime(row.lastEditedTime ?? '').unixNanoseconds ? 'created' : 'updated'; }
  catch { return 'updated'; }
}

function decodeRowsPage(body: Uint8Array, pageSize: number): QueriedRows {
  const invalidPage = (cause?: unknown) => new DatabaseRowPollError('Notion returned an invalid databaseRowChanged query page', { cause });
  let page: unknown;
  try { page = JSON.parse(Buffer.from(body).toString('utf8')); } catch (error) { throw invalidPage(error); }
  if (page === null) return { rows: [], nextCursor: '' };
  if (typeof page !== 'object' || Array.isArray(page)) throw invalidPage();
  const { results, next_cursor: nextCursor, has_more: hasMore, request_status: requestStatus } = page as Record<string, unknown>;
  if ((results != null && !Array.isArray(results)) || (nextCursor != null && typeof nextCursor !== 'string') || (hasMore != null && typeof hasMore !== 'boolean')
    || (requestStatus != null && (typeof requestStatus !== 'object' || Array.isArray(requestStatus))) || (results?.length ?? 0) > pageSize) throw invalidPage();
  let validatedCursor: string;
  try { validatedCursor = validateCursor((nextCursor as string | null | undefined) ?? ''); }
  catch (error) { throw new DatabaseRowPollError('Notion returned an invalid databaseRowChanged query cursor', { cause: error }); }
  const rows = ((results as unknown[] | null | undefined) ?? []).map((resource, index): QueriedRow => {
    let row: Page;
    try { row = decodePage(resource); }
    catch (error) { throw new DatabaseRowPollError(`Notion returned an invalid row ${index} in the databaseRowChanged query page`, { cause: error }); }
    try { return { page: row, lastEditedTime: parseGoTime(row.lastEditedTime ?? '').inUTC() }; }
    catch (error) { throw new DatabaseRowPollError(`Notion returned row ${index} without a valid last_edited_time`, { cause: error }); }
  });
  return { rows, nextCursor: hasMore === true ? validatedCursor : '' };
}

function laterTime(current: GoTime, candidate: GoTime): GoTime { return candidate.isAfter(current) ? candidate : current; }

function encode(position: DatabaseRowCursor): Cursor {
  try { return { token: encodeDatabaseRowCursor(position) }; }
  catch (error) { throw new DatabaseRowPollError('encode Notion databaseRowChanged cursor', { cause: error }); }
}
