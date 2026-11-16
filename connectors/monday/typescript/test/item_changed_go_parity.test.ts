// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go itemChanged fetcher: each step serves raw GraphQL pages to one fetcher that lives
// across the case's steps, and the cursor token and snapshot entries must match Go byte for byte, so Go and
// TypeScript replicas share one cursor document.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { newTriggerContext } from '@superdurable/connectors-sdk';
import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { ItemChangedTriggerDefinition, type ItemChangedEvent } from '../src/index.js';
import { ItemChangeFetcher } from '../src/item_changed_fetcher.js';
import { newFakeClient } from './monday_board_fake.js';

interface FixtureEntry { key: string; hash: string; seen: number; value: string }
interface FixtureStep {
  name: string; pages: string[]; seed?: boolean; hasMorePages: boolean; changeCount: number;
  requests: Array<{ operation: string; variables: Record<string, unknown> }>;
  changes: Array<{ id: string; occurredAt: string; payload: string }>;
  cursor: { token: string; entryCount: number; entries?: FixtureEntry[]; entriesSha256?: string };
}
const fixture = JSON.parse(readFileSync(new URL('./fixtures/item-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { boardId: string; cases: Array<{ name: string; steps: FixtureStep[] }> };
const reference = { provider: 'monday', name: 'monday-board-watch' };

function entriesOf(cursor: Cursor): FixtureEntry[] {
  return (cursor.snapshot?.entries ?? []).map(entry => ({ key: entry.key, hash: entry.hash, seen: entry.seen, value: Buffer.from(entry.value ?? new Uint8Array()).toString('utf8') }));
}

describe('itemChanged Go parity', () => {
  for (const recordedCase of fixture.cases) {
    it(`matches the Go itemChanged fetcher recording: ${recordedCase.name}`, async () => {
      let step: FixtureStep = recordedCase.steps[0]!;
      let served: Array<{ operation: string; variables: Record<string, unknown> }> = [];
      const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
        const body = JSON.parse(Buffer.from(request.body!).toString('utf8')) as { query: string; variables: Record<string, unknown> };
        served.push({ operation: body.query.split(/\s+/)[1]!.split('(')[0]!, variables: body.variables });
        return { status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(step.pages[served.length - 1]!, 'utf8') };
      };
      const fetcher = new ItemChangeFetcher(newFakeClient(send), reference, ItemChangedTriggerDefinition.trigger, { boardId: fixture.boardId });
      const context = newTriggerContext();
      let cursor: Cursor = { token: '' };
      for (const [index, current] of recordedCase.steps.entries()) {
        step = current; served = [];
        let next: Cursor;
        if (step.seed) next = await fetcher.seedCursor(context);
        else {
          const page = await fetcher.fetchChanges(context, cursor);
          next = page.next;
          expect(page.hasMorePages ?? false, step.name).toBe(step.hasMorePages);
          expect(page.changes, step.name).toHaveLength(step.changeCount);
          const listed = page.changes.length > 10 ? [...page.changes.slice(0, 3), page.changes.at(-1)!] : page.changes;
          expect(listed.map(change => change.id), step.name).toEqual(step.changes.map(change => change.id));
          for (const [position, change] of listed.entries()) {
            const expected = step.changes[position]!;
            expect(change.payload, step.name).toEqual(JSON.parse(expected.payload) as ItemChangedEvent);
            expect(change.occurredAt.toISOString().replace(/\.000Z$/, 'Z'), step.name).toBe(expected.occurredAt);
          }
        }
        expect(served, step.name).toEqual(step.requests);
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
  }
});
