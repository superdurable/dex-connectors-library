// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { FreshdeskClient, TicketChangeFetcher, decodeTicketChangedEventPayload, type TicketChangeKind, type TicketChangedEvent } from '../src/index.js';
import { pruneDeliveredTicketKeys } from '../src/ticket_change_fetcher.js';

const apiKey = 'freshdeskTriggerKey0123456789';
const connection = { provider: 'freshdesk', name: 'freshdesk-trigger' };

interface RecordedResponse { status: number; date?: string; link?: boolean; retryAfter?: string; body: unknown }
interface RecordedChange { id: string; occurredAt: string; payload: { changeKind: string; ticket?: Record<string, unknown> } }
interface RecordedCase {
  name: string; kind: 'seed' | 'fetch'; now?: string; changeKinds?: TicketChangeKind[]; cursorIn?: string; responses: RecordedResponse[]; requests: string[];
  cursorOut?: string; hasMorePages?: boolean; changes?: RecordedChange[]; error?: 'cursorInvalid' | 'rateLimited' | 'failed'; retryAfterSeconds?: number;
}

const cases = (JSON.parse(readFileSync(new URL('./fixtures/go-ticket-change-cases.json', import.meta.url), 'utf8')) as { cases: RecordedCase[] }).cases;

function cannedClient(recorded: RecordedCase, requests: string[]): FreshdeskClient {
  const responses = [...recorded.responses];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    requests.push(new URL(request.url).search.slice(1));
    const response = responses.shift();
    if (!response) throw new Error('unexpected request');
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (response.date) headers.date = response.date;
    if (response.link) headers.link = '<https://acme.freshdesk.com/api/v2/tickets?page=2>; rel="next"';
    if (response.retryAfter) headers['retry-after'] = response.retryAfter;
    return { status: response.status, headers, body: Buffer.from(JSON.stringify(response.body)) };
  };
  const now = recorded.now ? new Date(recorded.now) : undefined;
  return new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString(apiKey) }) }, { send, ...(now ? { now: () => now } : {}) });
}

// Go encodes times with the fewest fraction digits; compare instants, and every other field exactly.
function comparableTicket(ticket: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(ticket).map(([name, value]) => [name, typeof value === 'string' && /At$|By$/.test(name) ? Date.parse(value) : value]));
}

describe('ticketChanged fetches recorded from the Go connector', () => {
  it.each(cases.map(recorded => [recorded.name, recorded] as const))('%s matches Go', async (_name, recorded) => {
    const requests: string[] = [];
    const fetcher = new TicketChangeFetcher(cannedClient(recorded, requests), connection, { changeKinds: recorded.changeKinds ?? [] });
    let error: unknown;
    let cursorOut = '';
    let hasMorePages = false;
    let changes: TriggerEvent<TicketChangedEvent>[] = [];
    try {
      if (recorded.kind === 'seed') cursorOut = (await fetcher.seedCursor(newTriggerContext())).token;
      else {
        const page = await fetcher.fetchChanges(newTriggerContext(), { token: recorded.cursorIn ?? '' });
        cursorOut = page.next.token; hasMorePages = page.hasMorePages ?? false; changes = page.changes;
      }
    } catch (caught) { error = caught; }
    expect(requests).toEqual(recorded.requests);
    if (recorded.error === 'cursorInvalid') expect(isCursorInvalid(error)).toBe(true);
    else if (recorded.error === 'rateLimited') {
      expect(error).toBeInstanceOf(RateLimitedError);
      expect((error as RateLimitedError).retryAfterMs).toBe((recorded.retryAfterSeconds ?? 0) * 1000);
    } else if (recorded.error === 'failed') {
      expect(error).toBeInstanceOf(Error);
      expect(isCursorInvalid(error) || error instanceof RateLimitedError).toBe(false);
      expect(String(error)).not.toContain(apiKey);
    } else expect(error).toBeUndefined();
    expect(cursorOut).toBe(recorded.cursorOut ?? '');
    expect(Buffer.byteLength(cursorOut)).toBeLessThan(64 << 10);
    expect(hasMorePages).toBe(recorded.hasMorePages ?? false);
    const expected = recorded.changes ?? [];
    expect(changes.map(change => change.id)).toEqual(expected.map(change => change.id));
    changes.forEach((change, index) => {
      const want = expected[index]!;
      expect(change.occurredAt.getTime()).toBe(Date.parse(want.occurredAt));
      expect(change.payload.changeKind).toBe(want.payload.changeKind);
      if (expected.length > 3) return;
      expect(comparableTicket(change.payload.ticket as Record<string, unknown>)).toEqual(comparableTicket(want.payload.ticket!));
      const replayed = decodeTicketChangedEventPayload(JSON.stringify(want.payload));
      expect(comparableTicket(replayed.ticket as Record<string, unknown>)).toEqual(comparableTicket(change.payload.ticket as Record<string, unknown>));
    });
  });
});

describe('pruneDeliveredTicketKeys', () => {
  it('keeps the newest keys inside the window and skips a partly dropped second', () => {
    const { pruned, floorMs } = pruneDeliveredTicketKeys(['1@999', '2@1000', '3@1001', '3@1001', 'bad', '4@x', '5@ 1002'], 1_000_000);
    expect(pruned).toEqual(['2@1000', '3@1001']);
    expect(floorMs).toBe(Number.NEGATIVE_INFINITY);
    const many = Array.from({ length: 1510 }, (_, index) => `${index}@${2000 + index}`);
    const overflow = pruneDeliveredTicketKeys(many, 1_000_000);
    expect(overflow.pruned).toHaveLength(1500);
    expect(overflow.pruned.at(-1)).toBe(many.at(-1));
    expect(overflow.floorMs).toBe(2_010_000);
    const sameSecond = ['1@3000', '2@3000', ...Array.from({ length: 1500 }, (_, index) => `${10 + index}@${3000 + (index % 2)}`)];
    const skipped = pruneDeliveredTicketKeys(sameSecond, 1_000_000);
    expect(skipped.floorMs).toBe(3_001_000);
    expect(skipped.pruned.every(key => key.endsWith('@3001'))).toBe(true);
  });
});
