// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTriggerContext, SecretString, TriggerDeliveryAbortedError, type TriggerEvent, type TriggerLogger } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  CardChangedEventSchema, CardChangedTriggerConfigurationSchema, CardChangedTriggerDefinition, TrelloClient, decodeCardChangedEventPayload,
  defineCardChangedTriggerBinding, newCardChangedTrigger, newCardTriggerRunner, newConnection, type CardChangedEvent,
} from '../src/index.js';
import { APIKey, Token, TrelloBoard } from './trello_board_fake.js';

const reference = { provider: 'trello', name: 'trello-cards' };
/** A payload as the Go connector writes it to the inbox. */
const goPayload = '{"boardId":"6512f0a1c2d3e4f5a6b70001","actionId":"6512f0a1c2d3e4f5a6b74001","actionType":"updateCard","occurredAt":"2026-11-02T09:00:00.123Z","card":{"id":"6512f0a1c2d3e4f5a6b71001","isClosed":false},"changedFields":["closed"]}';

function connectionOn(board: TrelloBoard) {
  const client = new TrelloClient({ pollIntervalMs: 10_000 }, { resolve: async () => ({ apiKey: new SecretString(APIKey), token: new SecretString(Token) }) }, { send: board.send });
  return newConnection(client, reference);
}

async function waitUntil(condition: () => Promise<boolean> | boolean, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error(`condition not met within ${timeoutMs}ms`);
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

describe('cardChanged Trigger wiring', () => {
  it('requires a poll interval of at least ten seconds', () => {
    const credentials = { resolve: async () => ({ apiKey: new SecretString(APIKey), token: new SecretString(Token) }) };
    expect(() => new TrelloClient({ pollIntervalMs: 9_000 }, credentials)).toThrow('pollInterval must be at least 10s');
    expect(new TrelloClient({}, credentials).pollIntervalMs).toBe(30_000);
  });

  it('keeps credentials a failed poll reflects out of Trigger logs', async () => {
    const records: string[] = [];
    const triggerLogger: TriggerLogger = { isEnabled: () => true, log: (level, message, attributes) => { records.push(`${level} ${message} ${JSON.stringify(attributes)}`); } };
    const reflectCredentials = async () => ({ status: 429, headers: {}, body: Buffer.from(JSON.stringify({ error: Token, message: APIKey })) });
    const client = new TrelloClient({ pollIntervalMs: 10_000 }, { resolve: async () => ({ apiKey: new SecretString(APIKey), token: new SecretString(Token) }) }, { send: reflectCredentials, triggerLogger });
    const runner = newCardTriggerRunner({ connection: newConnection(client, reference), cardChangedRoutes: [{ bindingName: 'cards', configuration: { boardId: 'AbCd1234' }, target: { handleTrigger: async () => {} } }] });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    try {
      await waitUntil(() => records.length > 0);
      expect(records.some(record => record.includes('rate limited'))).toBe(true);
      for (const record of records) expect(record.includes(APIKey) || record.includes(Token)).toBe(false);
    } finally {
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });

  it('decodes Go inbox payloads and strict configurations', () => {
    const decoded = decodeCardChangedEventPayload(goPayload, Buffer.from(goPayload));
    expect(decoded.card.isClosed).toBe(false);
    expect(JSON.stringify(CardChangedEventSchema.parse(decoded))).toBe(goPayload);
    expect(() => decodeCardChangedEventPayload('[]', Buffer.from('[]'))).toThrow();
    expect(() => CardChangedTriggerConfigurationSchema.parse({ board: 'AbCd1234' })).toThrow();
    const binding = defineCardChangedTriggerBinding({ connectionName: 'trello-cards', bindingName: 'cards', configurationUI: { units: [] } });
    expect(binding.definition).toEqual(CardChangedTriggerDefinition);
  });

  it('records a durable baseline, then delivers through the inbox', async () => {
    const board = new TrelloBoard();
    board.createCard('Existing card');
    const objects = new InMemoryObjectStore();
    const scope = { projectId: 'trello-test', kind: 'live' as const };
    const key = { connectorId: 'trello', connectionName: reference.name, triggerName: 'cardChanged', bindingName: 'cards' };
    const cursor = newPollCursorStore(objects, scope, key);
    const delivered: TriggerEvent<CardChangedEvent>[] = [];
    const target = newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, { handleTrigger: async (_context, event: TriggerEvent<CardChangedEvent>) => { delivered.push(event); } }, { decodePayload: decodeCardChangedEventPayload });
    const runner = newCardTriggerRunner({ connection: connectionOn(board), cardChangedRoutes: [{ bindingName: 'cards', configuration: { boardId: board.boardId }, target, cursor }] });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    try {
      await waitUntil(async () => (await cursor.readPollCursor()).state !== undefined);
      const created = board.createCard('Created after the baseline');
      runner.wakePoller('cards');
      await waitUntil(() => delivered.length > 0);
      expect(delivered.map(event => event.payload.card.id)).toEqual([created]);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token).boardId).toBe(board.boardId);
    } finally {
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });

  it('runs one binding as a TriggerRunner that baselines, then delivers the next action', async () => {
    const board = new TrelloBoard();
    const existing = board.createCard('Existing card');
    const delivered: TriggerEvent<CardChangedEvent>[] = [];
    const trigger = newCardChangedTrigger({ connection: connectionOn(board), bindingName: 'cards', configuration: { boardId: board.boardId }, target: { handleTrigger: async (_context, event) => { delivered.push(event); } } });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await waitUntil(() => board.requests().length > 0);
      board.commentOnCard(existing, 'Still open.');
      // The single-binding Trigger polls on its own schedule, at least ten seconds apart.
      await waitUntil(() => delivered.length > 0, 30_000);
      expect(delivered[0]!.payload.commentText).toBe('Still open.');
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  }, 40_000);

  it('rejects incomplete routes and configurations', () => {
    const board = new TrelloBoard();
    const connection = connectionOn(board);
    const target = { handleTrigger: async () => undefined };
    expect(() => newCardTriggerRunner({ connection, cardChangedRoutes: [] })).toThrow('routes are required');
    expect(() => newCardTriggerRunner({ connection, cardChangedRoutes: [{ bindingName: ' ', configuration: { boardId: board.boardId }, target }] })).toThrow('binding name and target');
    expect(() => newCardTriggerRunner({ connection, cardChangedRoutes: [{ bindingName: 'cards', configuration: {}, target }] })).toThrow('boardId must be');
    expect(() => newCardChangedTrigger({ connection, bindingName: 'cards', configuration: { boardId: 'x' }, target })).toThrow('boardId must be');
  });
});
