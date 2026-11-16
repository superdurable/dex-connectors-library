// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go memberChanged fetcher: each step lists the requests Go sent and the responses it
// received, and the cursor token, snapshot entries, and change IDs must match Go byte for byte.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { MailchimpClient } from '../src/client.js';
import { MemberChangedTriggerDefinition } from '../src/index.js';
import { MemberChangeFetcher } from '../src/member_change_fetcher.js';
import { APIBaseURL, APIKey } from './mailchimp_audience_fake.js';

interface FixtureEntry { key: string; hash: string; seen: number }
interface FixtureStep {
  name: string; seed?: boolean; statuses?: string[]; requests: string[]; responses: Array<{ status: number; retryAfter?: string; body: string }>;
  error?: string; retryAfterSeconds?: number; hasMorePages: boolean; changeCount: number;
  changes: Array<{ id: string; occurredAt: string; payload: string }>;
  cursor?: { token: string; entryCount: number; entries?: FixtureEntry[]; entriesSha256?: string };
}
const fixture = JSON.parse(readFileSync(new URL('./fixtures/member-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { now: string; steps: FixtureStep[] };
const reference = { provider: 'mailchimp', name: 'mailchimp-fetcher-test' };
const audienceId = '57afe96172';

function entriesDigest(cursor: Cursor): string {
  const digest = createHash('sha256');
  for (const entry of cursor.snapshot?.entries ?? []) digest.update(`${entry.key}\t${entry.hash}\t${entry.seen}\n`);
  return digest.digest('hex');
}

function goRFC3339Nano(instant: Date): string { return instant.toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?)0+Z$/, '$1Z'); }

it('matches the Go memberChanged fetcher recording', async () => {
  let pending: Array<[string, FixtureStep['responses'][number]]> = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const next = pending.shift();
    if (!next) throw new Error('TypeScript sent more requests than Go');
    const url = new URL(request.url);
    expect(url.pathname).toBe(`/3.0/lists/${audienceId}/members`);
    expect(url.search.slice(1)).toBe(next[0]);
    const headers: Record<string, string> = { 'content-type': 'application/json', ...(next[1].retryAfter ? { 'retry-after': next[1].retryAfter } : {}) };
    return { status: next[1].status, headers, body: Buffer.from(next[1].body, 'utf8') };
  };
  const now = new Date(fixture.now);
  const client = new MailchimpClient({}, { resolve: async () => ({ apiKey: new SecretString(APIKey) }) }, { apiBaseURL: APIBaseURL, send, now: () => now });
  const context = newTriggerContext();
  let cursor: Cursor = { token: '' };
  let commitCount = 0;
  for (const step of fixture.steps) {
    pending = step.requests.map((query, index) => [query, step.responses[index]!]);
    const fetcher = new MemberChangeFetcher(client, reference, MemberChangedTriggerDefinition.trigger, { audienceId, statuses: step.statuses ?? [] });
    let next: Cursor;
    if (step.seed) next = await fetcher.seedCursor(context);
    else if (step.error) {
      const error = await fetcher.fetchChanges(context, cursor).then(() => undefined, (caught: unknown) => caught);
      expect(error, step.name).toBeInstanceOf(Error);
      if (step.error === 'cursorInvalid') expect(isCursorInvalid(error), step.name).toBe(true);
      else if (step.error === 'rateLimited') {
        expect(error, step.name).toBeInstanceOf(RateLimitedError);
        expect((error as RateLimitedError).retryAfterMs).toBe((step.retryAfterSeconds ?? 0) * 1000);
      } else expect(isCursorInvalid(error) || error instanceof RateLimitedError, step.name).toBe(false);
      expect(String(error)).not.toContain(APIKey);
      expect(pending, step.name).toHaveLength(0);
      continue;
    } else {
      const page = await fetcher.fetchChanges(context, cursor);
      next = page.next;
      expect(page.hasMorePages ?? false, step.name).toBe(step.hasMorePages);
      expect(page.changes, step.name).toHaveLength(step.changeCount);
      const listed = page.changes.length > 10 ? [...page.changes.slice(0, 3), page.changes.at(-1)!] : page.changes;
      expect(listed.map(change => change.id), step.name).toEqual(step.changes.map(change => change.id));
      listed.forEach((change, index) => {
        expect(goRFC3339Nano(change.occurredAt), step.name).toBe(step.changes[index]!.occurredAt);
        expect(change.payload, step.name).toEqual(JSON.parse(step.changes[index]!.payload));
      });
    }
    expect(pending, step.name).toHaveLength(0);
    const expected = step.cursor!;
    expect(next.token, step.name).toBe(expected.token);
    expect(next.snapshot?.entries ?? [], step.name).toHaveLength(expected.entryCount);
    if (expected.entriesSha256) expect(entriesDigest(next), step.name).toBe(expected.entriesSha256);
    else expect((next.snapshot?.entries ?? []).map(entry => ({ key: entry.key, hash: entry.hash, seen: entry.seen })), step.name).toEqual(expected.entries ?? []);
    commitCount += 1;
    cursor = { token: next.token, snapshot: { entries: (next.snapshot?.entries ?? []).map(entry => entry.seen === 0 ? { ...entry, seen: commitCount } : entry) } };
  }
});
