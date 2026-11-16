// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { runCardChangedApp } from '../../examples/card-changed/app.js';
import { CardChangedTriggerBinding, ConnectionName, resolveFlowId, type CardChangeOutcome } from '../../examples/card-changed/flow/workflow.js';
import { cardChangeEventId } from '../../src/index.js';
import { APIKey, Token, TrelloBoard } from '../trello_board_fake.js';

const scope = { projectId: 'trello-card-changed', kind: 'live' } as const;
const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

/** Project storage that survives a process restart; each call builds a new LoadedProject over it. */
async function newTestProject(objects: InMemoryObjectStore, boardId: string, isFirstStart: boolean): Promise<LoadedProject> {
  if (isFirstStart) {
    await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'trello', connectionName: ConnectionName }, 0, new CredentialMaterial({ api_key: APIKey, token: Token }, undefined, 'apiKey'));
  }
  return new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'trello', connectionName: ConnectionName, modulePath: 'trello', provider: 'trello', configuration: { pollInterval: 10_000_000_000 } }],
    triggerBindings: [{ connectorId: 'trello', connectionName: ConnectionName, triggerName: 'cardChanged', bindingName: CardChangedTriggerBinding, configuration: { boardId } }],
  }, undefined, objects, undefined, undefined);
}

function newRecordingLogger(): TriggerLogger & { records: { message: string; attributes: TriggerLogAttributes }[] } {
  const records: { message: string; attributes: TriggerLogAttributes }[] = [];
  return { records, isEnabled: () => true, log: (_level: TriggerLogLevel, message, attributes) => { records.push({ message, attributes }); } };
}

/** One run of the example application; stopping it models a process exit. */
function startApp(project: LoadedProject, board: TrelloBoard) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveReady!: (ready: { client: Client; wakePoller(): void }) => void;
  const ready = new Promise<{ client: Client; wakePoller(): void }>(resolve => { resolveReady = resolve; });
  const running = runCardChangedApp(project, { serverAddress: dexAddress(), logger, trelloOptions: { send: board.send }, onReady: resolveReady }, stop.signal);
  const seeded = () => logger.records.some(record => record.message === 'poll cursor seeded');
  return { logger, ready, seeded, stop: async () => { stop.abort(); await running; } };
}

/** The Flow ID that one Trello action starts. */
const flowIdOf = (actionId: string) => resolveFlowId({ id: cardChangeEventId(actionId), occurredAt: new Date(), payload: undefined as never });

async function waitForOutcome(client: Client, flowId: string): Promise<CardChangeOutcome> {
  await expect.poll(async () => client.describeFlow(flowId).then(() => true, () => false), { timeout: 30_000, interval: 100 }).toBe(true);
  const result = await client.waitForFlow(flowId, 60_000);
  expect(result.status, result.errorMessage).toBe('completed');
  return result.singleOutput<CardChangeOutcome>();
}

const delivered = (logger: ReturnType<typeof newRecordingLogger>) => logger.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate !== true).length;

it('starts one Flow per card action across a restart and never delivers the baseline', async () => {
  const board = new TrelloBoard();
  const objects = new InMemoryObjectStore();
  const existing = board.createCard('Existing card');
  const baselineAction = board.lastActionId();
  const first = startApp(await newTestProject(objects, board.boardId, true), board);
  let created = '';
  try {
    const { client, wakePoller } = await first.ready;
    await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true);
    created = board.createCard('Replace badge reader');
    const createdAction = board.lastActionId();
    board.moveCard(created);
    const movedAction = board.lastActionId();
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(createdAction))).actionType).toBe('createCard');
    const moved = await waitForOutcome(client, flowIdOf(movedAction));
    expect(moved.current?.listId, 'the Flow reads the card back').toBe(board.cards.get(created)!.idList);
    await expect(client.describeFlow(flowIdOf(baselineAction))).rejects.toThrow();
    expect(delivered(first.logger)).toBe(2);
  } finally { await first.stop(); }

  board.commentOnCard(existing, 'Filed while stopped.');
  const commentAction = board.lastActionId();
  board.deleteCard(existing);
  const deleteAction = board.lastActionId();
  board.renameCard(created, 'Replace badge reader (east)');
  const renameAction = board.lastActionId();
  const second = startApp(await newTestProject(objects, board.boardId, false), board);
  try {
    const { client, wakePoller } = await second.ready;
    wakePoller();
    const commented = await waitForOutcome(client, flowIdOf(commentAction));
    expect(commented.current, 'a card deleted after its action is recorded without a current state').toBeUndefined();
    const deleted = await waitForOutcome(client, flowIdOf(deleteAction));
    expect([deleted.actionType, deleted.current]).toEqual(['deleteCard', undefined]);
    expect((await waitForOutcome(client, flowIdOf(renameAction))).current?.name).toBe('Replace badge reader (east)');
    const requestsBefore = board.requests().length;
    wakePoller();
    await expect.poll(() => board.requests().length, { timeout: 30_000 }).toBeGreaterThan(requestsBefore);
    expect(delivered(second.logger), 'no action is delivered twice').toBe(3);
  } finally { await second.stop(); }
}, 180_000);

it('pauses on a 429, then delivers without losing an action', async () => {
  const board = new TrelloBoard();
  const app = startApp(await newTestProject(new InMemoryObjectStore(), board.boardId, true), board);
  try {
    const { client, wakePoller } = await app.ready;
    await expect.poll(app.seeded, { timeout: 30_000 }).toBe(true);
    board.rateLimitNextActionRequests(1);
    const created = board.createCard('Filed during a rate limit');
    const action = board.lastActionId();
    wakePoller();
    expect((await waitForOutcome(client, flowIdOf(action))).cardId).toBe(created);
    expect(board.requests().map(request => request.status)).toContain(429);
    expect(app.logger.records.some(record => record.message === 'poll rate limited; pausing connection')).toBe(true);
  } finally { await app.stop(); }
}, 120_000);

it('records a new baseline when the binding watches another board', async () => {
  const board = new TrelloBoard();
  const objects = new InMemoryObjectStore();
  const first = startApp(await newTestProject(objects, board.boardId, true), board);
  try { await first.ready; await expect.poll(first.seeded, { timeout: 30_000 }).toBe(true); } finally { await first.stop(); }
  board.boardId = new TrelloBoard().boardId;
  board.createCard('Existing on the new board');
  const gapAction = board.lastActionId();
  const project = await newTestProject(objects, board.boardId, false);
  const second = startApp(project, board);
  try {
    const { client, wakePoller } = await second.ready;
    const cursor = project.pollCursorStore({ connectorId: 'trello', connectionName: ConnectionName, triggerName: 'cardChanged', bindingName: CardChangedTriggerBinding });
    await expect.poll(async () => (await cursor.readPollCursor()).state?.resetCount, { timeout: 30_000 }).toBe(1);
    expect(second.logger.records.some(record => record.message === 'poll cursor invalid; reseeding')).toBe(true);
    const after = board.createCard('After the reset');
    const afterAction = board.lastActionId();
    wakePoller();
    const outcome = await waitForOutcome(client, flowIdOf(afterAction));
    expect([outcome.cardId, outcome.boardId]).toEqual([after, board.boardId]);
    await expect(client.describeFlow(flowIdOf(gapAction)), "the new board's earlier actions are its baseline").rejects.toThrow();
  } finally { await second.stop(); }
}, 120_000);
