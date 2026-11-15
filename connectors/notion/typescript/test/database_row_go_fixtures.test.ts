// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replay the Go databaseRowChanged fetcher's recorded polls: same requests, cursor bytes, and events.

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { SecretString, newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { NotionClient } from '../src/client.js';
import { SeenRows, databaseRowCursorVersion, encodeDatabaseRowCursor } from '../src/database_row_cursor.js';
import { DatabaseRowFetcher } from '../src/database_row_fetcher.js';
import { DatabaseRowChangedTriggerDefinition, decodeDatabaseRowChangedEventPayload } from '../src/database_row_trigger.js';
import { parseGoTime } from '../src/go_time.js';

interface FixtureExchange { requestBody: unknown; status: number; retryAfter?: string; responseBody: string }
interface FixtureStep {
  call: 'seed' | 'fetch'; cursor?: string; exchanges: FixtureExchange[] | null; token?: string; hasMorePages?: boolean;
  events?: { id: string; occurredAt: string; payload: unknown }[]; error?: string; retryAfterSeconds?: number;
}
interface FixtureScenario { name: string; dataSourceId: string; pageSize: number; steps: FixtureStep[] }
interface SeenRowsFixture { name: string; highWater: string; tokenBytes: number; tokenSha256: string; tokenPrefix: string }

const fixtures = JSON.parse(readFileSync(new URL('./fixtures/go-database-row-changed-fixtures.json', import.meta.url), 'utf8')) as { scenarios: FixtureScenario[]; seenRows: SeenRowsFixture[] };
const token = 'ntn_fake_rows_token';
const connection = { provider: 'notion', name: 'rows' };
const dataSourceId = '2a4d6f80-1b3c-4d5e-8f70-112233445566';

/** Answers each request with the next recorded Go exchange after checking the request body and its key order. */
class RecordedNotion {
  exchanges: FixtureExchange[] = [];
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const exchange = this.exchanges.shift();
    if (!exchange) throw new Error('the TypeScript fetcher sent a request the Go fetcher did not');
    expect(request.headers?.Authorization).toBe(`Bearer ${token}`);
    expect(JSON.stringify(JSON.parse(Buffer.from(request.body!).toString('utf8')))).toBe(JSON.stringify(exchange.requestBody));
    return { status: exchange.status, headers: { 'content-type': 'application/json', ...(exchange.retryAfter ? { 'retry-after': exchange.retryAfter } : {}) }, body: Buffer.from(exchange.responseBody) };
  };
}

describe('the TypeScript fetcher matches the Go recording', () => {
  for (const scenario of fixtures.scenarios) {
    it(scenario.name, async () => {
      const provider = new RecordedNotion();
      const client = new NotionClient({ endpoint: 'https://api.notion.com', pollIntervalMs: 5000, pollPageSize: scenario.pageSize },
        { resolve: async () => ({ apiToken: new SecretString(token) }) }, { send: provider.send });
      const fetcher = new DatabaseRowFetcher(client, connection, DatabaseRowChangedTriggerDefinition.trigger, scenario.dataSourceId);
      const context = newTriggerContext();
      for (const [index, step] of scenario.steps.entries()) {
        provider.exchanges = [...(step.exchanges ?? [])];
        const where = `step ${index} (${step.call})`;
        let outcome: { token: string; hasMorePages: boolean; events: Awaited<ReturnType<typeof fetcher.fetchChanges>>['changes'] };
        try {
          if (step.call === 'seed') outcome = { token: (await fetcher.seedCursor(context)).token, hasMorePages: false, events: [] };
          else {
            const page = await fetcher.fetchChanges(context, { token: step.cursor! });
            outcome = { token: page.next.token, hasMorePages: page.hasMorePages ?? false, events: page.changes };
          }
        } catch (error) {
          expect(step.error, `${where} threw ${String(error)}`).toBeTruthy();
          expect(isCursorInvalid(error), where).toBe(step.error === 'cursorInvalid');
          expect(error instanceof RateLimitedError, where).toBe(step.error === 'rateLimited');
          if (error instanceof RateLimitedError) expect(error.retryAfterMs, where).toBe(step.retryAfterSeconds! * 1000);
          expect(provider.exchanges, `${where} sent fewer requests than Go`).toEqual([]);
          continue;
        }
        expect(step.error, `${where} succeeded where Go failed`).toBeUndefined();
        expect(provider.exchanges, `${where} sent fewer requests than Go`).toEqual([]);
        expect(outcome.token, where).toBe(step.token);
        expect(outcome.hasMorePages, where).toBe(step.hasMorePages ?? false);
        const expected = step.events ?? [];
        expect(outcome.events.map(event => event.id), where).toEqual(expected.map(event => event.id));
        outcome.events.forEach((event, eventIndex) => {
          const go = expected[eventIndex]!;
          expect(event.occurredAt.getTime(), where).toBe(parseGoTime(go.occurredAt).toDate().getTime());
          const payload = JSON.stringify(go.payload);
          expect(decodeDatabaseRowChangedEventPayload(payload, Buffer.from(payload)), where).toEqual(event.payload);
        });
      }
    });
  }
});

/** Rebuild the Go generator's rows, as each fixture's description states. */
function seenRowsInputs(name: string): [string, string][] {
  const base = parseGoTime('2026-11-02T09:00:00Z');
  if (name === 'sharedSeconds') {
    const rows: [string, string][] = Array.from({ length: 2100 }, (_, index) => [base.add(BigInt(index % 90) * 1_000_000_000n).formatRFC3339Nano(), `row-${index}`]);
    rows.push(['2026-11-02T08:55:00Z', 'too-old']);
    return rows;
  }
  return Array.from({ length: 2000 }, (_, index) => [base.add(BigInt(index) * 50_000_000n).formatRFC3339Nano(), `row-${index}`]);
}

describe('seen rows are bounded and encoded as Go encodes them', () => {
  for (const fixture of fixtures.seenRows) {
    it(fixture.name, () => {
      const seen = new SeenRows([]);
      for (const [at, rowId] of seenRowsInputs(fixture.name)) seen.add(parseGoTime(at), rowId);
      const highWater = parseGoTime(fixture.highWater);
      const encoded = encodeDatabaseRowCursor({ version: databaseRowCursorVersion, dataSourceId, highWater, queryFrom: null, startCursor: '', seen: seen.groupsWithin(highWater) });
      expect(encoded.startsWith(fixture.tokenPrefix)).toBe(true);
      expect(Buffer.byteLength(encoded)).toBe(fixture.tokenBytes);
      expect(fixture.tokenBytes).toBeLessThan(64 * 1024);
      expect(createHash('sha256').update(encoded).digest('hex')).toBe(fixture.tokenSha256);
    });
  }
});
