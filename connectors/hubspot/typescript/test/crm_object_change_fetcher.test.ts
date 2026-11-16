// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it } from 'vitest';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { SecretString, StaticCredentialProvider, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { HubSpotClient } from '../src/client.js';
import { CRMObjectChangeFetcher } from '../src/crm_object_change_fetcher.js';
import { decodeCRMObjectChangeCursor, encodeCRMObjectChangeCursor, lagWindowStart, maximumSeenRecordVersions, pruneSeen } from '../src/crm_object_change_cursor.js';
import { MaximumTriggerProperties, validateCRMObjectChangedTriggerConfiguration, type CRMObjectChangedEvent } from '../src/crm_object_changed.js';
import { parseRFC3339Milliseconds } from '../src/crm_object_change_search.js';
import type { ObjectType } from '../src/operations.js';
import { FakeAccessToken, HubSpotSearchFake } from './support/hubspot_search_fake.js';

const fakeStart = new Date(Date.UTC(2026, 9, 25, 10, 0, 0));
const context = newTriggerContext();
const fakes: HubSpotSearchFake[] = [];
afterEach(async () => { await Promise.all(fakes.splice(0).map(fake => fake.close())); });

async function newFakeChangeFetcher(objectType: ObjectType, ...properties: string[]): Promise<[CRMObjectChangeFetcher, HubSpotSearchFake]> {
  const fake = await HubSpotSearchFake.start(objectType, fakeStart);
  fakes.push(fake);
  const reference = { provider: 'hubspot', name: 'hubspot-crm-watcher' };
  const client = new HubSpotClient({ endpoint: fake.url }, new StaticCredentialProvider([[reference, { authMethodId: 'private-app-token', accessToken: new SecretString(FakeAccessToken) }]]));
  const configuration = { objectType, properties };
  validateCRMObjectChangedTriggerConfiguration(configuration);
  return [new CRMObjectChangeFetcher(client, reference, configuration), fake];
}

/** Fetch until the feed has no more pages, as one kit poll does. */
async function drainChanges(fetcher: CRMObjectChangeFetcher, cursor: Cursor): Promise<[TriggerEvent<CRMObjectChangedEvent>[], Cursor]> {
  const events: TriggerEvent<CRMObjectChangedEvent>[] = [];
  for (let page = 0; page < 50; page++) {
    const changes = await fetcher.fetchChanges(context, cursor);
    events.push(...changes.changes);
    cursor = changes.next;
    if (!changes.hasMorePages) return [events, cursor];
  }
  throw new Error('change feed did not finish within 50 pages');
}

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try { await promise; } catch (error) { return error; }
  throw new Error('expected a rejection');
}

it('seeds a baseline and delivers later changes once with their kind', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('deals', 'dealname', 'dealstage');
  const existing = fake.create({ dealname: 'Existing renewal', dealstage: 'appointmentscheduled' });
  let cursor = await fetcher.seedCursor(context);
  let events: TriggerEvent<CRMObjectChangedEvent>[];
  [events, cursor] = await drainChanges(fetcher, cursor);
  expect(events, 'records that exist at seed are the baseline').toEqual([]);

  const created = fake.create({ dealname: 'New expansion', dealstage: 'appointmentscheduled' });
  fake.update(existing, { dealstage: 'qualifiedtobuy' });
  const before = cursor;
  [events, cursor] = await drainChanges(fetcher, cursor);
  expect(events.map(event => [event.payload.object.id, event.payload.changeKind])).toEqual([[created, 'created'], [existing, 'updated']]);
  expect(events[1]!.payload.object.properties?.dealstage).toBe('qualifiedtobuy');
  expect(events[1]!.occurredAt.getTime()).toBe(Date.parse(events[1]!.payload.modifiedAt));
  expect(events[1]!.payload.modifiedAt).toBe('2026-10-25T10:00:03Z');

  const [refetched] = await drainChanges(fetcher, before);
  expect(refetched.map(event => event.id), 'event IDs are pure functions of the record version').toEqual(events.map(event => event.id));
  const [again] = await drainChanges(fetcher, cursor);
  expect(again, 'the lag-window re-read skips delivered versions').toEqual([]);
});

it("searches the object type's last-modified property with the requested properties", async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('contacts', 'email');
  fake.create({ email: 'ada@example.com' });
  await drainChanges(fetcher, await fetcher.seedCursor(context));
  const bodies = fake.searchBodies();
  expect(bodies[0]!.sorts).toEqual([{ propertyName: 'lastmodifieddate', direction: 'DESCENDING' }]);
  expect(bodies[1]!.sorts).toEqual([{ propertyName: 'lastmodifieddate', direction: 'ASCENDING' }]);
  expect(bodies[1]!.properties).toEqual(['email', 'createdate', 'lastmodifieddate']);
  expect((bodies[1]!.filterGroups as { filters: { operator: string }[] }[])[0]!.filters[0]!.operator).toBe('GTE');
  expect(bodies[1]!.limit).toBe(100);
});

it('seeds an empty object type at zero and delivers the first record', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('companies');
  const cursor = await fetcher.seedCursor(context);
  const created = fake.create({ name: 'Analytical Engines' });
  const [events] = await drainChanges(fetcher, cursor);
  expect(events.map(event => [event.payload.object.id, event.payload.changeKind])).toEqual([[created, 'created']]);
});

it('delivers a record indexed late within the lag window', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('deals');
  fake.create({ dealname: 'Baseline' });
  let cursor = await fetcher.seedCursor(context);
  const late = fake.createUnindexed({ dealname: 'Indexed late' });
  const prompt = fake.create({ dealname: 'Indexed at once' });
  let events: TriggerEvent<CRMObjectChangedEvent>[];
  [events, cursor] = await drainChanges(fetcher, cursor);
  expect(events.map(event => event.payload.object.id)).toEqual([prompt]);
  fake.index(late);
  [events] = await drainChanges(fetcher, cursor);
  expect(events.map(event => [event.payload.object.id, event.payload.changeKind]), 'a record found after a newer change counts as updated').toEqual([[late, 'updated']]);
});

it('pages by offset through more records than a page at one time and delivers each once', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('contacts');
  const cursor = await fetcher.seedCursor(context);
  const imported = fake.createAtSameTime(250, { lifecyclestage: 'lead' });
  const after = fake.create({ lifecyclestage: 'customer' });
  const [events, next] = await drainChanges(fetcher, cursor);
  const ids = events.map(event => event.payload.object.id);
  expect(new Set(ids).size).toBe(ids.length);
  expect(new Set(ids)).toEqual(new Set([...imported, after]));
  expect((await drainChanges(fetcher, next))[0]).toEqual([]);
});

it('maps rate limits, quotas, and rejected offsets to kit signals', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('deals');
  const cursor = await fetcher.seedCursor(context);
  fake.failNextSearches({ status: 429, headers: { 'Retry-After': '7' }, body: '{"category":"RATE_LIMITS","policyName":"SECONDLY"}' });
  const limited = await rejection(fetcher.fetchChanges(context, cursor));
  expect(limited).toBeInstanceOf(RateLimitedError);
  expect((limited as RateLimitedError).retryAfterMs).toBe(7000);

  fake.failNextSearches({ status: 429, body: '{"category":"RATE_LIMITS","policyName":"DAILY"}' });
  const quota = await rejection(fetcher.fetchChanges(context, cursor));
  expect(quota).toBeInstanceOf(RateLimitedError);
  expect((quota as RateLimitedError).retryAfterMs).toBe(0);

  fake.failNextSearches({ status: 502, body: '{}' });
  const unavailable = await rejection(fetcher.fetchChanges(context, cursor));
  expect(isCursorInvalid(unavailable)).toBe(false);
  expect(unavailable).not.toBeInstanceOf(RateLimitedError);

  const paging = decodeCRMObjectChangeCursor(cursor.token);
  paging.after = '9900';
  fake.failNextSearches({ status: 400, body: '{"category":"VALIDATION_ERROR"}' });
  expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, encodeCRMObjectChangeCursor(paging)))), 'a rejected paging offset reseeds').toBe(true);
  fake.failNextSearches({ status: 400, body: '{"category":"VALIDATION_ERROR"}' });
  expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, cursor))), 'a rejected search without an offset is a failed poll').toBe(false);
});

it('marks unusable cursors invalid', async () => {
  const [fetcher] = await newFakeChangeFetcher('deals');
  for (const token of [
    'not json',
    '{"objectType":"contacts","floorMs":1,"highWaterMs":1,"anchorMs":2}',
    '{"objectType":"deals","floorMs":5,"highWaterMs":4,"anchorMs":6}',
    '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"after":"x"}',
    '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":{"abc":3}}',
    '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"droppedThroughMs":5}',
    '{"objectType":"deals","floorMs":1.5,"highWaterMs":1,"anchorMs":2}',
    '{"objectType":"deals","floorMs":1.0,"highWaterMs":1,"anchorMs":2}',
    '{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":{"7":1e0}}',
  ]) expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, { token }))), token).toBe(true);
});

it("parses last-modified dates with Go's RFC3339Nano rules", () => {
  expect(parseRFC3339Milliseconds('2026-01-02T03:04:05.1234567891239Z')).toBe(1767323045123);
  expect(parseRFC3339Milliseconds('2026-01-02T03:04:05,5Z')).toBe(1767323045500);
  expect(parseRFC3339Milliseconds('2026-01-02T03:04:05+05:60')).toBe(1767301445000);
  expect(parseRFC3339Milliseconds('2026-01-02T03:04:05.999+24:00')).toBe(1767236645999);
  for (const text of ['2026-01-02T03:04:05+05:61', '2026-01-02T03:04:05+25:00', '2026-02-29T03:04:05Z', '2026-01-02T03:04:05.Z', '2026-01-02T03:04:05z']) {
    expect(parseRFC3339Milliseconds(text), text).toBeUndefined();
  }
});

it('prunes seen to the newest versions inside the lag window', () => {
  const position = { objectType: 'deals' as const, floorMs: 0, highWaterMs: 10_000_000, anchorMs: 1, after: '', seen: new Map([['1', 1]]), droppedThroughMs: 0 };
  for (let index = 0; index < maximumSeenRecordVersions + 10; index++) position.seen.set(String(100_000 + index), 10_000_000 - index);
  pruneSeen(position);
  expect(position.seen.size).toBe(maximumSeenRecordVersions);
  expect(position.seen.has('1'), 'a version older than the lag window is dropped').toBe(false);
  expect(position.seen.has('100000')).toBe(true);
  expect(position.seen.has(String(100_000 + maximumSeenRecordVersions)), 'the oldest versions go first').toBe(false);
  expect(position.droppedThroughMs, 'the re-read starts after the dropped versions').toBe(10_000_000 - maximumSeenRecordVersions);
  expect(lagWindowStart(position)).toBe(position.droppedThroughMs + 1);
  expect(Buffer.byteLength(encodeCRMObjectChangeCursor(position).token)).toBeLessThan(64 << 10);
});

it('validates the Trigger configuration', () => {
  expect(() => validateCRMObjectChangedTriggerConfiguration({ objectType: '' as ObjectType })).toThrow('objectType is required');
  expect(() => validateCRMObjectChangedTriggerConfiguration({ objectType: 'tickets' as ObjectType })).toThrow();
  expect(() => validateCRMObjectChangedTriggerConfiguration({ objectType: 'deals', properties: ['bad name'] })).toThrow();
  const tooMany = Array.from({ length: MaximumTriggerProperties + 1 }, (_value, index) => `property_${index}`);
  expect(() => validateCRMObjectChangedTriggerConfiguration({ objectType: 'deals', properties: tooMany })).toThrow();
  expect(() => validateCRMObjectChangedTriggerConfiguration({ objectType: 'deals', properties: ['dealname', 'createdate'] })).not.toThrow();
});

it('does not redeliver a burst larger than the seen capacity on later polls', async () => {
  const [fetcher, fake] = await newFakeChangeFetcher('contacts');
  let cursor = await fetcher.seedCursor(context);
  const imported = fake.createAtSameTime(maximumSeenRecordVersions + 250, { lifecyclestage: 'lead' });
  let events: TriggerEvent<CRMObjectChangedEvent>[];
  [events, cursor] = await drainChanges(fetcher, cursor);
  expect(events).toHaveLength(imported.length);
  for (let poll = 0; poll < 3; poll++) {
    [events, cursor] = await drainChanges(fetcher, cursor);
    expect(events, 'an import larger than the seen set must not repeat on every poll').toEqual([]);
  }
  const later = fake.create({ lifecyclestage: 'customer' });
  [events] = await drainChanges(fetcher, cursor);
  expect(events.map(event => event.payload.object.id)).toEqual([later]);
});
