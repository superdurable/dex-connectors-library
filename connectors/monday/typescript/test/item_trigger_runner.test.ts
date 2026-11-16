// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// itemChanged runners, ported from the Go connector's item_trigger_runner tests.
import { readFileSync } from 'node:fs';
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import {
  decodeItemChangedEventPayload, newConnection, newConnectionWithOperations, newItemChangedTrigger, newItemTriggerRunner, newProjectConnection, newProjectItemTriggerRunner,
  type ItemChangedEvent, type MondayClient,
} from '../src/index.js';
import { MondayBoardFake, newFakeClient, newTestProject } from './monday_board_fake.js';

const boardId = '1234567890';
const connectionName = 'monday-board-watch';
const reference = { provider: 'monday', name: connectionName };
const key = { connectorId: 'monday', connectionName, triggerName: 'itemChanged', bindingName: 'boardChanges' };

/** Consumes each event into a list the test awaits. */
class QueueTarget implements TriggerTarget<ItemChangedEvent> {
  readonly events: Array<TriggerEvent<ItemChangedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<ItemChangedEvent>): Promise<void> { this.events.push(event); }
  async nextEvent(index: number): Promise<TriggerEvent<ItemChangedEvent>> {
    await eventually(() => this.events.length > index);
    return this.events[index]!;
  }
}

async function eventually(condition: () => Promise<boolean> | boolean): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

function running(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

describe('itemChanged runners', () => {
  it('rejects incomplete routes and a connection without a MondayClient', () => {
    const connection = newConnection(newFakeClient(new MondayBoardFake(boardId).send), reference), target = new QueueTarget();
    const board = { boardId };
    expect(() => newItemTriggerRunner(connection, [])).toThrow();
    expect(() => newItemTriggerRunner(connection, [{ bindingName: '', configuration: board, target }])).toThrow();
    expect(() => newItemTriggerRunner(connection, [{ bindingName: 'boardChanges', configuration: board, target: undefined as never }])).toThrow();
    expect(() => newItemTriggerRunner(connection, [{ bindingName: 'boardChanges', configuration: undefined as never, target }])).toThrow('boardId');
    expect(() => newItemTriggerRunner(connection, [{ bindingName: 'a', configuration: board, target }, { bindingName: 'a', configuration: board, target }])).toThrow();
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], reference);
    expect(() => newItemTriggerRunner(mock, [{ bindingName: 'boardChanges', configuration: board, target }])).toThrow('MondayClient');
  });

  it('requires a saved board and rejects an invalid saved configuration', async () => {
    const send = new MondayBoardFake(boardId).send;
    const { project: unsaved } = await newTestProject(connectionName, []);
    expect(() => newProjectItemTriggerRunner(unsaved, newProjectConnection(unsaved, connectionName, { send }), [{ bindingName: 'boardChanges', target: new QueueTarget() }])).toThrow('has no saved board');
    const { project: invalid } = await newTestProject(connectionName, [{ bindingName: 'boardChanges', configuration: { boardId, unknown: true } }]);
    expect(() => newProjectItemTriggerRunner(invalid, newProjectConnection(invalid, connectionName, { send }), [{ bindingName: 'boardChanges', target: new QueueTarget() }])).toThrow('boardChanges');
  });

  it('reads the stored pollInterval as Go decodes a duration', async () => {
    const pollIntervalOf = async (configuration: Record<string, unknown>) => {
      const { project } = await newTestProject(connectionName, [], undefined, configuration);
      return (newProjectConnection(project, connectionName, { send: new MondayBoardFake(boardId).send }).operations as MondayClient).pollIntervalMs;
    };
    expect(await pollIntervalOf({ pollInterval: 0 }), 'zero uses the default').toBe(300_000);
    expect(await pollIntervalOf({}), 'omitted uses the default').toBe(300_000);
    await expect(pollIntervalOf({ pollInterval: '2m' }), 'Go rejects a duration string').rejects.toThrow('pollInterval has an invalid type');
    await expect(pollIntervalOf({ pollInterval: 29_000_000_000 })).rejects.toThrow('at least 30s');
  });

  it('loads the saved board and commits after recording', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550001', 'Existing request');
    const { project } = await newTestProject(connectionName, [{ bindingName: 'boardChanges', configuration: { boardId } }]);
    const connection = newProjectConnection(project, connectionName, { send: board.send, apiURL: 'http://127.0.0.1:9/v2' });
    expect((connection.operations as MondayClient).pollIntervalMs, 'the stored Go duration is read').toBe(30_000);
    const target = new QueueTarget();
    const runner = newProjectItemTriggerRunner(project, connection, [{ bindingName: 'boardChanges', target }]);
    const cursor = project.pollCursorStore(key);
    const hasToken = async (itemId: string, entryCount: number) => {
      const state = (await cursor.readPollCursor()).state;
      return state?.token === `{"boardId":"${boardId}","watermark":"${board.updatedAtText(itemId)}"}` && state.snapshot?.entries.length === entryCount;
    };
    const process = running(runner);
    try {
      await eventually(() => hasToken('5550001', 1));
      board.create('5550002', 'New request');
      runner.wakePoller('boardChanges');
      const event = await target.nextEvent(0);
      expect(event.payload.item.id, 'the baseline item is never delivered').toBe('5550002');
      expect(event.payload.changeKind).toBe('created');
      await eventually(() => hasToken('5550002', 2));
      const inbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('records a baseline on start for a single Trigger', async () => {
    const board = new MondayBoardFake(boardId);
    board.create('5550001', 'Existing request');
    const connection = newConnection(newFakeClient(board.send), reference);
    const target = new QueueTarget();
    const trigger = newItemChangedTrigger({ connection, bindingName: 'boardChanges', configuration: { boardId }, target });
    const process = running(trigger);
    try {
      await eventually(() => board.requests.length > 0);
      expect(board.requests[0]!.operation).toBe('ListBoardItems');
    } finally { await process.stop(); }
    expect(() => newItemChangedTrigger({ connection, bindingName: 'boardChanges', configuration: { boardId: '' }, target })).toThrow('boardId');
  });

  it('decodes a Go-written inbox payload', () => {
    const fixture = JSON.parse(readFileSync(new URL('./fixtures/item-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { cases: Array<{ steps: Array<{ changes: Array<{ payload: string }> }> }> };
    const event = decodeItemChangedEventPayload(fixture.cases[0]!.steps[2]!.changes[1]!.payload);
    expect(event.changeKind).toBe('updated');
    expect(event.item.name).toBe('Fix <b>&</b> "quoted"\u2028 caf\u00e9 \u{1f600}');
    expect(event.item.columnValues?.[0]?.value).toEqual({ index: 2, post_id: null });
    expect(() => decodeItemChangedEventPayload('{"boardId":"1","changeKind":"archived","item":{"id":"1","name":""}}')).toThrow();
  });
});
