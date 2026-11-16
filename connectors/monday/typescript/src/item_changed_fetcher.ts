// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerRef } from '@superdurable/connectors-sdk';
import {
  DefaultKeyedSnapshotEntries, applyKeyedSnapshotChanges, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshot } from '@superdurable/connectors-sdk/projectconfig';
import type { MondayClient, MondayExchange } from './client.js';
import {
  GoZeroTimeNanoseconds, ItemChangeCreated, ItemChangeUpdated, PolledItem, decodePolledItem, formatGoTime, goJSONString, newItemChangedTriggerEvent, parseCursorTime,
  type ItemChangeKind, type ItemChangedEvent, type ItemChangedTriggerConfiguration,
} from './item_snapshot.js';
import { listDocument, nextDocument } from './operations.js';

const itemChangedOperation = 'itemChanged';
/** The items_page limit of every poll request, the connector's listItems bound. */
export const ItemPollPageSize = 100;
/** Bounds one poll's newest-first scan at 10,000 items. */
export const MaximumItemScanPages = 100;
/** Rescans items updated shortly before the cursor, so a late or same-second update still arrives. */
export const ItemChangeLookbackMs = 5 * 60_000;
/** With MaximumItemChangeBatchBytes, keeps one recorded batch well below the durable inbox's 1000 events and 8 MiB. */
export const MaximumItemChangesPerPage = 100;
/** Bounds the item JSON of one batch. */
export const MaximumItemChangeBatchBytes = 4 << 20;
/** Tracks the items updated inside the lookback window, keeping each one's last-updated time. */
export const ItemChangedSnapshotOptions: KeyedSnapshotOptions = { maxEntries: DefaultKeyedSnapshotEntries, keepValues: true, evictLeastRecentlySeen: true };
const lookbackNanoseconds = BigInt(ItemChangeLookbackMs) * 1_000_000n;
const maximumCursorBytes = 4096;
const lastUpdatedOrder = { order_by: [{ column_id: '__last_updated__', direction: 'desc' }] };

/** More items changed since the last poll than one scan reads. */
export class ItemScanExceededError extends Error {
  /** Name the scan bound. */
  constructor() { super(`more than ${MaximumItemScanPages * ItemPollPageSize} items changed since the last poll`); }
}

/** One polled item whose content differs from the snapshot, with its derived kind. */
export interface ItemChange { /** The polled item. */ polled: PolledItem; /** The derived kind. */ kind: ItemChangeKind }

interface ItemScan { items: PolledItem[]; newest: bigint | undefined; isExceeded: boolean }

/**
 * Reads one board's items newest first by monday.com's last-updated time. The committed cursor token is
 * {"boardId":...,"watermark":...}, the latest delivered last-updated time, encoded byte for byte as the Go
 * connector does; a zero watermark means the board was empty at the baseline, so every item that appears is delivered.
 */
export class ItemChangeFetcher implements ChangeFetcher<TriggerEvent<ItemChangedEvent>> {
  private readonly boardId: string;
  // The changes a scan found beyond the delivered batch, for the immediate next fetch.
  private pending: { cursorToken: string; changes: ItemChange[] } | undefined;

  /** Bind the client, connection, Trigger identity, and validated configuration. */
  constructor(private readonly client: MondayClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, configuration: ItemChangedTriggerConfiguration) {
    this.boardId = configuration.boardId;
  }

  /** Record the newest last-updated time and snapshot the items inside the lookback window before it. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    this.pending = undefined;
    // An oversized baseline window keeps the items read so far; the rest may arrive once as changes.
    const scan = await this.scanItemsUpdatedSince(context, undefined, true);
    return { token: this.encodeCursor(scan.newest), snapshot: applyKeyedSnapshotChanges(undefined, scan.items, [], ItemChangedSnapshotOptions) };
  }

  /**
   * Deliver, oldest first, the items whose content changed since cursor. An undecodable cursor, a cursor for
   * another board, or more changes than one scan can read throw markCursorInvalid.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<ItemChangedEvent>>> {
    const position = decodeCursor(cursor.token);
    if (!position) throw markCursorInvalid(new Error('monday.com itemChanged cursor is not decodable'));
    if (position.boardId !== this.boardId) throw markCursorInvalid(new Error('monday.com itemChanged cursor belongs to another board'));
    const changes = await this.changesAfter(context, cursor, position.watermark);
    const batchSize = itemChangeBatchSize(changes);
    const batch = changes.slice(0, batchSize), rest = changes.slice(batchSize);
    let watermark = position.watermark;
    for (const change of batch) {
      const updatedAt = change.polled.updatedAt;
      if (updatedAt !== undefined && (watermark === undefined || updatedAt > watermark)) watermark = updatedAt;
    }
    const events = batch.map(change => newItemChangedTriggerEvent(this.boardId, change.polled, change.kind));
    const next: Cursor = { token: this.encodeCursor(watermark), snapshot: nextItemSnapshot(cursor.snapshot, batch, floorOf(watermark)) };
    this.pending = rest.length ? { cursorToken: next.token, changes: rest } : undefined;
    return { changes: events, next, hasMorePages: rest.length > 0 };
  }

  private async changesAfter(context: TriggerContext, cursor: Cursor, watermark: bigint | undefined): Promise<ItemChange[]> {
    if (this.pending && this.pending.cursorToken === cursor.token) return this.pending.changes;
    this.pending = undefined;
    const floor = floorOf(watermark);
    const scan = await this.scanItemsUpdatedSince(context, floor, false);
    if (scan.isExceeded) throw markCursorInvalid(new ItemScanExceededError());
    const previousHashes = new Map((cursor.snapshot?.entries ?? []).map(entry => [entry.key, entry.hash]));
    const changes: ItemChange[] = [];
    for (const polled of scan.items) {
      const previousHash = previousHashes.get(polled.item.id);
      if (previousHash === polled.snapshotHash()) continue;
      const isCreated = previousHash === undefined && polled.createdAt !== undefined && polled.createdAt >= floor;
      changes.push({ polled, kind: isCreated ? ItemChangeCreated : ItemChangeUpdated });
    }
    // Array.prototype.sort is stable, as Go's slices.SortStableFunc.
    return changes.sort((left, right) => {
      const leftTime = left.polled.updatedAt ?? 0n, rightTime = right.polled.updatedAt ?? 0n;
      if (leftTime !== rightTime) return leftTime < rightTime ? -1 : 1;
      return Buffer.compare(Buffer.from(left.polled.item.id), Buffer.from(right.polled.item.id));
    });
  }

  /** Read pages newest first until an item older than floor or the last page; isFloorFromNewest derives floor from the newest item. */
  private async scanItemsUpdatedSince(context: TriggerContext, floor: bigint | undefined, isFloorFromNewest: boolean): Promise<ItemScan> {
    let document = listDocument;
    let variables: Record<string, unknown> = { boardIds: [this.boardId], limit: ItemPollPageSize, columnIds: null, queryParams: lastUpdatedOrder };
    const items: PolledItem[] = [], seenItemIDs = new Set<string>();
    let newest: bigint | undefined;
    for (let pageIndex = 0; pageIndex < MaximumItemScanPages; pageIndex++) {
      const page = await this.readItemsPage(context, document, variables, pageIndex > 0);
      let hasReachedFloor = false;
      for (const polled of page.items) {
        const updatedAt = polled.updatedAt;
        if (updatedAt === undefined || seenItemIDs.has(polled.item.id)) continue;
        seenItemIDs.add(polled.item.id);
        if (newest === undefined && isFloorFromNewest) floor = updatedAt - lookbackNanoseconds;
        if (newest === undefined || updatedAt > newest) newest = updatedAt;
        if (floor !== undefined && updatedAt < floor) { hasReachedFloor = true; continue; }
        items.push(polled);
      }
      if (hasReachedFloor || !page.nextCursor) return { items, newest, isExceeded: false };
      document = nextDocument;
      variables = { cursor: page.nextCursor, limit: ItemPollPageSize, columnIds: null };
    }
    return { items, newest, isExceeded: true };
  }

  private async readItemsPage(context: TriggerContext, document: string, variables: Record<string, unknown>, isContinuation: boolean): Promise<{ items: PolledItem[]; nextCursor: string }> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const exchange = await this.client.exchange(call, itemChangedOperation, { document, variables });
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    if (exchange.outcome !== 'succeeded') throw this.pollError(exchange);
    let page: { items: PolledItem[]; nextCursor: string } | undefined;
    try { page = decodeItemsPage(exchange.data, isContinuation); }
    catch (error) { throw new Error(`monday.com itemChanged poll returned an invalid page of items: ${error instanceof Error ? error.message : String(error)}`); }
    if (!page) throw new Error('monday.com itemChanged poll found no board with this ID that the connection can see');
    return page;
  }

  /** Pause the connection for a rate or daily call limit; other failures back off the binding. */
  private pollError(exchange: MondayExchange): Error {
    const failure = new Error(`monday.com itemChanged poll: ${exchange.failure?.message ?? 'request failed'}`);
    if (exchange.failure?.kind === 'RATE_LIMIT') return markRateLimited(failure, exchange.retryAfterMs);
    if (exchange.failure?.kind === 'QUOTA_EXHAUSTED') {
      const now = this.client.currentTime().getTime();
      const nextMidnight = (Math.floor(now / 86_400_000) + 1) * 86_400_000;
      return markRateLimited(failure, Math.max(exchange.retryAfterMs, nextMidnight - now));
    }
    return failure;
  }

  private encodeCursor(watermark: bigint | undefined): string {
    return `{"boardId":${goJSONString(this.boardId)},"watermark":"${formatGoTime(watermark)}"}`;
  }
}

/** Count the oldest changes that fit one batch; the first change always fits. */
export function itemChangeBatchSize(changes: readonly ItemChange[]): number {
  let encodedBytes = 0;
  for (const [index, change] of changes.entries()) {
    encodedBytes += change.polled.encoded.length;
    if (index === MaximumItemChangesPerPage || (index > 0 && encodedBytes > MaximumItemChangeBatchBytes)) return index;
  }
  return changes.length;
}

// Go's zero time minus the lookback is still before every real item.
function floorOf(watermark: bigint | undefined): bigint {
  return (watermark ?? GoZeroTimeNanoseconds) - lookbackNanoseconds;
}

/** Decode the token as Go's json.Unmarshal fills itemChangeCursor, keys matched case-insensitively; undefined when Go would fail. */
function decodeCursor(token: string): { boardId: string; watermark: bigint | undefined } | undefined {
  let decoded: unknown;
  try { decoded = JSON.parse(token); } catch { return undefined; }
  if (decoded === null) return { boardId: '', watermark: undefined };
  if (typeof decoded !== 'object' || Array.isArray(decoded)) return undefined;
  let boardId = '', watermark: bigint | undefined;
  for (const [key, value] of Object.entries(decoded as Record<string, unknown>)) {
    if (key.toLowerCase() === 'boardid') {
      if (value !== null && typeof value !== 'string') return undefined;
      if (typeof value === 'string') boardId = value;
    } else if (key.toLowerCase() === 'watermark' && value !== null) {
      if (typeof value !== 'string') return undefined;
      watermark = parseCursorTime(value);
      if (watermark === undefined) return undefined;
    }
  }
  return { boardId, watermark: watermark === GoZeroTimeNanoseconds ? undefined : watermark };
}

/** Add the delivered items and drop entries updated before floor, which no later scan reads. */
function nextItemSnapshot(previous: KeyedSnapshot | undefined, delivered: readonly ItemChange[], floor: bigint): KeyedSnapshot {
  const snapshot = applyKeyedSnapshotChanges(previous, delivered.map(change => change.polled), [], ItemChangedSnapshotOptions);
  const expiredKeys = snapshot.entries.filter(entry => isEntryExpired(entry.value, floor)).map(entry => entry.key);
  return expiredKeys.length ? applyKeyedSnapshotChanges(snapshot, [], expiredKeys, ItemChangedSnapshotOptions) : snapshot;
}

function isEntryExpired(value: Uint8Array | undefined, floor: bigint): boolean {
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(value ?? new Uint8Array()).toString('utf8')); } catch { return true; }
  let updatedAt: bigint | undefined;
  if (decoded !== null && typeof decoded === 'object' && !Array.isArray(decoded)) {
    for (const [key, item] of Object.entries(decoded as Record<string, unknown>)) {
      if (key.toLowerCase() !== 'updatedat' || item === null) continue;
      if (typeof item !== 'string') return true;
      updatedAt = parseCursorTime(item);
      if (updatedAt === undefined) return true;
    }
  } else if (decoded !== null) return true;
  return (updatedAt ?? GoZeroTimeNanoseconds) < floor;
}

/** Read boards[0].items_page, or next_items_page; undefined when the board is not visible. */
function decodeItemsPage(data: unknown, isContinuation: boolean): { items: PolledItem[]; nextCursor: string } | undefined {
  const document = data !== null && typeof data === 'object' && !Array.isArray(data) ? data as Record<string, unknown> : undefined;
  let resource: Record<string, unknown>;
  if (isContinuation) {
    const page = document?.next_items_page;
    if (!page || typeof page !== 'object' || Array.isArray(page)) throw new TypeError('next_items_page is missing');
    resource = page as Record<string, unknown>;
  } else {
    if (data !== null && data !== undefined && !document) throw new TypeError('boards is not a list');
    const boards = document?.boards;
    if (boards !== null && boards !== undefined && !Array.isArray(boards)) throw new TypeError('boards is not a list');
    if (!boards?.length || boards[0] === null) return undefined;
    const board = boards[0] as Record<string, unknown>;
    if (boards.length !== 1 || typeof board !== 'object' || Array.isArray(board) || !board.items_page || typeof board.items_page !== 'object' || Array.isArray(board.items_page)) {
      throw new TypeError('expected one board with an items_page');
    }
    resource = board.items_page as Record<string, unknown>;
  }
  let nextCursor = '';
  if (resource.cursor !== null && resource.cursor !== undefined) {
    const cursor = resource.cursor;
    if (typeof cursor !== 'string' || !cursor || Buffer.byteLength(cursor) > maximumCursorBytes || !/^[\x21-\x7e]+$/.test(cursor)) throw new TypeError('cursor is not a printable token');
    nextCursor = cursor;
  }
  const items = resource.items ?? [];
  if (!Array.isArray(items)) throw new TypeError('items is not a list');
  if (items.length > ItemPollPageSize) throw new TypeError(`page holds more than ${ItemPollPageSize} items`);
  return { items: items.map(decodePolledItem), nextCursor };
}
