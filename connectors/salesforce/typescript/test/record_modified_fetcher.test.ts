// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { StaticCredentialProvider, type TriggerEvent } from '@superdurable/connectors-sdk';
import { SalesforceClient } from '../src/client.js';
import type { RecordPollFetcher } from '../src/record_modified_fetcher.js';
import { MaxTriggerFieldNames, validateRecordModifiedTriggerConfiguration, type RecordModifiedEvent } from '../src/record_modified.js';
import type { Credentials } from '../src/models.js';
import { stringField } from '../src/record_poll_cursor.js';
import { context, newPollTestFetcher, pollTestToken } from './support/poll_test_fetcher.js';

type Events = TriggerEvent<RecordModifiedEvent>[];

async function fetchAll(fetcher: RecordPollFetcher, cursor: Cursor): Promise<[Events, Cursor]> {
  const events: Events = [];
  for (let page = 0; page < 20; page++) {
    const changes = await fetcher.fetchChanges(context, cursor);
    events.push(...changes.changes);
    cursor = changes.next;
    if (!changes.hasMorePages) return [events, cursor];
  }
  throw new Error('poll never caught up');
}

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try { await promise; } catch (error) { return error; }
  throw new Error('expected a rejection');
}

it('seeds existing records as a baseline and delivers later changes', async () => {
  const [fetcher, org, clock] = newPollTestFetcher(200, { sObjectType: 'Opportunity', fieldNames: ['Name', 'StageName', 'Id'] });
  const existing = org.insert('Opportunity', { Name: 'Renewal', StageName: 'Prospecting' });
  org.insert('Account', { Name: 'Meridian' });
  let cursor = await fetcher.seedCursor(context);
  expect(cursor.token, 'the cursor sits on the newest existing record').toContain(existing);
  const [baseline, caughtUp] = await fetchAll(fetcher, cursor);
  cursor = caughtUp;
  expect(baseline, 'existing records are never delivered').toEqual([]);

  const created = org.insert('Opportunity', { Name: 'Expansion', StageName: 'Qualification' });
  org.update(existing, { StageName: 'Closed Won' });
  clock.advance(1000);
  const refetched = await fetcher.fetchChanges(context, cursor);
  const [changes, next] = await fetchAll(fetcher, cursor);
  expect(changes).toHaveLength(2);
  expect(refetched.changes, 'a refetch from one cursor yields the same event IDs').toEqual(changes);
  expect([changes[0]!.payload.recordId, changes[0]!.payload.changeKind]).toEqual([created, 'created']);
  expect([changes[1]!.payload.recordId, changes[1]!.payload.changeKind]).toEqual([existing, 'updated']);
  expect(stringField(changes[1]!.payload.record, 'StageName')).toBe('Closed Won');
  expect(changes[0]!.id).toMatch(/^sfdc-[0-9a-f]{32}$/);
  expect(new Date(changes[1]!.payload.systemModstamp)).toEqual(changes[1]!.occurredAt);
  expect(org.requests.at(-1)!.soql).toContain('SELECT Id, CreatedDate, LastModifiedDate, SystemModstamp, Name, StageName FROM Opportunity');

  expect((await fetchAll(fetcher, next))[0]).toEqual([]);
  org.update(existing, { StageName: 'Closed Lost' });
  const [later] = await fetchAll(fetcher, next);
  expect(later).toHaveLength(1);
  expect(later[0]!.id, 'a later change to one record gets a new event ID').not.toBe(changes[1]!.id);
});

it('pages records sharing one SystemModstamp by Id', async () => {
  const [fetcher, org] = newPollTestFetcher(2, { sObjectType: 'Lead' });
  org.insert('Lead', {});
  const cursor = await fetcher.seedCursor(context);
  const ids = org.insertBatch('Lead', 5);
  const [events] = await fetchAll(fetcher, cursor);
  expect(events.map(event => event.payload.recordId)).toEqual(ids);
});

it('leaves recent changes for the settle delay', async () => {
  const [fetcher, org, clock] = newPollTestFetcher(200, { sObjectType: 'Case' });
  const cursor = await fetcher.seedCursor(context);
  expect(cursor.token, 'an empty object seeds at the settle horizon').toContain('"systemModstamp":"2026-03-02T08:59:45Z"');
  clock.advance(50_000);
  org.insert('Case', {}); // Stamped 08:59:50, after the 08:59:35 horizon of a clock 50s back.
  clock.advance(-50_000);
  const queries = org.queryCount;
  const page = await fetcher.fetchChanges(context, cursor);
  expect(page.changes).toEqual([]);
  expect(page.next).toEqual(cursor);
  expect(org.queryCount, 'a cursor at the horizon sends no request').toBe(queries);
  clock.advance(30_000);
  expect((await fetchAll(fetcher, cursor))[0]).toHaveLength(1);
});

it('keeps the settle delay when Salesforce runs behind the host', async () => {
  const [fetcher, org, clock] = newPollTestFetcher(200, { sObjectType: 'Case' });
  org.clockSkewMs = -60_000; // Salesforce reads 08:59:00 while this host reads 09:00:00.
  let cursor = await fetcher.seedCursor(context);
  expect(cursor.token, "an empty object seeds at Salesforce's settle horizon").toContain('"systemModstamp":"2026-03-02T08:58:45Z"');
  const recent = org.insertRecent('Case', {}); // Stamped 08:59:00, after Salesforce's horizon.
  let events: Events;
  [events, cursor] = await fetchAll(fetcher, cursor);
  expect(events, 'a change Salesforce stamped within the settle delay waits').toEqual([]);
  clock.advance(20_000);
  [events] = await fetchAll(fetcher, cursor);
  expect(events.map(event => event.payload.recordId)).toEqual([recent]);
});

it("stops a page at Salesforce's horizon after the host clock jumps ahead", async () => {
  const [fetcher, org, clock] = newPollTestFetcher(200, { sObjectType: 'Case' });
  const cursor = await fetcher.seedCursor(context);
  clock.advance(60_000);
  const settled = org.insert('Case', {});
  org.clockSkewMs = -30_000;
  org.insertRecent('Case', {});
  const page = await fetcher.fetchChanges(context, cursor);
  expect(page.changes.map(event => event.payload.recordId)).toEqual([settled]);
  expect(page.hasMorePages).toBe(false);
  expect((await fetchAll(fetcher, page.next))[0]).toEqual([]);
  clock.advance(20_000);
  expect((await fetchAll(fetcher, page.next))[0]).toHaveLength(1);
});

it.each([
  'not json',
  '{"sObjectType":"Contact","systemModstamp":"yesterday"}',
  '{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z","recordId":"bad id"}',
  '{"sObjectType":"Lead","systemModstamp":"2026-03-02T08:00:00Z"}',
])('marks the invalid cursor %s for reseed', async token => {
  const [fetcher] = newPollTestFetcher(200, { sObjectType: 'Contact' });
  expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, { token })))).toBe(true);
});

it('pauses the poller on REQUEST_LIMIT_EXCEEDED', async () => {
  const [fetcher, org] = newPollTestFetcher(200, { sObjectType: 'Contact' });
  org.rateLimitNext(2, '120');
  const seeded = await rejection(fetcher.seedCursor(context));
  expect(seeded).toBeInstanceOf(RateLimitedError);
  expect((seeded as RateLimitedError).retryAfterMs).toBe(120_000);
  expect(String((seeded as Error).message) + String((seeded as Error).cause)).toContain('REQUEST_LIMIT_EXCEEDED');
  expect(JSON.stringify(seeded) + String(seeded)).not.toContain(pollTestToken);
  const fetched = await rejection(fetcher.fetchChanges(context, { token: '{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}' }));
  expect(fetched).toBeInstanceOf(RateLimitedError);
  expect(isCursorInvalid(fetched)).toBe(false);
});

it('fails the poll for an unreadable object instead of resetting the cursor', async () => {
  const [fetcher, org] = newPollTestFetcher(200, { sObjectType: 'Contact' });
  org.rejectedObjects.set('Contact', 'INVALID_TYPE');
  const seeded = await rejection(fetcher.seedCursor(context));
  expect(String((seeded as Error).message)).toContain('INVALID_TYPE');
  expect(isCursorInvalid(seeded)).toBe(false);
  const fetched = await rejection(fetcher.fetchChanges(context, { token: '{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}' }));
  expect(isCursorInvalid(fetched)).toBe(false);
});

it('validates the Trigger configuration as Go does', () => {
  expect(() => validateRecordModifiedTriggerConfiguration({ sObjectType: 'Invoice__c', fieldNames: ['Amount__c'] })).not.toThrow();
  const tooMany = Array.from({ length: MaxTriggerFieldNames + 1 }, (_, index) => 'Field' + 'x'.repeat(index + 1));
  for (const configuration of [
    { sObjectType: '' }, { sObjectType: 'Bad Name' }, { sObjectType: 'Contact', fieldNames: ['Account.Name'] },
    { sObjectType: 'Contact', fieldNames: ['Email', 'email'] }, { sObjectType: 'Contact', fieldNames: tooMany },
  ]) expect(() => validateRecordModifiedTriggerConfiguration(configuration), JSON.stringify(configuration)).toThrow();
});

it('rejects an unsupported poll configuration', () => {
  const credentials = new StaticCredentialProvider<Credentials>([]);
  expect(() => new SalesforceClient({ pollIntervalMs: 1000 }, credentials)).toThrow('pollInterval');
  expect(() => new SalesforceClient({ pollPageSize: 501 }, credentials)).toThrow('pollPageSize');
});
