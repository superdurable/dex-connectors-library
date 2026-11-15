// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { afterEach, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, newTriggerContext } from '@superdurable/connectors-sdk';
import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { HubSpotClient } from '../src/client.js';
import { CRMObjectChangeFetcher, crmObjectChangeID } from '../src/crm_object_change_fetcher.js';
import { decodeCRMObjectChangeCursor, encodeCRMObjectChangeCursor, maximumSeenRecordVersions, pruneSeen } from '../src/crm_object_change_cursor.js';
import { CRMObjectChangedEventSchema } from '../src/crm_object_changed.js';
import { formatGoTimeMilliseconds } from '../src/crm_object_change_search.js';
import { FakeAccessToken, HubSpotSearchFake } from './support/hubspot_search_fake.js';

/** Recorded from the Go connector's fetcher over its hubspotsearchfake with the same scenario. */
interface GoParityFixture {
  seed: string;
  pages: { token: string; hasMorePages: boolean; eventIds: string[]; payloads: unknown[]; occurredAt: string[] }[];
  pruned: string;
  eventId: string;
}
const fixture = JSON.parse(readFileSync(new URL('./fixtures/go-crm-object-change-cursors.json', import.meta.url), 'utf8')) as GoParityFixture;
let fake: HubSpotSearchFake | undefined;
afterEach(async () => { await fake?.close(); fake = undefined; });

it("writes the Go connector's cursor documents and event IDs byte for byte", async () => {
  fake = await HubSpotSearchFake.start('deals', new Date(Date.UTC(2026, 9, 25, 10, 0, 0)));
  const reference = { provider: 'hubspot', name: 'hubspot-crm-watcher' };
  const client = new HubSpotClient({ endpoint: fake.url }, new StaticCredentialProvider([[reference, { authMethodId: 'private-app-token', accessToken: new SecretString(FakeAccessToken) }]]));
  const fetcher = new CRMObjectChangeFetcher(client, reference, { objectType: 'deals', properties: ['dealname'] });
  const context = newTriggerContext();
  const existing = fake.create({ dealname: 'Existing' });
  let cursor: Cursor = await fetcher.seedCursor(context);
  expect(cursor.token).toBe(fixture.seed);
  const pages: GoParityFixture['pages'] = [];
  const poll = async (): Promise<void> => {
    for (let page = 0; page < 20; page++) {
      const changes = await fetcher.fetchChanges(context, cursor);
      pages.push({
        token: changes.next.token, hasMorePages: changes.hasMorePages ?? false, eventIds: changes.changes.map(change => change.id),
        payloads: changes.changes.map(change => change.payload), occurredAt: changes.changes.map(change => JSON.stringify(formatGoTimeMilliseconds(change.occurredAt.getTime()))),
      });
      cursor = changes.next;
      if (!changes.hasMorePages) return;
    }
  };
  await poll();
  fake.create({ dealname: 'New' });
  fake.update(existing, { dealname: 'Renamed' });
  await poll();
  fake.createAtSameTime(150, { dealname: 'Imported' });
  await poll();
  await poll();
  expect(pages.map(page => [page.token, page.hasMorePages, page.eventIds])).toEqual(fixture.pages.map(page => [page.token, page.hasMorePages, page.eventIds]));
  for (const [index, expected] of fixture.pages.entries()) {
    if (expected.payloads.length === 0) continue;
    expect(pages[index]!.payloads).toEqual(expected.payloads.map(payload => CRMObjectChangedEventSchema.parse(payload)));
    expect(pages[index]!.occurredAt).toEqual(expected.occurredAt);
  }
});

it('prunes and encodes a full seen set as Go does', () => {
  const position = { objectType: 'deals' as const, floorMs: 0, highWaterMs: 10_000_000, anchorMs: 7, after: '200', seen: new Map([['1', 1], ['999', 9_999_999], ['1000', 9_999_999]]), droppedThroughMs: 0 };
  for (let index = 0; index < maximumSeenRecordVersions + 10; index++) position.seen.set(String(100_000 + index), 10_000_000 - index);
  pruneSeen(position);
  expect(encodeCRMObjectChangeCursor(position).token).toBe(fixture.pruned);
  expect(encodeCRMObjectChangeCursor(decodeCRMObjectChangeCursor(fixture.pruned)).token).toBe(fixture.pruned);
  expect(crmObjectChangeID('contacts', '12345', 1761386401123)).toBe(fixture.eventId);
});
