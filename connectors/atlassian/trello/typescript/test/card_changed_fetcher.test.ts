// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { decodeCardActionCursor, encodeCardActionCursor, formatGoJSONTime, maximumSeenActionIds, parseGoTime } from '../src/card_action_cursor.js';
import { CardActionFetcher } from '../src/card_action_fetcher.js';
import { CardChangedEventSchema, CardChangedTriggerConfigurationSchema, TrelloClient, type CardChangedEvent } from '../src/index.js';
import { APIKey, MemberID, Token, TrelloBoard } from './trello_board_fake.js';

interface FixtureStep {
  operation: 'seed' | 'fetch'; visibleCount: number; cursor?: string; requests: Record<string, string>[]; next?: string; hasMorePages?: boolean;
  events?: { id: string; occurredAt: string; payload: unknown }[]; nextSha256?: string; eventCount?: number; eventIdsSha256?: string;
}
interface Fixtures {
  boardId: string;
  cursorCases: { token: string; valid: boolean; encoded?: string }[];
  actionCases: { raw: unknown; valid: boolean; event?: { id: string; occurredAt: string; payload: unknown } }[];
  scenarios: { name: string; actions?: Record<string, unknown>[]; bulk?: { count: number; startUnixMilli: number; spacingMilli: number; firstSequence: number }; steps: FixtureStep[] }[];
}
const fixtureText = readFileSync(new URL('./fixtures/go-card-changed-cases.json', import.meta.url), 'utf8');
const fixtures = JSON.parse(fixtureText) as Fixtures;
// Keeps each number literal of the raw actions as Go wrote it, such as 1e2, when the test re-encodes them.
const rawJSON = (JSON as typeof JSON & { rawJSON(text: string): unknown }).rawJSON;
const rawActionTexts = (JSON.parse(fixtureText, (_key, value: unknown, parse?: { source?: string }) =>
  typeof value === 'number' && parse?.source ? rawJSON(parse.source) : value) as Fixtures).actionCases.map(testCase => JSON.stringify([testCase.raw]));
const connection = { provider: 'trello', name: 'trello-cards' };
const context = () => newTriggerContext(new AbortController().signal);
const sha256 = (text: string) => createHash('sha256').update(text, 'utf8').digest('hex');

function fetcherFor(board: TrelloBoard, boardId = board.boardId, credentials = true): CardActionFetcher {
  const client = new TrelloClient({ pollIntervalMs: 10_000 }, {
    resolve: async () => { if (!credentials) throw new Error('missing'); return { apiKey: new SecretString(APIKey), token: new SecretString(Token) }; },
  }, { send: board.send });
  return new CardActionFetcher(client, connection, { boardId });
}

/** Encode an event as the Go fixtures do; TriggerEvent.occurredAt is a Date, so Go's sub-millisecond digits are truncated. */
function eventJSON(event: TriggerEvent<CardChangedEvent>): string {
  const occurredAt = formatGoJSONTime(BigInt(event.occurredAt.getTime()) * 1_000_000n);
  return JSON.stringify({ id: event.id, occurredAt, payload: CardChangedEventSchema.parse(event.payload) });
}

function goEventJSON(event: { id: string; occurredAt: string; payload: unknown }): string {
  const milliseconds = parseGoTime(event.occurredAt)!.unixNanoseconds / 1_000_000n;
  return JSON.stringify({ ...event, occurredAt: formatGoJSONTime(milliseconds * 1_000_000n) });
}

function bulkDocuments(bulk: NonNullable<Fixtures['scenarios'][number]['bulk']>): Record<string, unknown>[] {
  return Array.from({ length: bulk.count }, (_, index) => {
    const milliseconds = bulk.startUnixMilli + index * bulk.spacingMilli;
    return {
      id: Math.floor(milliseconds / 1000).toString(16).padStart(8, '0') + (bulk.firstSequence + index).toString(16).padStart(16, '0'),
      idMemberCreator: MemberID, type: 'createCard', date: new Date(milliseconds).toISOString(), data: { card: { id: '6512f0a1c2d3e4f5a6b73001' } },
    };
  });
}

describe('cardChanged fetch at parity with the Go connector', () => {
  it.each(fixtures.scenarios)('replays $name byte for byte', async scenario => {
    const board = new TrelloBoard(fixtures.boardId);
    const documents = scenario.actions ?? bulkDocuments(scenario.bulk!);
    const fetcher = fetcherFor(board);
    let previous = '';
    for (const [index, step] of scenario.steps.entries()) {
      while (board.actions.length < step.visibleCount) board.addDocument(documents[board.actions.length]!);
      const before = board.requests().length;
      let token: string, events: TriggerEvent<CardChangedEvent>[] = [], hasMorePages = false;
      if (step.operation === 'seed') token = (await fetcher.seedCursor(context())).token;
      else {
        const page = await fetcher.fetchChanges(context(), { token: step.cursor ?? previous });
        token = page.next.token; events = [...page.changes]; hasMorePages = page.hasMorePages ?? false;
      }
      const requests = board.requests().slice(before).map(request => ({ path: request.path, ...request.query }));
      expect(requests, `step ${index} requests`).toEqual(step.requests);
      expect(hasMorePages, `step ${index} hasMorePages`).toBe(step.hasMorePages ?? false);
      if (step.nextSha256) {
        expect(sha256(token), `step ${index} cursor`).toBe(step.nextSha256);
        expect(events.length, `step ${index} event count`).toBe(step.eventCount ?? 0);
        if (events.length) expect(sha256(events.map(event => event.id).join('\n'))).toBe(step.eventIdsSha256);
      } else {
        expect(token, `step ${index} cursor`).toBe(step.next);
        expect(events.map(eventJSON), `step ${index} events`).toEqual((step.events ?? []).map(goEventJSON));
      }
      previous = token;
    }
  });

  it.each(fixtures.cursorCases)('decodes and encodes cursor $token as Go does', ({ token, valid, encoded }) => {
    if (!valid) { expect(() => decodeCardActionCursor(token, fixtures.boardId)).toThrow(TypeError); return; }
    expect(encodeCardActionCursor(decodeCardActionCursor(token, fixtures.boardId)).token).toBe(encoded);
  });

  it.each(fixtures.actionCases.map((testCase, index) => ({ ...testCase, index })))('decodes and validates action case $index as Go does', async ({ valid, event, index }) => {
    const client = new TrelloClient({}, { resolve: async () => ({ apiKey: new SecretString(APIKey), token: new SecretString(Token) }) }, {
      send: async () => ({ status: 200, headers: {}, body: Buffer.from(rawActionTexts[index]!) }),
    });
    const single = new CardActionFetcher(client, connection, { boardId: fixtures.boardId });
    const seed = encodeCardActionCursor({ boardId: fixtures.boardId, watermark: '', seenActionIds: [], floorActionId: '' });
    if (!valid) { await expect(single.fetchChanges(context(), seed)).rejects.toThrow(/Trello actions page/); return; }
    const page = await single.fetchChanges(context(), seed);
    expect(page.changes.map(eventJSON)).toEqual([goEventJSON(event!)]);
  });
});

describe('cardChanged provider failures', () => {
  it('pauses one Trello window for a 429 and names the error code', async () => {
    const board = new TrelloBoard();
    board.createCard('Existing card');
    const fetcher = fetcherFor(board);
    const seed = await fetcher.seedCursor(context());
    board.rateLimitNextActionRequests(1);
    const error = await fetcher.fetchChanges(context(), seed).catch((failure: unknown) => failure);
    expect(error).toBeInstanceOf(RateLimitedError);
    expect((error as RateLimitedError).retryAfterMs).toBe(10_000);
    expect(String((error as Error).message)).toContain('API_TOKEN_LIMIT_EXCEEDED');
  });

  it('reseeds after a rejected since and backs off for a missing board or credentials', async () => {
    const board = new TrelloBoard();
    board.createCard('Existing card');
    const fetcher = fetcherFor(board);
    const seed = await fetcher.seedCursor(context());
    board.rejectNextCursor();
    expect(isCursorInvalid(await fetcher.fetchChanges(context(), seed).catch((failure: unknown) => failure))).toBe(true);
    const missing = await fetcherFor(board, '5b6893f01cb3228998cf629e').seedCursor(context()).catch((failure: unknown) => failure);
    expect(isCursorInvalid(missing)).toBe(false);
    expect(missing).not.toBeInstanceOf(RateLimitedError);
    const requests = board.requests().length;
    await expect(fetcherFor(board, board.boardId, false).seedCursor(context())).rejects.toThrow(/credentials are unavailable/);
    expect(board.requests()).toHaveLength(requests);
  });

  it('delivers each live action once, oldest first, and leaves an empty poll where it was', async () => {
    const board = new TrelloBoard();
    const existing = board.createCard('Existing card');
    const fetcher = fetcherFor(board);
    const seed = await fetcher.seedCursor(context());
    const created = board.createCard('Replace badge reader');
    board.moveCard(created);
    board.commentOnCard(created, 'Ordered the part.');
    board.deleteCard(existing);
    const page = await fetcher.fetchChanges(context(), seed);
    expect(page.changes.map(change => change.payload.actionType)).toEqual(['createCard', 'updateCard', 'commentCard', 'deleteCard']);
    expect(page.changes[1]!.payload.changedFields).toEqual(['idList']);
    expect(page.changes[2]!.payload.commentText).toBe('Ordered the part.');
    const again = await fetcher.fetchChanges(context(), page.next);
    expect(again.changes).toEqual([]);
    expect(again.next).toEqual(page.next);
    expect((JSON.parse(page.next.token) as { seenActionIds?: string[] }).seenActionIds?.length ?? 0).toBeLessThanOrEqual(maximumSeenActionIds);
  });

  it('accepts only a board ID or short link', () => {
    for (const valid of ['5b6893f01cb3228998cf629e', 'AbCd1234']) expect(CardChangedTriggerConfigurationSchema.safeParse({ boardId: valid }).success).toBe(true);
    for (const invalid of ['', ' AbCd1234', 'https://trello.com/b/AbCd1234/support', 'AbCd123']) expect(CardChangedTriggerConfigurationSchema.safeParse({ boardId: invalid }).success).toBe(false);
    expect(CardChangedTriggerConfigurationSchema.safeParse({}).success).toBe(false);
  });
});
