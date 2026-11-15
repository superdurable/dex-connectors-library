// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go meetingChanged fetcher: each step serves raw List meetings pages, and the cursor
// token and snapshot entries must match Go byte for byte, so Go and TypeScript replicas share one cursor document.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { newTriggerContext } from '@superdurable/connectors-sdk';
import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { MeetingChangedTriggerDefinition, type MeetingChangedEvent } from '../src/index.js';
import { MeetingListFetcher } from '../src/meeting_list_fetcher.js';
import { newFakeClient } from './zoom_meetings_fake.js';

interface FixtureEntry { key: string; hash: string; seen: number; value: string }
interface FixtureStep {
  name: string; pages: string[]; seed?: boolean; changeKinds?: string[]; hasMorePages: boolean; changeCount: number;
  changes: Array<{ id: string; occurredAt: string; payload: string }> | null;
  cursor: { token: string; entryCount: number; entries?: FixtureEntry[]; entriesSha256?: string };
}
const fixture = JSON.parse(readFileSync(new URL('./fixtures/meeting-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { observedAt: string; steps: FixtureStep[] };
const reference = { provider: 'zoom', name: 'zoom-watcher' };

function entriesOf(cursor: Cursor): FixtureEntry[] {
  return (cursor.snapshot?.entries ?? []).map(entry => ({ key: entry.key, hash: entry.hash, seen: entry.seen, value: Buffer.from(entry.value ?? new Uint8Array()).toString('utf8') }));
}

/** Go's RFC 3339 with trailing fractional zeros removed. */
function goRFC3339Nano(instant: Date): string { return instant.toISOString().replace(/\.?0+Z$/, 'Z').replace(/\.Z$/, 'Z'); }

it('matches the Go meetingChanged fetcher recording byte for byte', async () => {
  let current = 0;
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    expect(url.searchParams.get('type')).toBe('scheduled');
    expect(url.searchParams.get('page_size')).toBe('300');
    const token = url.searchParams.get('next_page_token') ?? '';
    const page = fixture.steps[current]!.pages[token ? Number(token.slice(1)) : 0]!;
    return { status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(page, 'utf8') };
  };
  const client = newFakeClient({ send }, 'zoom-watcher', 30_000, () => new Date(fixture.observedAt));
  const context = newTriggerContext();
  let cursor: Cursor = { token: '' };
  for (const [index, step] of fixture.steps.entries()) {
    current = index;
    const fetcher = new MeetingListFetcher(client, reference, MeetingChangedTriggerDefinition.trigger, step.changeKinds ? { changeKinds: step.changeKinds } : {});
    let next: Cursor;
    if (step.seed) next = await fetcher.seedCursor(context);
    else {
      const page = await fetcher.fetchChanges(context, cursor);
      next = page.next;
      expect(page.hasMorePages ?? false, step.name).toBe(step.hasMorePages);
      expect(page.changes, step.name).toHaveLength(step.changeCount);
      const listed = page.changes.length > 10 ? [...page.changes.slice(0, 3), page.changes.at(-1)!] : page.changes;
      expect(listed.map(change => change.id), step.name).toEqual((step.changes ?? []).map(change => change.id));
      for (const [position, change] of listed.entries()) {
        const expected = step.changes![position]!, goPayload = JSON.parse(expected.payload) as MeetingChangedEvent;
        expect(goRFC3339Nano(change.occurredAt), step.name).toBe(expected.occurredAt);
        expect(change.payload.changeKind).toBe(goPayload.changeKind);
        expect(change.payload.meeting).toEqual(goPayload.meeting);
        expect(change.payload.previousMeeting).toEqual(goPayload.previousMeeting);
      }
    }
    expect(next.token, step.name).toBe(step.cursor.token);
    const entries = entriesOf(next);
    expect(entries, step.name).toHaveLength(step.cursor.entryCount);
    if (step.cursor.entriesSha256) {
      const digest = createHash('sha256');
      for (const entry of entries) digest.update(`${entry.key}\t${entry.hash}\t${entry.seen}\t${entry.value}\n`, 'utf8');
      expect(digest.digest('hex'), step.name).toBe(step.cursor.entriesSha256);
    } else expect(entries, step.name).toEqual(step.cursor.entries ?? []);
    // Emulate the kit's commit, which stamps entries this commit added or changed with its commit count.
    cursor = { token: next.token, snapshot: { entries: (next.snapshot?.entries ?? []).map(entry => ({ ...entry, seen: entry.seen || index + 1 })) } };
  }
});
