// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { ConfluenceClient } from '../src/client.js';
import { parseGoTime } from '../src/go_time.js';
import { encodeSeenPageVersion, pageChangeEventId, seenPageHash } from '../src/page_change_cursor.js';
import { PageChangeFetcher } from '../src/page_change_fetcher.js';
import { decodePageChangedEventPayload } from '../src/trigger.js';

interface RecordedResponse { status: number; headers?: Record<string, string>; body: string }
interface RecordedCursor { token: string; snapshot: { key: string; hash: string; seen: number; value?: string }[] }
interface RecordedStep {
  action: 'seed' | 'fetch'; now: string; cursor?: RecordedCursor; responses: RecordedResponse[]; requests: { path: string; query: string }[];
  result: { error?: string; retryAfterMs?: number; cursor?: RecordedCursor; hasMore?: boolean; events?: { id: string; occurredAt: string; payload: unknown }[] };
}
interface RecordedScenario { name: string; cloudId: string; spaceKey: string; steps: RecordedStep[] }
interface SeenItem { pageId: string; versionNumber: number; lastModifiedAt: string; hash: string; value: string; eventId: string }
const goCases = JSON.parse(readFileSync(new URL('./fixtures/go-page-change-cases.json', import.meta.url), 'utf8')) as { scenarios: RecordedScenario[]; seenItems: SeenItem[] };
const connection = { provider: 'atlassian', name: 'confluence-trigger-test' };
// The Go recordings use this bearer token; one scripted answer echoes it to test secret reflection.
const accessToken = 'fixture-' + 'access';
const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('client-secret'), accessToken: new SecretString(accessToken), refreshToken: new SecretString('r') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
const encoder = new TextEncoder(), decoder = new TextDecoder();

/** Answers each request with the next recorded response and records the request. */
class ScriptedSite {
  responses: RecordedResponse[] = [];
  requests: { path: string; query: string }[] = [];
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    this.requests.push({ path: url.pathname, query: url.search.slice(1) });
    const next = this.responses.shift();
    if (!next) return { status: 599, headers: {}, body: new Uint8Array() };
    const headers = Object.fromEntries(Object.entries(next.headers ?? {}).map(([name, value]) => [name.toLowerCase(), value]));
    return { status: next.status, headers, body: encoder.encode(next.body) };
  };
}

function toCursor(recorded: RecordedCursor): Cursor {
  return { token: recorded.token, snapshot: { entries: recorded.snapshot.map(entry => ({ key: entry.key, hash: entry.hash, seen: entry.seen, ...(entry.value ? { value: encoder.encode(entry.value) } : {}) })) } };
}
function fromCursor(cursor: Cursor): RecordedCursor {
  return { token: cursor.token, snapshot: (cursor.snapshot?.entries ?? []).map(entry => ({ key: entry.key, hash: entry.hash, seen: entry.seen, ...(entry.value ? { value: decoder.decode(entry.value) } : {}) })) };
}
/** A Date keeps milliseconds, so a Go occurredAt with finer digits is compared at millisecond precision. */
function millisecondTime(text: string): string { return parseGoTime(text)!.toDate().toISOString(); }

async function runStep(site: ScriptedSite, fetcher: PageChangeFetcher, step: RecordedStep): Promise<RecordedStep['result']> {
  site.responses = [...step.responses];
  site.requests = [];
  const context = newTriggerContext();
  try {
    if (step.action === 'seed') return { cursor: fromCursor(await fetcher.seedCursor(context)) };
    const page = await fetcher.fetchChanges(context, toCursor(step.cursor!));
    return {
      cursor: fromCursor(page.next), ...(page.hasMorePages ? { hasMore: true } : {}),
      ...(page.changes.length ? { events: page.changes.map(change => ({ id: change.id, occurredAt: change.occurredAt.toISOString(), payload: change.payload })) } : {}),
    };
  } catch (error) {
    if (error instanceof RateLimitedError) return { error: 'rateLimited', ...(error.retryAfterMs ? { retryAfterMs: error.retryAfterMs } : {}) };
    return { error: isCursorInvalid(error) ? 'cursorInvalid' : 'failed' };
  }
}

describe('pageChanged fetcher replays the Go recordings', () => {
  for (const scenario of goCases.scenarios) {
    it(scenario.name, async () => {
      const site = new ScriptedSite();
      let now = new Date(0);
      const client = new ConfluenceClient({ cloudId: scenario.cloudId, endpoint: 'http://127.0.0.1:12345' }, source, { send: site.send, now: () => now });
      const fetcher = new PageChangeFetcher(client, connection, { spaceKey: scenario.spaceKey });
      for (const [index, step] of scenario.steps.entries()) {
        now = parseGoTime(step.now)!.toDate();
        const result = await runStep(site, fetcher, step);
        const expected = { ...step.result, ...(step.result.events ? { events: step.result.events.map(event => ({ ...event, occurredAt: millisecondTime(event.occurredAt) })) } : {}) };
        expect(site.requests, `step ${index} requests`).toEqual(step.requests);
        expect(result, `step ${index} result`).toEqual(expected);
        for (const event of step.result.events ?? []) expect(decodePageChangedEventPayload('', encoder.encode(JSON.stringify(event.payload)))).toEqual(event.payload);
      }
    });
  }
});

describe('seen versions and event IDs match Go', () => {
  for (const item of goCases.seenItems) {
    it(item.pageId, () => {
      const lastModifiedAt = parseGoTime(item.lastModifiedAt)!.utc();
      expect(seenPageHash(item.pageId, item.versionNumber)).toBe(item.hash);
      expect(decoder.decode(encodeSeenPageVersion({ versionNumber: item.versionNumber, lastModifiedAt }))).toBe(item.value);
      expect(pageChangeEventId('11223344-a1b2-4b33-8c44-def123456789', item.pageId, item.versionNumber)).toBe(item.eventId);
    });
  }
});
