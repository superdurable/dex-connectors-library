// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { SecretString, type TriggerBindingRef, type TriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { AirtableClient } from '../src/client.js';
import { RecordChangedFetcher } from '../src/record_changed_fetcher.js';
import { RecordChangedTriggerDefinition, validateRecordChangedTriggerConfiguration, type RecordChangedTriggerConfiguration } from '../src/trigger.js';
import { AccessToken, AirtableTableFake, BaseID, Endpoint, FieldID, TableID, fakeRecord, type FakeRecord } from './airtable_table_fake.js';

const seedTime = Date.UTC(2026, 10, 15, 9, 0, 0);
const binding: TriggerBindingRef = { connection: { provider: 'airtable', name: 'airtable-intake' }, trigger: RecordChangedTriggerDefinition.trigger, name: 'lead-changed' };
const configuration: RecordChangedTriggerConfiguration = { baseId: BaseID, tableId: TableID, fieldId: FieldID };
const maximumPollCursorTokenBytes = 64 * 1024;

function context(): TriggerContext { return { signal: new AbortController().signal } as TriggerContext; }
function at(offsetMs: number): Date { return new Date(seedTime + offsetMs); }

/** A clock that moves one second per reading, so per-base pacing never waits in these tests. */
function steppingClock(): () => Date {
  let now = Date.now();
  return () => new Date((now += 1_000));
}

function newFetcher(table: AirtableTableFake, pollPageSize: number): RecordChangedFetcher {
  const client = new AirtableClient({ endpoint: Endpoint, pollIntervalMs: 1_000, pollPageSize }, {
    resolve: async () => ({ personalAccessToken: new SecretString(AccessToken) }),
  }, { send: table.send, now: steppingClock() });
  return new RecordChangedFetcher(client, binding, configuration);
}

async function fetchIDs(fetcher: RecordChangedFetcher, cursor: Cursor): Promise<{ ids: string[]; next: Cursor; hasMore: boolean }> {
  const page = await fetcher.fetchChanges(context(), cursor);
  return { ids: page.changes.map(event => event.payload.record.id), next: page.next, hasMore: page.hasMorePages ?? false };
}

/** Fetch until a page reports no more and delivers nothing, counting deliveries per record. */
async function drainIDs(fetcher: RecordChangedFetcher, cursor: Cursor, maximumFetches: number): Promise<{ delivered: Map<string, number>; cursor: Cursor }> {
  const delivered = new Map<string, number>();
  for (let fetch = 0; fetch < maximumFetches; fetch++) {
    const page = await fetchIDs(fetcher, cursor);
    expect(Buffer.byteLength(page.next.token)).toBeLessThanOrEqual(maximumPollCursorTokenBytes);
    cursor = page.next;
    for (const id of page.ids) delivered.set(id, (delivered.get(id) ?? 0) + 1);
    if (!page.hasMore && page.ids.length === 0) return { delivered, cursor };
  }
  throw new Error(`the feed did not become current within ${maximumFetches} fetches`);
}

function burstRecords(prefix: string, count: number, modifiedAt: Date): FakeRecord[] {
  return Array.from({ length: count }, (_, index) => fakeRecord(`rec${prefix}${String(index).padStart(14 - prefix.length, '0')}`, modifiedAt));
}

describe('recordChanged fetcher at parity with the Go fetcher tests', () => {
  it('seeds without existing records and delivers later changes oldest first', async () => {
    const table = new AirtableTableFake(fakeRecord('recExisting000001', at(0)), fakeRecord('recExisting000002', at(0)), fakeRecord('recOlder000000001', at(-3_600_000)));
    const fetcher = newFetcher(table, 100);
    let cursor = await fetcher.seedCursor(context());
    expect(cursor.token).toBe('{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["recExisting000002:0","recExisting000001:0"]}');
    let page = await fetchIDs(fetcher, cursor);
    expect(page.ids, 'records present at seed never start Flows').toEqual([]);
    cursor = page.next;

    table.put(fakeRecord('recNewLead0000001', at(1_000)), { ...fakeRecord('recExisting000001', at(2_000)), createdAt: at(-3_600_000) }, fakeRecord('recSameMilli00001', at(0)));
    const changes = await fetcher.fetchChanges(context(), cursor);
    expect(changes.changes.map(event => event.payload.record.id)).toEqual(['recSameMilli00001', 'recNewLead0000001', 'recExisting000001']);
    const [, created, update] = changes.changes;
    expect(created!.payload.isCreatedRecord).toBe(true);
    expect(update!.payload.isCreatedRecord).toBe(false);
    expect(update!.id).toBe('recExisting000001-20261115T090002000000000Z');
    expect(update!.occurredAt).toEqual(at(2_000));
    expect(update!.payload).toMatchObject({ baseId: BaseID, tableId: TableID, lastModifiedTime: '2026-11-15T09:00:02Z' });
    expect(update!.payload.record.fields?.Email).toBe('recExisting000001@example.test');

    page = await fetchIDs(fetcher, changes.next);
    expect(page.ids, 'a delivered change is not delivered again').toEqual([]);
    table.put(fakeRecord('recExisting000001', at(3_000)));
    expect((await fetchIDs(fetcher, page.next)).ids, 'a later edit of the same record is a new change').toEqual(['recExisting000001']);
    expect(table.listRequests[0]!.sort).toEqual([{ field: FieldID, direction: 'desc' }]);
    expect(table.listRequests.slice(1).every(request => request.sort[0]!.direction === 'asc')).toBe(true);
    expect(table.listRequests.at(-1)!.filterByFormula).toBe('IS_AFTER({fldLastModified01}, DATETIME_PARSE("2026-11-15T08:59:56.000Z"))');
    expect(table.schemaRequests, 'the field name is read once').toBe(1);
  });

  it('delivers pages and tie groups larger than a page once', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 2);
    const cursor = await fetcher.seedCursor(context());
    expect(cursor.token).toBe('{"v":2,"fieldId":"fldLastModified01","after":""}');
    table.put(fakeRecord('recTie00000000001', at(60_000)), fakeRecord('recTie00000000002', at(60_000)), fakeRecord('recTie00000000003', at(60_000)), fakeRecord('recLater000000001', at(61_000)));
    const { delivered } = await drainIDs(fetcher, cursor, 4);
    expect([...delivered.entries()].sort()).toEqual([['recLater000000001', 1], ['recTie00000000001', 1], ['recTie00000000002', 1], ['recTie00000000003', 1]]);
    expect(table.listRequests.some(request => request.offset), 'ties are skipped through the offset').toBe(true);
  });

  it('resolves a renamed field again and fails the poll for a changed field type', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 100);
    const cursor = await fetcher.seedCursor(context());
    table.renameField('Updated At');
    table.put(fakeRecord('recRenamed0000001', at(0)));
    await expect(fetcher.fetchChanges(context(), cursor)).rejects.toThrow('field name is read again');
    expect((await fetchIDs(fetcher, cursor)).ids).toEqual(['recRenamed0000001']);
    expect(table.schemaRequests).toBe(2);
    table.setFieldType('dateTime');
    table.renameField('Due');
    await expect(fetcher.fetchChanges(context(), cursor)).rejects.toThrow();
    const error = await fetcher.fetchChanges(context(), cursor).catch((caught: unknown) => caught);
    expect(String(error)).toContain('not a Last modified time field');
    expect(isCursorInvalid(error), 'a configuration fault backs off without losing the cursor').toBe(false);
  });

  it('pauses the runner for Airtable\'s thirty seconds after a 429', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 100);
    table.script({ status: 429, retryAfter: '1', errorType: 'RATE_LIMIT_REACHED' });
    const error = await fetcher.seedCursor(context()).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(RateLimitedError);
    expect((error as RateLimitedError).retryAfterMs).toBeGreaterThanOrEqual(29_000);
    expect(String(error)).not.toContain('SENTINEL');
    const before = table.completedRequests;
    await expect(fetcher.seedCursor(context())).rejects.toBeInstanceOf(RateLimitedError);
    expect(table.completedRequests, 'the per-base hold waits without calling Airtable').toBe(before);
  });
});

describe('recordChanged cursors', () => {
  it.each([
    '', 'not-json', '{"v":1,"fieldId":"fldLastModified01","after":""}', '{"v":2,"fieldId":"fldSomethingElse1","after":""}',
    '{"v":2,"fieldId":"fldLastModified01","after":"yesterday"}', '{"v":2,"fieldId":"fldLastModified01","after":"","seen":["recExisting000001:0"]}',
    '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["Lead:0"]}',
    '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","scan":{"since":"","offset":"itr1/recExisting000001"}}',
    '{"v":2.0,"fieldId":"fldLastModified01","after":""}', '{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["recExisting000001:-1"]}',
    '{"v":2,"fieldId":"fldLastModified01","after":"2026-02-30T09:00:00Z"}', '{"v":2,"fieldId":"fldLastModified01","after":""} x',
  ])('reseeds the unusable cursor %s', async token => {
    const error = await newFetcher(new AirtableTableFake(), 100).fetchChanges(context(), { token }).catch((caught: unknown) => caught);
    expect(isCursorInvalid(error)).toBe(true);
  });

  it('decodes cursors as Go does: keys ignore case, later keys win, unknown keys are ignored', async () => {
    const token = '{"V":2,"FIELDID":"fldLastModified01","after":"2026-11-15T09:00:00Z","After":"2026-11-15T10:00:00+01:00","after2":1,"seen":null}';
    const page = await newFetcher(new AirtableTableFake(), 100).fetchChanges(context(), { token });
    expect(page.next.token).toBe('{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z"}');
  });

  it('keeps the cursor after provider failures', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 100);
    const cursor = await fetcher.seedCursor(context());
    table.script({ status: 503 }, { status: 403, errorType: 'INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND' });
    for (let attempt = 0; attempt < 2; attempt++) {
      const error = await fetcher.fetchChanges(context(), cursor).catch((caught: unknown) => caught);
      expect(error).toBeInstanceOf(Error);
      expect(isCursorInvalid(error)).toBe(false);
      expect(error).not.toBeInstanceOf(RateLimitedError);
    }
  });

  it('validates configurations and poll settings', () => {
    validateRecordChangedTriggerConfiguration(configuration);
    for (const invalid of [{ ...configuration, baseId: '' }, { ...configuration, tableId: 'Leads' }, { ...configuration, fieldId: 'Last Modified' }]) {
      expect(() => validateRecordChangedTriggerConfiguration(invalid)).toThrow(TypeError);
    }
    const credentials = { resolve: async () => ({ personalAccessToken: new SecretString(AccessToken) }) };
    expect(() => new AirtableClient({ pollIntervalMs: 1 }, credentials)).toThrow('pollInterval');
    expect(() => new AirtableClient({ pollPageSize: 101 }, credentials)).toThrow('pollPageSize');
  });
});

describe('recordChanged bursts, overlaps, and offsets', () => {
  it('delivers a burst larger than the cursor bound without loss or repeats', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 100);
    let cursor = await fetcher.seedCursor(context());
    table.put(...burstRecords('Burst', 2600, at(60_000)));
    let drained = await drainIDs(fetcher, cursor, 200);
    expect(drained.delivered.size).toBe(2600);
    expect([...drained.delivered.values()].every(count => count === 1)).toBe(true);
    cursor = drained.cursor;
    table.put(fakeRecord('recAfterBurst0001', at(61_000)));
    drained = await drainIDs(fetcher, cursor, 50);
    expect([...drained.delivered.keys()], 'the poll is not stuck behind the delivered burst').toEqual(['recAfterBurst0001']);
  });

  it('resumes runs of delivered records longer than one fetch from the stored offset', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 10);
    const cursor = await fetcher.seedCursor(context());
    table.put(...burstRecords('Run', 250, at(60_000)));
    let drained = await drainIDs(fetcher, cursor, 200);
    expect(drained.delivered.size).toBe(250);
    table.put(fakeRecord('recAfterRun000001', at(62_000)));
    drained = await drainIDs(fetcher, drained.cursor, 20);
    expect([...drained.delivered.keys()]).toEqual(['recAfterRun000001']);
  });

  it('delivers a change that becomes visible late inside the overlap', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 100);
    const cursor = await fetcher.seedCursor(context());
    table.put(fakeRecord('recLaterStamp0001', at(4_000)));
    let drained = await drainIDs(fetcher, cursor, 10);
    expect([...drained.delivered.keys()]).toEqual(['recLaterStamp0001']);
    table.put(fakeRecord('recEarlierStamp01', at(500)));
    drained = await drainIDs(fetcher, drained.cursor, 10);
    expect([...drained.delivered.keys()]).toEqual(['recEarlierStamp01']);
    drained = await drainIDs(fetcher, drained.cursor, 10);
    expect(drained.delivered.size, 'remembered records inside the overlap are not delivered again').toBe(0);
  });

  it('restarts the scan after an expired offset without loss or repeats', async () => {
    const table = new AirtableTableFake();
    const fetcher = newFetcher(table, 10);
    let cursor = await fetcher.seedCursor(context());
    table.put(...burstRecords('Expire', 35, at(60_000)));
    const delivered = new Map<string, number>();
    for (let fetch = 0; fetch < 30; fetch++) {
      table.expireIterators();
      const page = await fetchIDs(fetcher, cursor);
      cursor = page.next;
      for (const id of page.ids) delivered.set(id, (delivered.get(id) ?? 0) + 1);
      if (!page.hasMore && page.ids.length === 0) break;
    }
    expect(delivered.size).toBe(35);
    expect([...delivered.values()].every(count => count === 1)).toBe(true);
  });

  it('hides existing records beyond the seed\'s first page', async () => {
    const table = new AirtableTableFake(...burstRecords('Existing', 250, at(0)));
    const fetcher = newFetcher(table, 100);
    const cursor = await fetcher.seedCursor(context());
    table.put(fakeRecord('recNewAfterSeed01', at(1_000)));
    const { delivered } = await drainIDs(fetcher, cursor, 20);
    expect([...delivered.keys()]).toEqual(['recNewAfterSeed01']);
  });
});

/**
 * SHA-256 digests of every committed cursor token and every event ID, one per line, that the Go fetcher
 * produced for the same scenarios against its internal/airtablefake table.
 */
const goParityDigests = {
  burst: [33, 'a21653a4a592406a5bf113fd3c31a9692dc1b28d724c40fe2ccca32ede34b049', 2601, 'b5238cc5afbf9c62357fe9d2cd9e8779a5ab54d4742ea36a037da8b2add85646'],
  run: [32, 'b0f4c0f6028fa0e8ae50e7044710c3262446573cc78affe81e8cafdef2d3d12c', 251, '4db52c52614551b2895a9c05137f3ea0decf82f4b77372462781c5d4a3a080eb'],
  expire: [6, 'd178370ba86b9c7ac2268c9915dfe7f88a68c94ef7810fc8ed8b29204b4e045b', 35, 'f3abe6225501b0583466d4a4163caf751d5c8c4ae0adf4918b43ce8f80e39cee'],
  seedpage: [6, '9848e5c08d99c1cb76995743409a1e422d59e193e2e503e4bbdf018a84fca076', 30, '2a64097f148840dee41cfcd9e47cb77409b794a920babb5e46a263d9152f64b7'],
} as const;

/** Drains a fetcher like the Go parity scenarios and digests its tokens and event IDs. */
class ParityRecorder {
  readonly tokens: string[] = [];
  readonly ids: string[] = [];
  constructor(private readonly fetcher: RecordChangedFetcher, private readonly table: AirtableTableFake) {}

  async seed(): Promise<Cursor> {
    const cursor = await this.fetcher.seedCursor(context());
    this.tokens.push(cursor.token);
    return cursor;
  }

  async drain(cursor: Cursor, shouldExpireIterators = false): Promise<Cursor> {
    for (let fetch = 0; fetch < 300; fetch++) {
      if (shouldExpireIterators) this.table.expireIterators();
      const page = await this.fetcher.fetchChanges(context(), cursor);
      cursor = page.next;
      this.tokens.push(cursor.token);
      this.ids.push(...page.changes.map(event => event.id));
      if (!page.hasMorePages && page.changes.length === 0) return cursor;
    }
    throw new Error('not current');
  }

  digests(): readonly [number, string, number, string] {
    const digest = (lines: string[]) => createHash('sha256').update(lines.join('\n')).digest('hex');
    return [this.tokens.length, digest(this.tokens), this.ids.length, digest(this.ids)];
  }
}

describe('recordChanged Go parity', () => {
  it('matches every cursor token and event ID of the Go fetcher byte for byte', async () => {
    let table = new AirtableTableFake();
    const burst = new ParityRecorder(newFetcher(table, 100), table);
    let cursor = await burst.seed();
    table.put(...burstRecords('Burst', 2600, at(60_123)));
    cursor = await burst.drain(cursor);
    table.put(fakeRecord('recAfterBurst0001', at(61_623)));
    await burst.drain(cursor);
    expect(burst.tokens.at(-1)).toBe('{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:01:01.623Z","floor":"2026-11-15T09:01:00.124Z","seen":["recAfterBurst0001:0"]}');
    expect(burst.digests()).toEqual(goParityDigests.burst);

    table = new AirtableTableFake();
    const run = new ParityRecorder(newFetcher(table, 10), table);
    cursor = await run.seed();
    table.put(...burstRecords('Run', 250, at(60_000)));
    cursor = await run.drain(cursor);
    table.put(fakeRecord('recAfterRun000001', at(62_000)));
    await run.drain(cursor);
    expect(run.digests()).toEqual(goParityDigests.run);

    table = new AirtableTableFake();
    const expire = new ParityRecorder(newFetcher(table, 10), table);
    cursor = await expire.seed();
    table.put(...burstRecords('Expire', 35, at(60_000)));
    await expire.drain(cursor, true);
    expect(expire.digests()).toEqual(goParityDigests.expire);

    table = new AirtableTableFake(...Array.from({ length: 250 }, (_, index) => fakeRecord(`recExisting${String(index).padStart(6, '0')}`, at(index % 7 * 317))));
    const seedPage = new ParityRecorder(newFetcher(table, 100), table);
    cursor = await seedPage.seed();
    table.put(...Array.from({ length: 30 }, (_, index) => fakeRecord(`recStagger${String(index).padStart(7, '0')}`, at(1_000 + index * 211))));
    cursor = await seedPage.drain(cursor);
    table.put(fakeRecord('recStagger0000003', at(9_000)), fakeRecord('recLateVisible001', at(5_007)));
    await seedPage.drain(cursor);
    expect(seedPage.digests()).toEqual(goParityDigests.seedpage);
  });
});
