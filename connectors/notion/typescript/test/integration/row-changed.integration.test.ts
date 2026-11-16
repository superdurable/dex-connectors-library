// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore, type Scope, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterAll, afterEach, beforeAll, expect, it } from 'vitest';
import { NotionClient, decodeDatabaseRowChangedEventPayload, newConnection, newDatabaseRowTriggerRunner, type Connection, type DatabaseRowChangedEvent } from '../../src/index.js';
import { parseGoTime } from '../../src/go_time.js';
import { ConnectionName, DatabaseRowChangeFlow, DatabaseRowChangedTriggerBinding, acceptEveryRowChange, mapToFlowInput, resolveFlowId, type ChangeOutcome } from '../../examples/row-changed/flow/workflow.js';
import { Endpoint, NotionRowsFake, Token } from '../notion_rows_fake.js';

const scope: Scope = { projectId: 'notion-row-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'notion', connectionName: ConnectionName, triggerName: 'databaseRowChanged', bindingName: DatabaseRowChangedTriggerBinding };
const rowStandup = '2a4d6f80-1b3c-4d5e-8f70-0000000000a1';
const rowReview = '2a4d6f80-1b3c-4d5e-8f70-0000000000b2';
const rowKickoff = '2a4d6f80-1b3c-4d5e-8f70-0000000000c3';
const rowOffline = '2a4d6f80-1b3c-4d5e-8f70-0000000000d4';

/** Poll a condition until it holds. */
async function eventually(isMet: () => boolean | Promise<boolean>, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await isMet())) {
    if (Date.now() > deadline) throw new Error('condition not met in time');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

/** A real Dex Worker and Client, the Notion fake, and storage that outlives each Trigger process. */
class RowChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  readonly delivered = new Map<string, TriggerEvent<DatabaseRowChangedEvent>>();
  readonly order: string[] = [];
  readonly connection: Connection;
  readonly flow: DatabaseRowChangeFlow;
  readonly now = new Date(Math.floor(Date.now() / 60_000) * 60_000);

  constructor(readonly provider: NotionRowsFake) {
    const client = new NotionClient({ endpoint: Endpoint, pollIntervalMs: 5_000 }, { resolve: async () => ({ apiToken: new SecretString(Token) }) }, { send: provider.send });
    this.connection = newConnection(client, { provider: 'notion', name: ConnectionName });
    this.flow = new DatabaseRowChangeFlow(this.connection);
  }

  minutesFromNow(minutes: number): Date { return new Date(this.now.getTime() + minutes * 60_000); }

  cursorStore() { return newPollCursorStore(this.objects, scope, inboxKey); }
  cursorState() { return this.cursorStore().readPollCursor().then(record => record.state); }

  /** Wait until the committed cursor watches this data source up to highWater with no open query. */
  async waitForCommittedHighWater(highWater: Date): Promise<void> {
    await eventually(async () => {
      const state = await this.cursorState();
      if (!state) return false;
      const position = JSON.parse(state.token) as { dataSourceId?: string; highWater: string; startCursor?: string };
      return position.dataSourceId === this.provider.dataSourceId && parseGoTime(position.highWater).toDate().getTime() === highWater.getTime() && !position.startCursor;
    });
  }

  /** Run one Trigger process, as a restartable replica, wiring the example's target behind a durable inbox. */
  startTriggerProcess(dex: DexClient): { runner: Runner; stop: () => Promise<void> } {
    const start = newDexFlowTriggerTarget(dex, this.flow, acceptEveryRowChange, resolveFlowId, mapToFlowInput);
    const counting: TriggerTarget<DatabaseRowChangedEvent> = {
      handleTrigger: async (context, event) => {
        await start.handleTrigger(context, event);
        if (!this.delivered.has(event.id)) this.order.push(event.id);
        this.delivered.set(event.id, event);
      },
    };
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, counting, { decodePayload: decodeDatabaseRowChangedEventPayload });
    const runner = newDatabaseRowTriggerRunner({ connection: this.connection, databaseRowChangedRoutes: [{
      bindingName: DatabaseRowChangedTriggerBinding, configuration: { dataSourceId: this.provider.dataSourceId, dataSourceName: 'Requests' },
      target, cursor: this.cursorStore(),
    }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal));
    return { runner, stop: async () => { abort.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
  }

  /** Wait for count more provider requests than were recorded when it was called. */
  async waitForQueries(count: number): Promise<void> {
    const baseline = this.provider.requests.length;
    await eventually(() => this.provider.requests.length >= baseline + count);
  }

  /** Wait for count deliveries, then each change's Flow outcome keyed by the title it was read with. */
  async waitForOutcomes(dex: DexClient, count: number): Promise<Map<string, ChangeOutcome>> {
    await eventually(() => this.delivered.size >= count);
    const outcomes = new Map<string, ChangeOutcome>();
    for (const eventId of this.order) {
      const event = this.delivered.get(eventId)!;
      const result = await dex.waitForFlow(resolveFlowId(event), 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const outcome = result.singleOutput<ChangeOutcome>();
      expect(outcome.rowId).toBe(event.payload.row.id);
      outcomes.set(event.payload.row.title, outcome);
    }
    return outcomes;
  }
}

let directory: string;
let cache: BlobCache;
const workers: Worker[] = [];
const clients: DexClient[] = [];

beforeAll(async () => { directory = await mkdtemp(join(tmpdir(), 'dex-notion-row-changed-')); cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 }); });
// Each test stops its Worker, so the next test's Flows run only against that test's provider.
afterEach(async () => {
  for (const client of clients.splice(0)) await client.close();
  for (const worker of workers.splice(0)) await worker.close();
});
afterAll(async () => {
  cache.close();
  await rm(directory, { recursive: true, force: true });
});

/** Start a Worker and Client for a new environment with a unique watched data source, so Flow IDs are unique. */
async function newEnvironment(): Promise<{ environment: RowChangedEnvironment; dex: DexClient }> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev service');
  const environment = new RowChangedEnvironment(new NotionRowsFake(randomUUID(), randomUUID()));
  const registry = new Registry([environment.flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  workers.push(worker);
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  const dex = new DexClient(registry, cache, { serverAddress });
  clients.push(dex);
  return { environment, dex };
}

it('starts one Flow per row version across a restart, never for the baseline or a trashed row', async () => {
  const { environment, dex } = await newEnvironment();
  const { provider } = environment;
  provider.create(rowStandup, 'Standup', environment.minutesFromNow(-60));
  provider.create(rowReview, 'Review', environment.minutesFromNow(-30));
  const first = environment.startTriggerProcess(dex);
  await environment.waitForCommittedHighWater(environment.minutesFromNow(-30));
  provider.create(rowKickoff, 'Kickoff', environment.now);
  provider.edit(rowStandup, 'Standup moved', environment.now);
  provider.trash(rowReview, environment.now);
  first.runner.wakePoller(DatabaseRowChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(dex, 2);
  expect(outcomes.get('Kickoff')!.changeKind).toBe('created');
  expect(outcomes.get('Standup moved')!.changeKind).toBe('updated');
  expect(outcomes.get('Standup moved')!.current.id, 'the Flow reads the row back from Notion').toBe(rowStandup);
  await first.stop();

  provider.create(rowOffline, 'Planned while stopped', environment.minutesFromNow(1));
  provider.edit(rowKickoff, 'Kickoff moved', environment.minutesFromNow(1));
  const second = environment.startTriggerProcess(dex);
  second.runner.wakePoller(DatabaseRowChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(dex, 4);
  expect(outcomes.get('Planned while stopped')!.changeKind).toBe('created');
  expect(outcomes.get('Kickoff moved')!.changeKind).toBe('updated');
  await environment.waitForCommittedHighWater(environment.minutesFromNow(1));
  second.runner.wakePoller(DatabaseRowChangedTriggerBinding);
  await environment.waitForQueries(1);
  await second.stop();
  expect(environment.delivered.size, 'no row version is delivered twice, the baseline and the trashed row never').toBe(4);
  expect(await new TriggerInbox(environment.objects, scope, inboxKey).pending()).toEqual([]);
}, 120_000);

it('pauses for Retry-After and records a new baseline for a foreign cursor', async () => {
  const { environment, dex } = await newEnvironment();
  const { provider } = environment;
  provider.create(rowStandup, 'Existing', environment.minutesFromNow(-60));
  const store = environment.cursorStore();
  const record = await store.readPollCursor();
  await store.replacePollCursor(record.revision, { token: '{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-01-01T00:00:00Z"}', seededAtUnixMs: Date.now(), committedAtUnixMs: Date.now(), commitCount: 1, resetCount: 0 });
  provider.rateLimitNextRequests(1, '1');
  const process = environment.startTriggerProcess(dex);
  await eventually(async () => (await environment.cursorState())?.resetCount === 1);
  await environment.waitForCommittedHighWater(environment.minutesFromNow(-60));
  expect(provider.requests[0]!.status).toBe(429);
  provider.create(rowKickoff, 'After the reset', environment.now);
  process.runner.wakePoller(DatabaseRowChangedTriggerBinding);
  const outcomes = await environment.waitForOutcomes(dex, 1);
  await process.stop();
  expect(outcomes.get('After the reset')!.changeKind).toBe('created');
  expect(outcomes.has('Existing'), 'the new baseline delivers no existing row').toBe(false);
}, 120_000);
