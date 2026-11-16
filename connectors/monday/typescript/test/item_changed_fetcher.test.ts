// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// itemChanged fetcher behavior, ported from the Go connector's item_changed_fetcher tests.
import { newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { ItemChangedTriggerConfigurationSchema, ItemChangedTriggerDefinition, MondayClient, type ItemChangedEvent } from '../src/index.js';
import {
  ItemChangeFetcher, ItemPollPageSize, ItemScanExceededError, MaximumItemChangeBatchBytes, MaximumItemChangesPerPage, MaximumItemScanPages, itemChangeBatchSize, type ItemChange,
} from '../src/item_changed_fetcher.js';
import { PolledItem } from '../src/item_snapshot.js';
import { MondayBoardFake, fakeCredentials, newFakeClient } from './monday_board_fake.js';

const boardId = '1234567890';
const reference = { provider: 'monday', name: 'monday-board-watch' };
const context = newTriggerContext();

function newFetcher(board: MondayBoardFake, now?: () => Date): ItemChangeFetcher {
  return new ItemChangeFetcher(newFakeClient(board.send, now), reference, ItemChangedTriggerDefinition.trigger, { boardId });
}

/** Follow hasMorePages from cursor, committing each page as the kit does. */
async function fetchAll(fetcher: ItemChangeFetcher, cursor: Cursor): Promise<[Array<TriggerEvent<ItemChangedEvent>>, Cursor]> {
  const events: Array<TriggerEvent<ItemChangedEvent>> = [];
  for (;;) {
    const page = await fetcher.fetchChanges(context, cursor);
    events.push(...page.changes);
    cursor = page.next;
    if (!page.hasMorePages) return [events, cursor];
  }
}

const watermarkToken = (watermark: string) => `{"boardId":"${boardId}","watermark":"${watermark}"}`;

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try { await promise; } catch (error) { return error; }
  throw new Error('expected a rejection');
}

describe('ItemChangeFetcher', () => {
  it('seeds without backlog and delivers changes oldest first', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550001', 'Existing request'); board.create('5550002', 'Second existing request');
    const fetcher = newFetcher(board);
    let cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toBe(watermarkToken(board.updatedAtText('5550002')));
    let events: Array<TriggerEvent<ItemChangedEvent>>;
    [events, cursor] = await fetchAll(fetcher, cursor);
    expect(events, 'existing items are the baseline').toEqual([]);

    board.create('5550003', 'New request'); board.setStatus('5550001', 'Done');
    [events, cursor] = await fetchAll(fetcher, cursor);
    expect(events.map(event => [event.payload.item.id, event.payload.changeKind])).toEqual([['5550003', 'created'], ['5550001', 'updated']]);
    expect(events[1]!.payload.item.columnValues?.[0]).toMatchObject({ text: 'Done', value: { index: 1 } });
    expect(events[1]!.occurredAt.getTime()).toBe(board.updatedAt('5550001'));
    const [again] = await fetchAll(fetcher, cursor);
    expect(again, 'the snapshot keeps items inside the lookback window from arriving twice').toEqual([]);
    const [refetched] = await fetchAll(newFetcher(board), { token: watermarkToken(board.updatedAtText('5550002')) });
    const refetchedIDs = new Map(refetched.map(event => [event.payload.item.id, event.id]));
    expect(refetchedIDs.get('5550003'), 'a refetch yields the same Trigger event ID').toBe(events[0]!.id);
    expect(refetchedIDs.get('5550001')).toBe(events[1]!.id);
  });

  it('delivers a second change within the same second', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550001', 'Request');
    const fetcher = newFetcher(board);
    const seeded = await fetcher.seedCursor(context);
    board.setStatus('5550001', 'Stuck');
    const [first, cursor] = await fetchAll(fetcher, seeded);
    expect(first).toHaveLength(1);
    board.setClock(board.updatedAt('5550001'));
    board.setStatus('5550001', 'Done');
    const [second] = await fetchAll(fetcher, cursor);
    expect(second).toHaveLength(1);
    expect(second[0]!.payload.item.columnValues?.[0]?.text).toBe('Done');
    expect(second[0]!.id).not.toBe(first[0]!.id);
  });

  it('treats every item of an empty baseline board as created', async () => {
    const board = new MondayBoardFake(boardId), fetcher = newFetcher(board);
    const cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toBe(watermarkToken('0001-01-01T00:00:00Z'));
    board.create('5550001', 'First request');
    const [events] = await fetchAll(fetcher, cursor);
    expect(events.map(event => event.payload.changeKind)).toEqual(['created']);
  });

  it('splits a burst into batches from one scan', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550000', 'Existing');
    const fetcher = newFetcher(board), cursor = await fetcher.seedCursor(context);
    for (let index = 1; index <= 150; index++) board.create(String(5550000 + index), `Imported ${index}`);
    const requestsBefore = board.requests.length;
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toHaveLength(MaximumItemChangesPerPage);
    expect(page.hasMorePages).toBe(true);
    expect(page.changes[0]!.payload.item.id).toBe('5550001');
    const scanRequests = board.requests.length - requestsBefore;
    expect(scanRequests, 'two pages reach the item before the cursor').toBe(2);
    const rest = await fetcher.fetchChanges(context, page.next);
    expect(rest.changes).toHaveLength(50);
    expect(rest.hasMorePages).toBe(false);
    expect(rest.changes[49]!.payload.item.id).toBe('5550150');
    expect(board.requests.length, 'the rest of the burst comes from the same scan').toBe(requestsBefore + scanRequests);
  });

  it('pauses the connection for rate and daily limits', async () => {
    const board = new MondayBoardFake(boardId);
    const fetcher = newFetcher(board, () => new Date(Date.UTC(2026, 2, 2, 21, 30)));
    board.rateLimitNextRequests(1, 7);
    const rateLimited = await rejection(fetcher.seedCursor(context));
    expect(rateLimited).toBeInstanceOf(RateLimitedError);
    expect((rateLimited as RateLimitedError).retryAfterMs).toBe(7_000);
    board.exhaustDailyLimitForNextRequests(1);
    const dailyLimited = await rejection(fetcher.fetchChanges(context, { token: watermarkToken('2026-03-02T09:00:00Z') }));
    expect(dailyLimited).toBeInstanceOf(RateLimitedError);
    expect((dailyLimited as RateLimitedError).retryAfterMs, 'the daily call limit resets at midnight UTC').toBe(150 * 60_000);
  });

  it('reports an unknown board as a binding failure', async () => {
    const failure = await rejection(newFetcher(new MondayBoardFake('999')).seedCursor(context));
    expect((failure as Error).message).toContain('found no board');
    expect(isCursorInvalid(failure)).toBe(false);
    expect(failure).not.toBeInstanceOf(RateLimitedError);
  });

  it('marks foreign, undecodable, and oversized cursors invalid', async () => {
    const board = new MondayBoardFake(boardId), fetcher = newFetcher(board);
    for (const token of ['not json', '[]', '{"boardId":"999","watermark":"2026-03-02T09:00:00Z"}', '{"boardId":"1234567890","watermark":"yesterday"}', 'null']) {
      expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, { token }))), token).toBe(true);
    }
    for (let index = 1; index <= MaximumItemScanPages * ItemPollPageSize + 1; index++) board.create(String(6000000 + index), 'Bulk import');
    const oversized = await rejection(fetcher.fetchChanges(context, { token: watermarkToken('2026-03-02T09:00:00Z') }));
    expect(isCursorInvalid(oversized), 'more changes than one scan reads record a new baseline').toBe(true);
    expect((oversized as Error).cause).toBeInstanceOf(ItemScanExceededError);
  });

  it('accepts a cursor in any key case and another offset', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550001', 'Request');
    const page = await newFetcher(board).fetchChanges(context, { token: '{"BoardID":"1234567890","Watermark":"2026-03-02T11:00:01+02:00"}', snapshot: { entries: [] } });
    expect(page.changes.map(event => event.payload.changeKind), 'an empty snapshot delivers the item inside the lookback window').toEqual(['created']);
    expect(page.next.token).toBe(watermarkToken('2026-03-02T09:00:01Z'));
  });

  it('requires a numeric board ID', () => {
    expect(ItemChangedTriggerConfigurationSchema.safeParse({ boardId }).success).toBe(true);
    for (const candidate of ['', ' 1234567890', 'board', '0123']) expect(ItemChangedTriggerConfigurationSchema.safeParse({ boardId: candidate }).success, candidate).toBe(false);
  });

  it('stops batches at the count or byte bound', () => {
    const changes = (count: number, encodedBytes: number): ItemChange[] => Array.from({ length: count }, () => ({ polled: new PolledItem({ id: '1', name: '' }, undefined, 0n, new Uint8Array(encodedBytes)), kind: 'updated' }));
    expect(itemChangeBatchSize(changes(40, 1024))).toBe(40);
    expect(itemChangeBatchSize(changes(150, 1024))).toBe(MaximumItemChangesPerPage);
    expect(itemChangeBatchSize(changes(10, MaximumItemChangeBatchBytes / 4)), 'a raised response limit cannot overfill the inbox').toBe(4);
    expect(itemChangeBatchSize(changes(3, MaximumItemChangeBatchBytes + 1)), 'an oversized item still moves alone').toBe(1);
  });

  it('refuses a poll interval under thirty seconds', () => {
    expect(() => new MondayClient({ pollIntervalMs: 29_500 }, fakeCredentials())).toThrow('at least 30s');
    expect(new MondayClient({}, fakeCredentials()).pollIntervalMs).toBe(300_000);
  });
});
