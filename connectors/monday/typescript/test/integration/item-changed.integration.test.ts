// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runItemChangeWorker } from '../../examples/item-changed/app.js';
import { ConnectionName, ItemChangedTriggerBinding, resolveFlowId, type ItemChangeOutcome } from '../../examples/item-changed/flow.js';
import type { ItemChangedEvent } from '../../src/index.js';
import { MondayBoardFake, newTestProject } from '../monday_board_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'monday', connectionName: ConnectionName, triggerName: 'itemChanged', bindingName: ItemChangedTriggerBinding };

/** One Worker and Trigger process of the example over shared project storage. */
async function startProcess(board: MondayBoardFake, delivered: Map<string, TriggerEvent<ItemChangedEvent>>, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [{ bindingName: ItemChangedTriggerBinding, configuration: { boardId: board.boardId } }], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runItemChangeWorker(project, {
    serverAddress: dexAddress(), mondayOptions: { send: board.send, apiURL: 'http://127.0.0.1:9/v2' }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForWatermark(project: LoadedProject, board: MondayBoardFake, itemId: string): Promise<void> {
  await expect.poll(async () => (await cursorState(project))?.token, { timeout: 60_000 }).toBe(`{"boardId":"${board.boardId}","watermark":"${board.updatedAtText(itemId)}"}`);
}

/** Wait for count deliveries, then each Flow by item ID; a later change replaces an earlier one. */
async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<ItemChangedEvent>>, count: number): Promise<Map<string, ItemChangeOutcome>> {
  await expect.poll(() => delivered.size, { timeout: 90_000 }).toBeGreaterThanOrEqual(count);
  const outcomes = new Map<string, ItemChangeOutcome>();
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<ItemChangeOutcome>();
    expect(outcome.itemId).toBe(event.payload.item.id);
    outcomes.set(outcome.itemId, outcome);
  }
  return outcomes;
}

// Unique board and item IDs keep Flow IDs unique per run against a shared Dex server.
const newBoard = () => new MondayBoardFake(String(1_000_000_000_000 + (Date.now() % 1_000_000_000_000)));
const itemIdOf = (prefix: number, index: number) => String(prefix * 100_000 + index);

it('starts one Flow per item change across a restart with real Dex', async () => {
  const board = newBoard(), delivered = new Map<string, TriggerEvent<ItemChangedEvent>>(), prefix = Date.now() % 1_000_000_000;
  const [existing, secondExisting, created, offline] = [1, 2, 3, 4].map(index => itemIdOf(prefix, index)) as [string, string, string, string];
  board.create(existing, 'Existing request'); board.create(secondExisting, 'Second existing request');
  const first = await startProcess(board, delivered);
  await waitForWatermark(first.project, board, secondExisting);
  board.create(created, 'New request'); board.setStatus(existing, 'Done');
  first.runner.wakePoller(ItemChangedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 2);
  expect(outcomes.get(created)?.changeKind).toBe('created');
  expect(outcomes.get(created)?.current.name).toBe('New request');
  expect(outcomes.get(existing)?.changeKind).toBe('updated');
  expect(outcomes.get(existing)?.current.columnValues?.[0]?.text, 'the Flow reads the item back').toBe('Done');
  await first.stop();

  board.create(offline, 'Filed while stopped'); board.setStatus(created, 'Stuck');
  board.rateLimitNextRequests(1, 1);
  const second = await startProcess(board, delivered, first.objects);
  try {
    outcomes = await waitForOutcomes(second.client, delivered, 4);
    expect(outcomes.get(offline)?.current.name).toBe('Filed while stopped');
    expect(outcomes.get(created)?.changeKind).toBe('updated');
    await waitForWatermark(second.project, board, created);
    second.runner.wakePoller(ItemChangedTriggerBinding);
    // Elapsed time is the behavior: no further delivery arrives in two seconds.
    await new Promise(resolve => setTimeout(resolve, 2_000));
    expect(delivered.size, 'no change twice and never the baseline').toBe(4);
    expect(board.requests.some(request => request.status === 429), 'the restart was rate limited').toBe(true);
  } finally { await second.stop(); }
}, 180_000);

it('records a new baseline after an oversized burst with real Dex', async () => {
  const board = newBoard(), delivered = new Map<string, TriggerEvent<ItemChangedEvent>>(), prefix = Date.now() % 1_000_000_000;
  board.create(itemIdOf(prefix, 1), 'Existing request');
  const first = await startProcess(board, delivered);
  await waitForWatermark(first.project, board, itemIdOf(prefix, 1));
  await first.stop();

  for (let index = 2; index <= 10_002; index++) board.create(itemIdOf(prefix, index), 'Bulk import');
  const second = await startProcess(board, delivered, first.objects);
  try {
    await expect.poll(async () => (await cursorState(second.project))?.resetCount, { timeout: 60_000 }).toBe(1);
    await waitForWatermark(second.project, board, itemIdOf(prefix, 10_002));
    const afterReset = itemIdOf(prefix, 10_003);
    board.create(afterReset, 'After the reset');
    second.runner.wakePoller(ItemChangedTriggerBinding);
    const outcomes = await waitForOutcomes(second.client, delivered, 1);
    expect([...outcomes.keys()], 'items changed in the gap are not delivered').toEqual([afterReset]);
    expect(outcomes.get(afterReset)?.current.name).toBe('After the reset');
  } finally { await second.stop(); }
}, 180_000);
