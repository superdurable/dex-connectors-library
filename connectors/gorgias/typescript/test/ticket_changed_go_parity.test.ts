// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replay the Go ticketChanged fetcher's recorded exchanges: TypeScript must send the same query strings, commit
// the same cursor tokens byte for byte, and deliver the same events, so Go and TypeScript replicas share a cursor.

import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type ChangePage } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { GorgiasClient, TicketEventFetcher, decodeTicketChangedEventPayload, type TicketChangedEvent } from '../src/index.js';
import { parseGoTime } from '../src/go_time.js';

interface RecordedExchange { query: string; status: number; date?: string; retryAfter?: string; apiCallLimit?: string; responseBody: string }
interface RecordedEvent { id: string; occurredAt: string; payload: TicketChangedEvent }
interface RecordedStep {
  call: 'seed' | 'fetch'; cursor?: string; exchanges: RecordedExchange[] | null; token?: string; hasMorePages?: boolean;
  events?: RecordedEvent[] | null; error?: 'cursorInvalid' | 'rateLimited' | 'failed'; retryAfterSeconds?: number;
}
interface RecordedScenario { name: string; eventTypes?: string[] | null; now: string; steps: RecordedStep[] }

const fixture = JSON.parse(readFileSync(new URL('./fixtures/go-ticket-changed-fixtures.json', import.meta.url), 'utf8')) as { email: string; apiKey: string; scenarios: RecordedScenario[] };
const connection = { provider: 'gorgias', name: 'gorgias-ticket-watcher' };

class RecordedProvider {
  exchanges: RecordedExchange[] = [];
  stepName = '';

  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const exchange = this.exchanges.shift();
    if (!exchange) throw new Error(`${this.stepName}: TypeScript sent more requests than Go`);
    const url = new URL(request.url);
    expect(request.method, this.stepName).toBe('GET');
    expect(url.pathname, this.stepName).toBe('/api/events');
    expect(url.search.slice(1), this.stepName).toBe(exchange.query);
    expect(request.headers?.Authorization).toBe(`Basic ${Buffer.from(`${fixture.email}:${fixture.apiKey}`).toString('base64')}`);
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (exchange.date) headers.date = exchange.date;
    if (exchange.retryAfter) headers['retry-after'] = exchange.retryAfter;
    if (exchange.apiCallLimit) headers['x-gorgias-account-api-call-limit'] = exchange.apiCallLimit;
    return { status: exchange.status, headers, body: Buffer.from(exchange.responseBody, 'utf8') };
  };
}

function rateLimitedCause(error: unknown): RateLimitedError | undefined {
  for (let current = error; current; current = (current as { cause?: unknown }).cause) if (current instanceof RateLimitedError) return current;
  return undefined;
}

describe('ticketChanged fetches recorded from the Go connector', () => {
  it.each(fixture.scenarios.map(scenario => [scenario.name, scenario] as const))('%s matches Go', async (_name, scenario) => {
    const provider = new RecordedProvider();
    const now = parseGoTime(scenario.now);
    const credentials = { email: fixture.email, apiKey: new SecretString(fixture.apiKey) };
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credentials }, { send: provider.send, now: () => now.toDate() });
    // Go reads its clock at nanosecond precision; replay the recorded instant exactly.
    const fetcher = new TicketEventFetcher(client, connection, { eventTypes: scenario.eventTypes ?? [] }, () => now);
    for (const [index, step] of scenario.steps.entries()) {
      const name = `${scenario.name} step ${index}`;
      provider.stepName = name;
      provider.exchanges = [...(step.exchanges ?? [])];
      let error: unknown;
      let token = '';
      let page: ChangePage<TriggerEvent<TicketChangedEvent>> | undefined;
      try {
        if (step.call === 'seed') token = (await fetcher.seedCursor(newTriggerContext())).token;
        else { page = await fetcher.fetchChanges(newTriggerContext(), { token: step.cursor ?? '' }); token = page.next.token; }
      } catch (caught) { error = caught; }
      expect(provider.exchanges, `${name}: TypeScript sent fewer requests than Go`).toEqual([]);
      if (step.error) {
        expect(error, name).toBeDefined();
        expect(isCursorInvalid(error), name).toBe(step.error === 'cursorInvalid');
        const rateLimited = rateLimitedCause(error);
        expect(rateLimited !== undefined, name).toBe(step.error === 'rateLimited');
        if (rateLimited) expect(rateLimited.retryAfterMs, name).toBe((step.retryAfterSeconds ?? 0) * 1000);
        continue;
      }
      expect(error, name).toBeUndefined();
      expect(token, name).toBe(step.token);
      if (!page) continue;
      expect(page.hasMorePages ?? false, name).toBe(step.hasMorePages ?? false);
      const expected = step.events ?? [];
      expect(page.changes.map(change => change.id), name).toEqual(expected.map(event => event.id));
      expect(page.changes.map(change => change.payload), name).toEqual(expected.map(event => event.payload));
      expect(page.changes.map(change => change.occurredAt.getTime()), name).toEqual(expected.map(event => parseGoTime(event.occurredAt).toDate().getTime()));
      for (const event of expected) expect(decodeTicketChangedEventPayload(JSON.stringify(event.payload)), name).toEqual(event.payload);
    }
  });
});
