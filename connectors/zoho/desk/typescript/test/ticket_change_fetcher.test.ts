// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { newTriggerContext } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { ZohoDeskClient } from '../src/client.js';
import type { Ticket } from '../src/generated/models.js';
import { TicketChangeFetcher, nextSearchPosition, ticketChangeEventId, ticketVersionHash } from '../src/ticket_change_fetcher.js';
import { TicketChangedEventSchema } from '../src/ticket_changed.js';
import { fakeCredentialSource } from './support/desk_search_fake.js';

const accessToken = '1000.zohoDeskTriggerAccess0123456789';
const organizationId = '2389290';
const reference = { provider: 'zoho', name: 'zoho-desk-fetcher' };

interface RecordedCursor { token: string; snapshot: [string, string, number][]; hasSnapshot: boolean }
interface RecordedCase {
  name: string; kind: 'seed' | 'fetch'; now: string; departmentId?: string; cursorIn?: RecordedCursor;
  responses: { status: number; date?: string; retryAfter?: string; body: string }[]; requests: string[];
  cursorOut?: RecordedCursor; hasMorePages?: boolean; changes?: { id: string; occurredAt: string; payload: { changeKind: string } & Record<string, unknown> }[];
  error?: 'cursorInvalid' | 'rateLimited' | 'failed'; retryAfterSeconds?: number;
}
/** Recorded from the Go connector's fetcher against desksearchfake and scripted responses. */
const cases = (JSON.parse(readFileSync(new URL('./fixtures/go-ticket-change-cases.json', import.meta.url), 'utf8')) as { cases: RecordedCase[] }).cases;

function newFetcher(recorded: RecordedCase, requests: string[]): TicketChangeFetcher {
  const responses = [...recorded.responses];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    expect(url.pathname).toBe('/api/v1/tickets/search');
    expect(request.headers?.Authorization).toBe(`Zoho-oauthtoken ${accessToken}`);
    expect(request.headers?.orgId).toBe(organizationId);
    requests.push(url.search.slice(1));
    const response = responses.shift();
    if (!response) throw new Error('unexpected request');
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (response.date) headers.date = response.date;
    if (response.retryAfter) headers['retry-after'] = response.retryAfter;
    return { status: response.status, headers, body: Buffer.from(response.body) };
  };
  const client = new ZohoDeskClient({ orgId: organizationId, pollIntervalMs: 15_000 }, fakeCredentialSource(reference, accessToken), {
    apiBaseURL: 'https://desk.example.test/api/v1', send, now: () => new Date(recorded.now),
  });
  return new TicketChangeFetcher(client, reference, { departmentId: recorded.departmentId ?? '' });
}

function recordedCursor(recorded: RecordedCursor | undefined): Cursor {
  if (!recorded) return { token: '' };
  return { token: recorded.token, ...(recorded.hasSnapshot ? { snapshot: { entries: recorded.snapshot.map(([key, hash, seen]) => ({ key, hash, seen })) } } : {}) };
}

function snapshotRows(cursor: Cursor): [string, string, number][] | null {
  return cursor.snapshot ? cursor.snapshot.entries.map(entry => [entry.key, entry.hash, entry.seen]) : null;
}

describe("replays the Go connector's ticketChanged fetches", () => {
  it.each(cases.map(recorded => [recorded.name, recorded] as const))('%s', async (_name, recorded) => {
    const requests: string[] = [];
    const fetcher = newFetcher(recorded, requests);
    const context = newTriggerContext();
    let error: unknown;
    let cursorOut: Cursor | undefined;
    let hasMorePages = false;
    let changes: Awaited<ReturnType<TicketChangeFetcher['fetchChanges']>>['changes'] = [];
    try {
      if (recorded.kind === 'seed') cursorOut = await fetcher.seedCursor(context);
      else {
        const page = await fetcher.fetchChanges(context, recordedCursor(recorded.cursorIn));
        cursorOut = page.next; hasMorePages = page.hasMorePages ?? false; changes = page.changes;
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
      expect(String(error)).not.toContain(accessToken);
    } else expect(error).toBeUndefined();
    if (recorded.cursorOut) {
      expect(cursorOut?.token).toBe(recorded.cursorOut.token);
      expect(snapshotRows(cursorOut!)).toEqual(recorded.cursorOut.hasSnapshot ? recorded.cursorOut.snapshot : null);
    } else expect(cursorOut).toBeUndefined();
    expect(hasMorePages).toBe(recorded.hasMorePages ?? false);
    const expected = recorded.changes ?? [];
    expect(changes.map(change => change.id)).toEqual(expected.map(change => change.id));
    changes.forEach((change, index) => {
      expect(change.occurredAt.getTime()).toBe(Date.parse(expected[index]!.occurredAt));
      expect(change.payload.changeKind).toBe(expected[index]!.payload.changeKind);
      // Payloads of large pages keep only the change kind in the recording.
      if (expected.length <= 3) expect(change.payload).toEqual(TicketChangedEventSchema.parse(expected[index]!.payload));
    });
  });
});

describe('ticket version identity', () => {
  const ticket: Ticket = { id: '1892000000200001', status: 'Open', createdAt: '2026-03-02T09:00:00Z', modifiedAt: '2026-03-02T09:00:00Z' };
  const later: Ticket = { ...ticket, status: 'Closed', modifiedAt: '2026-03-02T09:00:01Z' };

  it('derives the event ID and snapshot hash from the organization, ticket, and modified time only', () => {
    expect(ticketChangeEventId(organizationId, ticket)).toBe(ticketChangeEventId(organizationId, { ...ticket, subject: 'Renamed' }));
    expect(ticketChangeEventId(organizationId, ticket)).toMatch(/^zdesk-[0-9a-f]{32}$/);
    expect(ticketChangeEventId(organizationId, later)).not.toBe(ticketChangeEventId(organizationId, ticket));
    expect(ticketChangeEventId('1', ticket)).not.toBe(ticketChangeEventId(organizationId, ticket));
    expect(ticketVersionHash(later)).not.toBe(ticketVersionHash(ticket));
  });

  it('steps one millisecond past an instant beyond the offset limit, as Go does', () => {
    const instant = Date.UTC(2026, 2, 2, 9);
    const tickets = Array.from({ length: 100 }, () => ({ ...ticket, id: '1' }));
    const [next, hasMorePages] = nextSearchPosition({ sweepStart: instant, floor: instant, from: 900, highWater: instant }, { tickets, received: 100, last: instant, serverTime: undefined });
    expect(hasMorePages).toBe(true);
    expect(next).toEqual({ sweepStart: instant, floor: instant + 1, from: 0, highWater: instant });
  });
});
