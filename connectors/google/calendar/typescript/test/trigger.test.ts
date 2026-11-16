// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type ChangeFetcher, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, LoadedProject, encodeTriggerEvent } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  CalendarClient, DefaultTriggerCalendarID, EventChangedTriggerConfigurationSchema, decodeEventChangedPayload, newConnection,
  newEventChangedTrigger, newEventTriggerRunner, newProjectConnection, newProjectEventTriggerRunner, watchedCalendarId,
  type Connection, type Event, type EventChangedEvent,
} from '../src/index.js';
import { EventSyncFetcher, encodeEventSyncCursor, newEventChangedTriggerEvent } from '../src/event_sync.js';
import { GeneratedMock } from '../src/mock.js';
import { CalendarSyncFake } from './calendar_sync_fake.js';

const syncCalendarId = 'team@group.calendar.google.com';
const reference = { provider: 'google', name: 'calendar-sync' };
const context = newTriggerContext(new AbortController().signal);

function newClient(fake: CalendarSyncFake, pollIntervalMs = 5_000): CalendarClient {
  return new CalendarClient({ endpoint: 'http://127.0.0.1/calendar/v3', pollIntervalMs }, new StaticCredentialProvider([[reference, { authMethodId: 'google-oauth', accessToken: new SecretString('calendar-token') }]]), { send: fake.send });
}

function newSyncConnection(fake: CalendarSyncFake): Connection { return newConnection(newClient(fake), reference); }

function newFetcher(fake: CalendarSyncFake, calendarId = syncCalendarId): ChangeFetcher<TriggerEvent<EventChangedEvent>> {
  return new EventSyncFetcher(newClient(fake), reference, calendarId);
}

const decodeCursor = (cursor: Cursor) => JSON.parse(cursor.token) as Record<string, string>;
const event = (fields: Partial<Event> & { id: string }): Event => ({ calendarId: syncCalendarId, status: 'confirmed', start: {}, end: {}, isAllDay: false, transparency: 'opaque', ...fields });

describe('eventChanged parity with the Go connector', () => {
  it('encodes cursor documents and derives event IDs byte for byte as Go does', () => {
    // Printed by the Go connector's encodeCursor and newEventChangedTriggerEvent.
    expect(encodeEventSyncCursor({ calendarId: syncCalendarId, syncToken: 'sync-1' }).token).toBe('{"calendarId":"team@group.calendar.google.com","syncToken":"sync-1"}');
    expect(encodeEventSyncCursor({ calendarId: 'a<b>&c é', syncToken: 'CPDA/x+y==', pageToken: 'page-250-260' }).token)
      .toBe('{"calendarId":"a\\u003cb\\u003e\\u0026c\\u2028é","syncToken":"CPDA/x+y==","pageToken":"page-250-260"}');
    const epoch = new Date(0);
    const cases: Array<[Event, string, string, string]> = [
      [event({ id: 'kickoff0001', etag: '"3"', created: '2026-03-02T09:03:00Z', updated: '2026-03-02T09:03:00Z' }), 'gcal-9705eeeae88ed6742e3c44b195356bd6', 'created', '2026-03-02T09:03:00.000Z'],
      [event({ id: 'review00001', etag: '"5"', status: 'cancelled', updated: '2026-03-02T09:05:00Z' }), 'gcal-9131190cb3e47cb91d3442ebe907b851', 'cancelled', '2026-03-02T09:05:00.000Z'],
      [event({ id: 'x', created: '2026-03-02T09:00:00.000Z', updated: '2026-03-02T09:00:01.000000001Z' }), 'gcal-b5a258445a2a42235430d1d1e48f0183', 'updated', '2026-03-02T09:00:01.000Z'],
      [event({ id: 'y', created: '2026-03-02T10:00:00+01:00', updated: '2026-03-02T09:00:00.9Z' }), 'gcal-235fa0124c00f9dbb92cc0d2c3374c9e', 'created', '2026-03-02T09:00:00.900Z'],
    ];
    for (const [input, id, kind, occurredAt] of cases) {
      const change = newEventChangedTriggerEvent(syncCalendarId, input, epoch);
      expect([change.id, change.payload.changeKind, change.occurredAt.toISOString()]).toEqual([id, kind, occurredAt]);
    }
    expect(newEventChangedTriggerEvent(syncCalendarId, event({ id: 'a', status: 'tentative' }), epoch).payload.changeKind).toBe('updated');
  });

  it('replays a change a Go replica recorded and stores one Go decodes', () => {
    // Printed by the Go connector's json.Marshal of an EventChangedEvent.
    const goPayload = '{"calendarId":"team@group.calendar.google.com","changeKind":"created","event":{"id":"kickoff0001","calendarId":"team@group.calendar.google.com","status":"confirmed","summary":"Kickoff","start":{"dateTime":"2026-03-10T10:00:00Z"},"end":{"dateTime":"2026-03-10T11:00:00Z"},"isAllDay":false,"transparency":"opaque","etag":"\\"3\\"","created":"2026-03-02T09:03:00Z","updated":"2026-03-02T09:03:00Z"}}';
    const decoded = decodeEventChangedPayload(goPayload, Buffer.from(goPayload));
    expect([decoded.changeKind, decoded.event.summary, decoded.event.etag]).toEqual(['created', 'Kickoff', '"3"']);
    const stored = JSON.parse(Buffer.from(encodeTriggerEvent(newEventChangedTriggerEvent(syncCalendarId, decoded.event, new Date()))).toString()) as { payload: unknown };
    expect(stored.payload).toEqual(JSON.parse(goPayload));
  });
});

describe('eventChanged sync fetcher', () => {
  it('pages through the full sync baseline and keeps only the sync token', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    for (let index = 0; index < 260; index++) fake.insert(`existing${index}`, 'Existing');
    const cursor = await newFetcher(fake).seedCursor(context);
    expect(decodeCursor(cursor)).toEqual({ calendarId: syncCalendarId, syncToken: 'sync-260' });
    expect(fake.requests().map(request => request.query)).toEqual(['maxResults=250', 'maxResults=250&pageToken=page-250-260']);
  });

  it('reports created, updated, and cancelled events with stable IDs', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    fake.insert('standup0001', 'Standup');
    fake.insert('review00001', 'Review');
    const fetcher = newFetcher(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.insert('kickoff0001', 'Kickoff');
    fake.update('standup0001', 'Standup moved');
    fake.cancel('review00001');
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.hasMorePages).toBeFalsy();
    expect(page.changes.map(change => change.payload.changeKind)).toEqual(['created', 'updated', 'cancelled']);
    expect(page.changes.map(change => change.payload.calendarId)).toEqual([syncCalendarId, syncCalendarId, syncCalendarId]);
    expect([page.changes[0]!.payload.event.summary, page.changes[1]!.payload.event.summary, page.changes[2]!.payload.event.id]).toEqual(['Kickoff', 'Standup moved', 'review00001']);
    expect(page.changes[1]!.occurredAt.toISOString()).toBe('2026-03-02T09:04:00.000Z');
    expect(decodeCursor(page.next)).toEqual({ calendarId: syncCalendarId, syncToken: 'sync-5' });
    expect(fake.requests().at(-1)!.query).toBe('maxResults=250&syncToken=sync-2');
    const refetched = await fetcher.fetchChanges(context, cursor);
    expect(refetched.changes.map(change => change.id)).toEqual(page.changes.map(change => change.id));
    expect(page.changes[0]!.id).not.toBe(page.changes[1]!.id);
  });

  it('repeats the sync token with each page token', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    const fetcher = newFetcher(fake);
    const cursor = await fetcher.seedCursor(context);
    for (let index = 0; index < 260; index++) fake.insert(`burst${index}`, 'Burst');
    const first = await fetcher.fetchChanges(context, cursor);
    expect([first.hasMorePages, first.changes.length]).toEqual([true, 250]);
    expect(first.next.token).toBe('{"calendarId":"team@group.calendar.google.com","syncToken":"sync-0","pageToken":"page-250-260"}');
    const second = await fetcher.fetchChanges(context, first.next);
    expect([second.hasMorePages ?? false, second.changes.length]).toEqual([false, 10]);
    expect(decodeCursor(second.next)).toEqual({ calendarId: syncCalendarId, syncToken: 'sync-260' });
    expect(fake.requests().at(-1)!.query).toBe('maxResults=250&pageToken=page-250-260&syncToken=sync-0');
  });

  it('marks an expired, foreign, or undecodable cursor invalid', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    const fetcher = newFetcher(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.expireSyncTokens();
    expect(isCursorInvalid(await fetcher.fetchChanges(context, cursor).catch(error => error)), '410 Gone requires a new full sync').toBe(true);
    expect(isCursorInvalid(await newFetcher(fake, 'primary').fetchChanges(context, cursor).catch(error => error))).toBe(true);
    for (const token of ['not-json', '[]', '{"calendarId":"team@group.calendar.google.com"}', '{"syncToken":5}']) {
      expect(isCursorInvalid(await fetcher.fetchChanges(context, { token }).catch(error => error)), token).toBe(true);
    }
  });

  it('reports rate limits and other failures without resetting', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    const fetcher = newFetcher(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.rateLimitNextRequests(1, '7');
    const limited = await fetcher.fetchChanges(context, cursor).catch(error => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7_000);
    expect(isCursorInvalid(limited)).toBe(false);
    for (const reason of ['userRateLimitExceeded', 'rateLimitExceeded', 'quotaExceeded', 'dailyLimitExceeded']) {
      fake.throttleNextRequests(1, 403, reason, '');
      const throttled = await fetcher.fetchChanges(context, cursor).catch(error => error);
      expect(throttled, reason).toBeInstanceOf(RateLimitedError);
      expect((throttled as RateLimitedError).retryAfterMs, 'no Retry-After backs off exponentially').toBe(0);
    }
    // Google may send an HTTP date; the operation path rejects it, but a poll still pauses for it.
    fake.rateLimitNextRequests(1, new Date(Date.now() + 3_600_000).toUTCString());
    expect(((await fetcher.fetchChanges(context, cursor).catch(error => error)) as RateLimitedError).retryAfterMs).toBeGreaterThan(30 * 60_000);
    fake.throttleNextRequests(1, 403, 'forbidden', '');
    const forbidden = await fetcher.fetchChanges(context, cursor).catch(error => error);
    expect(forbidden).not.toBeInstanceOf(RateLimitedError);
    expect(isCursorInvalid(forbidden)).toBe(false);
    expect((forbidden as Error).message).toBe('Google Calendar sync request: provider rejected the request with HTTP 403 (forbidden)');
    const missing = await newFetcher(fake, 'deleted@group.calendar.google.com').seedCursor(context).catch(error => error);
    expect(missing).toBeInstanceOf(Error);
    expect(isCursorInvalid(missing), 'a missing calendar is retried, not reseeded').toBe(false);
    expect(String(missing)).not.toContain('calendar-token');
  });

  it('refuses a page without a continuation and unavailable credentials', async () => {
    const empty = new CalendarSyncFake(syncCalendarId);
    const broken = new CalendarClient({ endpoint: 'http://127.0.0.1/calendar/v3' }, new StaticCredentialProvider([[reference, { accessToken: new SecretString('t') }]]), {
      send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"items":[]}') }),
    });
    const fetcher = new EventSyncFetcher(broken, reference, 'primary');
    await expect(fetcher.seedCursor(context)).rejects.toThrow('neither a new page token nor a sync token');
    await expect(fetcher.fetchChanges(context, encodeEventSyncCursor({ calendarId: 'primary', syncToken: 'sync-1' }))).rejects.toThrow('neither a page token nor a sync token');
    const unauthorized = new CalendarClient({ endpoint: 'http://127.0.0.1/calendar/v3' }, new StaticCredentialProvider([]), { send: empty.send });
    const locked = new EventSyncFetcher(unauthorized, reference, syncCalendarId);
    await expect(locked.seedCursor(context)).rejects.toThrow('credentials are unavailable');
  });
});

describe('eventChanged configuration and runners', () => {
  it('defaults to the primary calendar, refuses whitespace, and floors the poll interval', () => {
    expect(watchedCalendarId({})).toBe(DefaultTriggerCalendarID);
    expect(watchedCalendarId({ calendarId: syncCalendarId })).toBe(syncCalendarId);
    expect(EventChangedTriggerConfigurationSchema.safeParse({ calendarId: ' primary' }).success).toBe(false);
    expect(EventChangedTriggerConfigurationSchema.safeParse({ calendarId: 'primary', extra: true }).success).toBe(false);
    expect(() => newClient(new CalendarSyncFake('x'), 1_000)).toThrow('pollInterval must be at least 5s');
    expect(newClient(new CalendarSyncFake('x'), 0).pollIntervalMs).toBe(30_000);
  });

  it('reads pollInterval from the saved connection as a Go duration', () => {
    const project = (configuration: unknown) => ({ configuration: { connections: [{ connectorId: 'google-calendar', connectionName: 'c', provider: 'google', configuration }] }, connections: { scope: { projectId: 'p', kind: 'live' } } }) as unknown as LoadedProject;
    expect((newProjectConnection(project({ pollInterval: 15_000_000_000 }), 'c').operations as CalendarClient).pollIntervalMs).toBe(15_000);
    expect((newProjectConnection(project({ pollInterval: '2m' }), 'c').operations as CalendarClient).pollIntervalMs).toBe(120_000);
    expect(() => newProjectConnection(project({ pollInterval: '1s' }), 'c')).toThrow('at least 5s');
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newSyncConnection(new CalendarSyncFake(syncCalendarId));
    const target = { handleTrigger: async () => undefined };
    for (const routes of [[], [{ bindingName: '', configuration: {}, target }], [{ bindingName: 'calendarChanges', configuration: {}, target: undefined as never }],
      [{ bindingName: 'calendarChanges', configuration: {}, target }, { bindingName: 'calendarChanges', configuration: {}, target }],
      [{ bindingName: 'calendarChanges', configuration: { calendarId: 'primary ' }, target }]]) {
      expect(() => newEventTriggerRunner(connection, routes)).toThrow();
    }
    expect(() => newEventTriggerRunner(new GeneratedMock('calendar-sync').connection(), [{ bindingName: 'calendarChanges', configuration: {}, target }])).toThrow('mock connection');
  });

  it('loads the saved calendar and commits the sync token after recording each change', async () => {
    const fake = new CalendarSyncFake(syncCalendarId);
    fake.insert('existing001', 'Existing');
    const objects = new InMemoryObjectStore();
    const project = new LoadedProject({
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope: { projectId: 'calendar-trigger-test', kind: 'live' }, revision: 1, operationConfigurations: [],
      connections: [{ connectorId: 'google-calendar', connectionName: 'calendar-sync', modulePath: 'google-calendar', provider: 'google', configuration: {} }],
      triggerBindings: [{ connectorId: 'google-calendar', connectionName: 'calendar-sync', triggerName: 'eventChanged', bindingName: 'teamChanges', configuration: { calendarId: syncCalendarId, calendarName: 'Ops Team' } }],
    }, undefined, objects, undefined, undefined);
    const delivered: Array<TriggerEvent<EventChangedEvent>> = [];
    const runner = newProjectEventTriggerRunner(project, newSyncConnection(fake), [{ bindingName: 'teamChanges', target: { handleTrigger: async (_context, change) => { delivered.push(change); } } }]);
    const key = { connectorId: 'google-calendar', connectionName: 'calendar-sync', triggerName: 'eventChanged', bindingName: 'teamChanges' };
    const committedToken = async () => (await project.pollCursorStore(key).readPollCursor()).state?.token ?? '';
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal)).catch(error => error);
    try {
      await expect.poll(committedToken, { timeout: 10_000 }).toBe('{"calendarId":"team@group.calendar.google.com","syncToken":"sync-1"}');
      fake.insert('kickoff0001', 'Kickoff');
      runner.wakePoller('teamChanges');
      await expect.poll(() => delivered.length, { timeout: 10_000 }).toBe(1);
      expect([delivered[0]!.payload.event.id, delivered[0]!.payload.changeKind], 'the baseline event is never delivered').toEqual(['kickoff0001', 'created']);
      await expect.poll(committedToken, { timeout: 10_000 }).toBe('{"calendarId":"team@group.calendar.google.com","syncToken":"sync-2"}');
      await expect.poll(async () => (await project.triggerInbox(key).pending()).length, { timeout: 10_000 }).toBe(0);
    } finally { stop.abort(); await running; }
  });

  it('runs one binding with a memory cursor, baselining and delivering the next change', async () => {
    const fake = new CalendarSyncFake('primary');
    fake.insert('existing001', 'Existing');
    const delivered: Array<TriggerEvent<EventChangedEvent>> = [];
    const trigger = newEventChangedTrigger({
      connection: newSyncConnection(fake), connectionName: 'calendar-sync', bindingName: 'primaryChanges', configuration: {},
      target: { handleTrigger: async (_context, change) => { delivered.push(change); } },
    });
    expect(() => newEventChangedTrigger({ connection: newSyncConnection(fake), connectionName: 'other', bindingName: 'b', configuration: {}, target: { handleTrigger: async () => undefined } })).toThrow('must equal');
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch(error => error);
    try {
      await expect.poll(() => fake.requests().length, { timeout: 10_000 }).toBeGreaterThan(0);
      fake.cancel('existing001');
      // The next poll comes one jittered 5s interval later.
      await expect.poll(() => delivered.length, { timeout: 20_000, interval: 100 }).toBe(1);
      expect([delivered[0]!.payload.changeKind, delivered[0]!.payload.calendarId]).toEqual(['cancelled', 'primary']);
    } finally { stop.abort(); await running; }
  }, 30_000);
});
