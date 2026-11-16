// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { TicketExportFetcher, ZendeskClient, decodeTicketChangedEventPayload, type TicketChangedEvent } from '../src/index.js';

const apiToken = 'fixtureZendeskToken0123';
const connection = { provider: 'zendesk', name: 'zendesk-ticket-watcher' };

interface RecordedResponse { status: number; retryAfter?: string; body: string }
interface RecordedChange { id: string; occurredAt: string; payload: string }
interface RecordedCase {
  name: string; kind: 'seed' | 'fetch'; now: string; includeDeleted?: boolean; maxResponseBytes?: number; cursorIn?: string;
  responses: RecordedResponse[] | null; requests: string[] | null; cursorOut?: string; hasMorePages?: boolean; changes?: RecordedChange[];
  error?: 'cursorInvalid' | 'rateLimited' | 'failed'; errorContains?: string; retryAfterSeconds?: number;
}

const cases = (JSON.parse(readFileSync(new URL('./fixtures/go-ticket-export-cases.json', import.meta.url), 'utf8')) as { cases: RecordedCase[] }).cases;

function cannedClient(recorded: RecordedCase, requests: string[]): ZendeskClient {
  const responses = [...(recorded.responses ?? [])];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    expect(url.pathname).toBe('/api/v2/incremental/tickets/cursor');
    requests.push(url.search.slice(1));
    const response = responses.shift();
    if (!response) throw new Error('unexpected request');
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (response.retryAfter) headers['retry-after'] = response.retryAfter;
    const body = Buffer.from(response.body, 'utf8');
    if (body.length > request.maxResponseBytes) throw new BodyTooLargeError(response.status, headers);
    return { status: response.status, headers, body };
  };
  const now = new Date(recorded.now);
  return new ZendeskClient({ subdomain: 'acme', ...(recorded.maxResponseBytes ? { maxResponseBytes: recorded.maxResponseBytes } : {}) },
    { resolve: async () => ({ email: 'admin@acme.example.com', apiToken: new SecretString(apiToken) }) },
    { apiBaseURL: 'https://acme.zendesk.com/api/v2', send, now: () => now, incrementalExportRequestsPerMinute: 600 });
}

describe('ticketChanged export fetches recorded from the Go connector', () => {
  it.each(cases.map(recorded => [recorded.name, recorded] as const))('%s matches Go', async (_name, recorded) => {
    const requests: string[] = [];
    const fetcher = new TicketExportFetcher(cannedClient(recorded, requests), connection, { includeDeleted: recorded.includeDeleted ?? false });
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
    expect(requests).toEqual(recorded.requests ?? []);
    if (recorded.error === 'cursorInvalid') expect(isCursorInvalid(error)).toBe(true);
    else if (recorded.error === 'rateLimited') {
      expect(error).toBeInstanceOf(RateLimitedError);
      expect((error as RateLimitedError).retryAfterMs).toBe((recorded.retryAfterSeconds ?? 0) * 1000);
    } else if (recorded.error === 'failed') {
      expect(error).toBeInstanceOf(Error);
      expect(isCursorInvalid(error) || error instanceof RateLimitedError).toBe(false);
      expect(String(error)).not.toContain(apiToken);
      expect(String(error)).toContain(recorded.errorContains ?? '');
    } else expect(error).toBeUndefined();
    expect(cursorOut).toBe(recorded.cursorOut ?? '');
    expect(hasMorePages).toBe(recorded.hasMorePages ?? false);
    const expected = recorded.changes ?? [];
    expect(changes.map(change => change.id)).toEqual(expected.map(change => change.id));
    changes.forEach((change, index) => {
      const want = expected[index]!;
      expect(change.occurredAt.getTime()).toBe(Date.parse(want.occurredAt));
      // The SDK stores object keys in Go map order and Go in struct order; the values are equal.
      expect(JSON.parse(encodeGoJSONValue(change.payload))).toEqual(JSON.parse(want.payload));
      expect(decodeTicketChangedEventPayload(want.payload)).toEqual(change.payload);
    });
  });
});
