// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore, type Scope, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterAll, afterEach, beforeAll, expect, it } from 'vitest';
import { Client, decodeTaskChangedEventPayload, newConnection, newTaskTriggerRunner, type Connection, type TaskChangedEvent } from '../../src/index.js';
import { ConnectionName, TaskChangedFlow, TaskChangedTriggerBinding, acceptEveryTaskChange, mapToFlowInput, resolveFlowId, type ChangeOutcome } from '../../examples/task-changed/flow/workflow.js';
import { AccessToken, AsanaEventsFake, Endpoint } from '../asana_events_fake.js';

const scope: Scope = { projectId: 'asana-task-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'asana', connectionName: ConnectionName, triggerName: 'taskChanged', bindingName: TaskChangedTriggerBinding };

/** Poll a condition until it holds. */
async function eventually(isMet: () => boolean | Promise<boolean>, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await isMet())) {
    if (Date.now() > deadline) throw new Error('condition not met in time');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

/** A real Dex Worker and Client, the Asana fake, and storage that outlives each Trigger process. */
class TaskChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  readonly delivered = new Map<string, TriggerEvent<TaskChangedEvent>>();
  readonly order: string[] = [];
  readonly connection: Connection;
  readonly flow: TaskChangedFlow;

  constructor(readonly provider: AsanaEventsFake) {
    const client = new Client({ endpoint: Endpoint, pollIntervalMs: 5_000 }, { resolve: async () => ({ accessToken: new SecretString(AccessToken) }) }, { send: provider.send });
    this.connection = newConnection(client, { provider: 'asana', name: ConnectionName });
    this.flow = new TaskChangedFlow(this.connection);
  }

  cursorState() { return newPollCursorStore(this.objects, scope, inboxKey).readPollCursor().then(record => record.state); }

  async waitForSyncToken(syncToken: string): Promise<void> {
    const expected = `{"projectId":"${this.provider.projectId}","sync":"${syncToken}"}`;
    await eventually(async () => (await this.cursorState())?.token === expected);
  }

  /** Run one Trigger process, as a restartable replica, wiring the example's target behind a durable inbox. */
  startTriggerProcess(dex: DexClient): { runner: Runner; stop: () => Promise<void> } {
    const start = newDexFlowTriggerTarget(dex, this.flow, acceptEveryTaskChange, resolveFlowId, mapToFlowInput);
    const counting: TriggerTarget<TaskChangedEvent> = {
      handleTrigger: async (context, event) => {
        await start.handleTrigger(context, event);
        if (!this.delivered.has(event.id)) this.order.push(event.id);
        this.delivered.set(event.id, event);
      },
    };
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, counting, { decodePayload: decodeTaskChangedEventPayload });
    const runner = newTaskTriggerRunner({ connection: this.connection, taskChangedRoutes: [{
      bindingName: TaskChangedTriggerBinding, configuration: { projectId: this.provider.projectId, projectName: 'Facilities' },
      target, cursor: newPollCursorStore(this.objects, scope, inboxKey),
    }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal));
    return { runner, stop: async () => { abort.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
  }

  /** Wait for count deliveries, then return each event's Flow outcome in delivery order. */
  async waitForOutcomes(dex: DexClient, count: number): Promise<ChangeOutcome[]> {
    await eventually(() => this.delivered.size >= count);
    const outcomes: ChangeOutcome[] = [];
    for (const eventId of this.order) {
      const event = this.delivered.get(eventId)!;
      const result = await dex.waitForFlow(resolveFlowId(event), 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const outcome = result.singleOutput<ChangeOutcome>();
      expect(outcome.taskId).toBe(event.payload.task.id);
      outcomes.push(outcome);
    }
    return outcomes;
  }
}

let directory: string;
let cache: BlobCache;
const workers: Worker[] = [];
const clients: DexClient[] = [];

beforeAll(async () => { directory = await mkdtemp(join(tmpdir(), 'dex-asana-task-changed-')); cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 }); });
// Each test stops its Worker, so the next test's Flows run only against that test's provider.
afterEach(async () => {
  for (const client of clients.splice(0)) await client.close();
  for (const worker of workers.splice(0)) await worker.close();
});
afterAll(async () => {
  cache.close();
  await rm(directory, { recursive: true, force: true });
});

/** Start a Worker and Client for a new environment with a unique watched project, so Flow IDs are unique. */
async function newEnvironment(): Promise<{ environment: TaskChangedEnvironment; dex: DexClient }> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev service');
  const environment = new TaskChangedEnvironment(new AsanaEventsFake(String(process.hrtime.bigint())));
  const registry = new Registry([environment.flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  workers.push(worker);
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  const dex = new DexClient(registry, cache, { serverAddress });
  clients.push(dex);
  return { environment, dex };
}

it('starts one Flow per task event across a restart whose first poll is rate limited', async () => {
  const { environment, dex } = await newEnvironment();
  const { provider } = environment;
  provider.addTask('1204000000000001', 'Inspect door sensors');
  provider.addTask('1204000000000002', 'Order spare badges');
  const first = environment.startTriggerProcess(dex);
  await environment.waitForSyncToken(provider.syncToken());
  provider.addTask('1204000000000003', 'Replace badge reader');
  provider.completeTask('1204000000000001');
  provider.addStory('1205000000000001');
  provider.deleteTask('1204000000000002');
  first.runner.wakePoller(TaskChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(dex, 3);
  expect([outcomes[0]!.action, outcomes[0]!.current?.name]).toEqual(['added', 'Replace badge reader']);
  expect([outcomes[1]!.action, outcomes[1]!.changedField, outcomes[1]!.current?.isCompleted]).toEqual(['changed', 'completed', true]);
  expect([outcomes[2]!.action, outcomes[2]!.current]).toEqual(['deleted', undefined]);
  await first.stop();

  provider.addTask('1204000000000004', 'Planned while stopped');
  provider.renameTask('1204000000000003', 'Replace badge reader at door 4');
  provider.rateLimitNext(1, '1');
  const second = environment.startTriggerProcess(dex);
  second.runner.wakePoller(TaskChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(dex, 5);
  expect(outcomes[3]!.current?.name).toBe('Planned while stopped');
  expect([outcomes[4]!.changedField, outcomes[4]!.current?.name]).toEqual(['name', 'Replace badge reader at door 4']);
  await environment.waitForSyncToken(provider.syncToken());
  await second.stop();
  expect(environment.delivered.size, 'no event is delivered twice and the baseline never').toBe(5);
  expect(provider.requests.some(request => request.status === 429), 'the restarted poller met a 429').toBe(true);
  expect(await new TriggerInbox(environment.objects, scope, inboxKey).pending()).toEqual([]);
}, 120_000);

it('records a new baseline after an expired sync token without delivering the gap', async () => {
  const { environment, dex } = await newEnvironment();
  const { provider } = environment;
  provider.addTask('1204000000000001', 'Inspect door sensors');
  const first = environment.startTriggerProcess(dex);
  await environment.waitForSyncToken(provider.syncToken());
  await first.stop();

  provider.addTask('1204000000000009', 'Lost in the gap');
  provider.expireSyncTokens();
  const second = environment.startTriggerProcess(dex);
  await eventually(async () => (await environment.cursorState())?.resetCount === 1);
  await environment.waitForSyncToken(provider.syncToken());
  provider.addTask('1204000000000010', 'After the reset');
  second.runner.wakePoller(TaskChangedTriggerBinding);
  const outcomes = await environment.waitForOutcomes(dex, 1);
  await second.stop();
  expect(outcomes, 'events between the expired token and the new baseline are not delivered').toHaveLength(1);
  expect(outcomes[0]!.current?.name).toBe('After the reset');
}, 120_000);
