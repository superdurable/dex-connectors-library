// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { expect, it } from 'vitest';
import { RecordModifiedEventSchema } from '../src/record_modified.js';
import { context, newPollTestFetcher } from './support/poll_test_fetcher.js';

interface RecordedEvent { id: string; occurredAt: string; payload: unknown }
interface RecordedPage { cursor: string; events: RecordedEvent[]; next: string; hasMorePages: boolean }
const fixture = JSON.parse(readFileSync(new URL('./fixtures/go-record-poll-cursors.json', import.meta.url), 'utf8')) as { seed: string; pages: RecordedPage[] };

it('replays a Go fetcher scenario: cursor tokens and event IDs byte for byte, payloads by value', async () => {
  const [fetcher, org, clock] = newPollTestFetcher(2, { sObjectType: 'Opportunity', fieldNames: ['Name', 'StageName'] });
  const existing = org.insert('Opportunity', { Name: 'Renewal', StageName: 'Prospecting' });
  expect((await fetcher.seedCursor(context)).token).toBe(fixture.seed);
  org.insert('Opportunity', { Name: 'Expansion', StageName: 'Qualification' });
  org.update(existing, { StageName: 'Closed Won' });
  org.insertBatch('Opportunity', 3, () => ({ Name: 'Bulk' }));
  clock.advance(1000);
  for (const recorded of fixture.pages) {
    const page = await fetcher.fetchChanges(context, { token: recorded.cursor });
    expect(page.next.token).toBe(recorded.next);
    expect(page.hasMorePages ?? false).toBe(recorded.hasMorePages);
    expect(page.changes).toHaveLength(recorded.events.length);
    page.changes.forEach((change, index) => {
      const expected = recorded.events[index]!;
      expect(change.id).toBe(expected.id);
      expect(change.occurredAt.toISOString().replace('.000Z', 'Z')).toBe(expected.occurredAt);
      // Go sorts the record's field map, so the payload matches by value rather than by byte.
      expect(JSON.parse(JSON.stringify(change.payload))).toEqual(expected.payload);
      expect(RecordModifiedEventSchema.parse(expected.payload)).toEqual(JSON.parse(JSON.stringify(change.payload)));
    });
  }
});
