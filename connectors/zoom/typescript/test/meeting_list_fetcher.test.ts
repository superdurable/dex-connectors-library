// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// meetingChanged fetcher behavior, ported from the Go connector's meeting_list_fetcher tests.
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { MeetingChangedTriggerConfigurationSchema, MeetingChangedTriggerDefinition, ZoomClient, newProjectConnection, type MeetingChangedTriggerConfiguration } from '../src/index.js';
import { MeetingChangesPerPage, MeetingListFetcher } from '../src/meeting_list_fetcher.js';
import { ListedMeeting } from '../src/meeting_snapshot.js';
import { ZoomMeetingsFake, newFakeClient, newTestProject } from './zoom_meetings_fake.js';

const hostId = 'zoom-host-1';
const reference = { provider: 'zoom', name: 'zoom-watcher' };
const context = newTriggerContext();

function newFetcher(fake: ZoomMeetingsFake, configuration: MeetingChangedTriggerConfiguration = {}, accessToken?: string): MeetingListFetcher {
  return new MeetingListFetcher(newFakeClient(fake, 'zoom-watcher', 30_000, undefined, accessToken), reference, MeetingChangedTriggerDefinition.trigger, configuration);
}

/** Emulate the kit's commit, which stamps new entries with the commit count. */
function committed(cursor: Cursor, commitCount: number): Cursor {
  return { token: cursor.token, snapshot: { entries: (cursor.snapshot?.entries ?? []).map(entry => ({ ...entry, seen: entry.seen || commitCount })) } };
}

describe('MeetingListFetcher', () => {
  it('seeds without backlog, then delivers each difference in meeting ID order', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1); fake.schedule(92674392840, 'Team review', 2); fake.schedule(92674392841, 'Planning', 3);
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    expect(JSON.parse(seed.token)).toEqual({ version: 1, hostId });
    expect(seed.snapshot?.entries).toHaveLength(3);
    expect(Buffer.from(seed.snapshot!.entries[0]!.value!).toString(), 'the snapshot never keeps the join URL').not.toContain('join');
    const unchanged = await fetcher.fetchChanges(context, seed);
    expect(unchanged.changes, 'the baseline is never delivered').toEqual([]);

    fake.schedule(92674392850, 'Kickoff', 4); fake.reschedule(92674392840, 2); fake.delete(92674392841);
    const page = await fetcher.fetchChanges(context, seed);
    expect(page.changes.map(change => [change.payload.meeting.id, change.payload.changeKind])).toEqual([[92674392840, 'updated'], [92674392841, 'removed'], [92674392850, 'scheduled']]);
    const [updated, removed, scheduled] = page.changes;
    expect(updated!.payload.previousMeeting?.startTime).toBe('2036-02-25T17:00:00Z');
    expect(updated!.payload.meeting.startTime).toBe('2036-02-25T19:00:00Z');
    expect(updated!.payload.meeting.joinUrl).toBe('https://us05web.zoom.us/j/92674392840');
    expect(removed!.payload.meeting.topic, 'a removal carries the last listed state').toBe('Planning');
    expect(removed!.payload.meeting.joinUrl).toBeUndefined();
    expect(scheduled!.occurredAt.toISOString(), 'a scheduled meeting occurs when created').toBe('2036-02-23T13:00:00.000Z');
    const refetched = await fetcher.fetchChanges(context, seed);
    expect(refetched.changes.map(change => change.id), 'a refetch keeps the event IDs').toEqual(page.changes.map(change => change.id));
    expect((await fetcher.fetchChanges(context, page.next)).changes).toEqual([]);
  });

  it('gives a recovered meeting a new scheduled ID', async () => {
    const fake = new ZoomMeetingsFake(hostId), fetcher = newFetcher(fake);
    let commits = 1, cursor = committed(await fetcher.seedCursor(context), commits);
    const poll = async () => { const page = await fetcher.fetchChanges(context, cursor); cursor = committed(page.next, ++commits); return page.changes; };
    fake.schedule(92674392836, 'Consultation', 1);
    const scheduled = await poll();
    expect(scheduled).toHaveLength(1);
    expect(await poll()).toEqual([]);
    expect(JSON.parse(cursor.token)).toEqual({ version: 1, hostId, sequence: 1 });
    fake.delete(92674392836);
    expect((await poll())[0]!.payload.changeKind).toBe('removed');
    fake.recover(92674392836);
    const recovered = await poll();
    expect(recovered[0]!.payload).toEqual(scheduled[0]!.payload);
    expect(recovered[0]!.id, 'a recovery is not deduplicated against the first scheduling').not.toBe(scheduled[0]!.id);
  });

  it('keeps the earliest listed start for a repeated meeting ID', () => {
    const listed = (startTime: string | undefined, topic: string) => new ListedMeeting({ id: 92674392836, topic, type: startTime ? 8 : 3, ...(startTime ? { startTime } : {}) });
    const next = listed('2036-03-02T17:00:00Z', 'Weekly sync'), later = listed('2036-03-09T17:00:00Z', 'Weekly sync');
    expect(next.listsBefore(later)).toBe(true);
    expect(later.listsBefore(next)).toBe(false);
    expect(later.listsBefore(listed(undefined, 'Weekly sync')), 'a listed start wins over none').toBe(true);
    const first = listed('2036-03-02T17:00:00Z', 'A'), second = listed('2036-03-02T17:00:00Z', 'B');
    expect(first.listsBefore(second), 'equal starts order by hash').not.toBe(second.listsBefore(first));
    expect(next.listsBefore(listed('2036-03-02T17:00:00.5Z', 'Weekly sync')), 'fractional seconds compare as instants').toBe(true);
  });

  it('skips unconfigured kinds but advances past them', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1);
    const fetcher = newFetcher(fake, { changeKinds: ['removed'] });
    const seed = await fetcher.seedCursor(context);
    fake.rename(92674392836, 'Consultation moved'); fake.schedule(92674392850, 'Kickoff', 4);
    let page = await fetcher.fetchChanges(context, seed);
    expect(page.changes).toEqual([]);
    expect(page.next.snapshot?.entries, 'skipped changes still advance the snapshot').toHaveLength(2);
    fake.delete(92674392850);
    page = await fetcher.fetchChanges(context, page.next);
    expect(page.changes.map(change => [change.payload.meeting.id, change.payload.changeKind])).toEqual([[92674392850, 'removed']]);
  });
});

describe('MeetingListFetcher paging and failures', () => {
  it('reads every page at size 300 and restarts once after a rejected page token', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    for (let index = 0; index < 650; index++) fake.schedule(80_000_000_000 + index, 'Office hours', index % 30);
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    expect(seed.snapshot?.entries).toHaveLength(650);
    expect(fake.listingRequestCount(), "Zoom's largest page size is used").toBe(3);
    expect(fake.requests.every(request => request.query.get('type') === 'scheduled' && request.query.get('page_size') === '300')).toBe(true);
    fake.rejectNextPageTokens(1);
    const page = await fetcher.fetchChanges(context, seed);
    expect(page.changes, 'an expired page token restarts the listing instead of reporting removals').toEqual([]);
    expect(fake.listingRequestCount()).toBe(3 + 2 + 3);
    fake.rejectNextPageTokens(2);
    const failure = await fetcher.fetchChanges(context, seed).catch((error: unknown) => error);
    expect(String(failure)).toContain('page token');
    expect(isCursorInvalid(failure), 'the stored snapshot stays valid').toBe(false);
  });

  it('splits large differences across commits without repeating a change', async () => {
    const fake = new ZoomMeetingsFake(hostId), fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    expect(JSON.parse(seed.token), 'an empty list names no host').toEqual({ version: 1 });
    for (let index = 0; index < 250; index++) fake.schedule(80_000_000_000 + index, 'Office hours', 1);
    const first = await fetcher.fetchChanges(context, seed);
    expect(first.changes).toHaveLength(MeetingChangesPerPage);
    expect(first.hasMorePages).toBe(true);
    expect(first.next.snapshot?.entries, 'the snapshot advances only past delivered changes').toHaveLength(MeetingChangesPerPage);
    expect(JSON.parse(first.next.token)).toEqual({ version: 1, hostId, sequence: 1 });
    const delivered = new Set(first.changes.map(change => change.id));
    let cursor = first.next;
    for (let round = 0; round < 2; round++) {
      const page = await fetcher.fetchChanges(context, cursor);
      for (const change of page.changes) { expect(delivered.has(change.id), 'no change is repeated').toBe(false); delivered.add(change.id); }
      cursor = page.next;
    }
    expect(delivered.size).toBe(250);
  });

  it.each([false, true])('maps every 429 to the kit rate-limit signal (daily %s)', async isDaily => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1);
    const fetcher = newFetcher(fake), seed = await fetcher.seedCursor(context);
    fake.rateLimitNext(1, isDaily, '7');
    const limited = await fetcher.fetchChanges(context, seed).catch((error: unknown) => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7_000);
    fake.rateLimitNext(1, isDaily, '');
    const unhinted = await fetcher.fetchChanges(context, seed).catch((error: unknown) => error);
    expect((unhinted as RateLimitedError).retryAfterMs, 'without Retry-After the runner backs off').toBe(0);
  });

  it('marks undecodable and foreign cursors invalid', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    fake.schedule(92674392836, 'Consultation', 1);
    const fetcher = newFetcher(fake), seed = await fetcher.seedCursor(context);
    for (const cursor of [
      { token: 'sync-1', snapshot: seed.snapshot }, { token: '{"version":2}', snapshot: seed.snapshot }, { token: '{"version":"1"}', snapshot: seed.snapshot },
      { token: seed.token }, { token: '{"version":1,"hostId":"another-host"}', snapshot: seed.snapshot },
    ]) expect(isCursorInvalid(await fetcher.fetchChanges(context, cursor).catch((error: unknown) => error)), cursor.token).toBe(true);
  });

  it('reports a rejected credential without the token', async () => {
    const fake = new ZoomMeetingsFake(hostId);
    const failure = await newFetcher(fake, {}, 'rejected-token').seedCursor(context).catch((error: unknown) => error);
    expect(String(failure)).toContain('HTTP 401');
    expect(String(failure)).not.toContain('rejected-token');
  });

  it('validates change kinds and the poll interval', () => {
    expect(MeetingChangedTriggerConfigurationSchema.safeParse({ changeKinds: ['scheduled', 'removed'] }).success).toBe(true);
    expect(JSON.stringify(MeetingChangedTriggerConfigurationSchema.safeParse({ changeKinds: ['ended'] }).error)).toContain('must be scheduled');
    expect(JSON.stringify(MeetingChangedTriggerConfigurationSchema.safeParse({ changeKinds: ['updated', 'updated'] }).error)).toContain('repeats');
    expect(MeetingChangedTriggerConfigurationSchema.safeParse({ unknown: true }).success).toBe(false);
    const fake = new ZoomMeetingsFake(hostId);
    expect(() => newFakeClient(fake, 'zoom-watcher', 10_000)).toThrow('pollInterval must be at least 30s');
    expect(newFakeClient(fake, 'zoom-watcher', 0).pollIntervalMs).toBe(120_000);
  });

  it('reads the stored pollInterval as Go nanoseconds and rejects a duration string, as Go does', async () => {
    const pollIntervalMs = async (pollInterval: unknown) => {
      const { project } = await newTestProject('zoom-watcher', [], undefined, pollInterval);
      return (newProjectConnection(project, 'zoom-watcher').operations as ZoomClient).pollIntervalMs;
    };
    expect(await pollIntervalMs(60_000_000_000)).toBe(60_000);
    expect(await pollIntervalMs(null)).toBe(120_000);
    await expect(pollIntervalMs('2m')).rejects.toThrow('pollInterval has an invalid type');
    await expect(pollIntervalMs(1.5e10 + 0.5)).rejects.toThrow('pollInterval has an invalid type');
    await expect(pollIntervalMs(10_000_000_000)).rejects.toThrow('pollInterval must be at least 30s');
  });
});
