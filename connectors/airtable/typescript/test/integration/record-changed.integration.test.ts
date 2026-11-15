// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, TriggerInbox, loadFromEnvironment, newDurableTriggerTarget, newPollCursorStore, type Scope, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterAll, afterEach, beforeAll, expect, it } from 'vitest';
import { AirtableClient, decodeRecordChangedEventPayload, newConnection, newProjectConnection, newRecordChangedTriggerRunner, type RecordChangedEvent } from '../../src/index.js';
import { ConnectionName, RecordChangedIntakeFlow, RecordChangedTriggerBinding, acceptChangedRecord, mapToFlowInput, resolveFlowId, type Intake } from '../../examples/record-changed-intake/flow/workflow.js';
import { newRecordChangedIntakeRunner } from '../../examples/record-changed-intake/runner.js';
import { AccessToken, AirtableTableFake, BaseID, Endpoint, FieldID, TableID, fakeRecord } from '../airtable_table_fake.js';

const scope: Scope = { projectId: 'airtable-record-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'airtable', connectionName: ConnectionName, triggerName: 'recordChanged', bindingName: RecordChangedTriggerBinding };

/** Poll a condition until it holds. */
async function eventually(isMet: () => boolean | Promise<boolean>, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await isMet())) {
    if (Date.now() > deadline) throw new Error('condition not met in time');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

/** A per-run base time on a whole second, so Flow IDs from earlier runs against one Dex Server never collide. */
function uniqueTime(): number { return Math.floor(Date.now() / 1000) * 1000; }

let directory: string;
let cache: BlobCache;
const workers: Worker[] = [];
const clients: DexClient[] = [];

beforeAll(async () => { directory = await mkdtemp(join(tmpdir(), 'dex-airtable-record-changed-')); cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 }); });
// Each test stops its Worker, so the next test's Flows run only against that test's provider.
afterEach(async () => {
  for (const client of clients.splice(0)) await client.close();
  for (const worker of workers.splice(0)) await worker.close();
});
afterAll(async () => {
  cache.close();
  await rm(directory, { recursive: true, force: true });
});

async function startDex(flow: RecordChangedIntakeFlow): Promise<DexClient> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new TypeError('DEX_FLOW_SERVICE_ADDRESS must name a running dexcli dev service');
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  workers.push(worker);
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  const client = new DexClient(registry, cache, { serverAddress });
  clients.push(client);
  return client;
}

/** Run one Trigger process, as a restartable replica. */
function startTriggerProcess(runner: Runner): () => Promise<void> {
  const abort = new AbortController();
  const running = runner.run(newTriggerContext(abort.signal));
  return async () => { abort.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); };
}

async function waitForStartedFlow(dex: DexClient, flowId: string): Promise<Intake> {
  let output: Intake | undefined;
  await eventually(async () => {
    try {
      const result = await dex.waitForFlow(flowId, 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      output = result.singleOutput<Intake>();
      return true;
    } catch (error) {
      if (error instanceof Error && /not found/i.test(error.message)) return false;
      throw error;
    }
  });
  return output!;
}

it('starts one Flow per create and edit across a renamed field and a restart that first answers 503', async () => {
  const baseTime = uniqueTime();
  const table = new AirtableTableFake(fakeRecord('recExisting000001', new Date(baseTime)));
  const client = new AirtableClient({ endpoint: Endpoint, pollIntervalMs: 1_000 }, { resolve: async () => ({ personalAccessToken: new SecretString(AccessToken) }) }, { send: table.send });
  const connection = newConnection(client, { provider: 'airtable', name: ConnectionName });
  const flow = new RecordChangedIntakeFlow(connection);
  const dex = await startDex(flow);
  const objects = new InMemoryObjectStore();
  const cursorStore = newPollCursorStore(objects, scope, inboxKey);
  const start = newDexFlowTriggerTarget(dex, flow, acceptChangedRecord, resolveFlowId, mapToFlowInput);
  const delivered = new Map<string, TriggerEvent<RecordChangedEvent>>();
  const order: string[] = [];
  const counting: TriggerTarget<RecordChangedEvent> = {
    handleTrigger: async (context, event) => {
      await start.handleTrigger(context, event);
      if (!delivered.has(event.id)) order.push(event.id);
      delivered.set(event.id, event);
    },
  };
  // The project runner's wiring: the inbox records each change before the cursor commits.
  const newRunner = () => newRecordChangedTriggerRunner({ connection, routes: [{
    bindingName: RecordChangedTriggerBinding, configuration: { baseId: BaseID, tableId: TableID, tableName: 'Leads', fieldId: FieldID, fieldName: 'Last Modified' },
    cursor: cursorStore,
    target: newDurableTriggerTarget(new TriggerInbox(objects, scope, inboxKey), inboxKey, counting, { decodePayload: decodeRecordChangedEventPayload }),
  }] });
  const outcomes = async (count: number) => {
    await eventually(() => delivered.size >= count);
    const results: Intake[] = [];
    for (const eventId of order) results.push(await waitForStartedFlow(dex, resolveFlowId(delivered.get(eventId)!)));
    // Each Flow read its record back through the connection's getRecord Step.
    for (const intake of results) expect([intake.isCompleted, intake.isRemoved]).toEqual([true, false]);
    return results;
  };

  const first = newRunner();
  let stop = startTriggerProcess(first);
  await eventually(async () => (await cursorStore.readPollCursor()).state !== undefined);
  table.put(fakeRecord('recNewLead0000001', new Date(baseTime + 1_000)), fakeRecord('recExisting000001', new Date(baseTime + 2_000)));
  first.wakePoller(RecordChangedTriggerBinding);
  let intakes = await outcomes(2);
  let changes = intakes.map(intake => intake.change);
  expect(changes.map(change => change.recordId)).toEqual(['recNewLead0000001', 'recExisting000001']);
  expect(changes[0]!.isCreatedRecord).toBe(true);
  expect(changes[0]!.fields.Email).toBe('recNewLead0000001@example.test');
  expect(intakes[0]!.currentFields?.Email).toBe('recNewLead0000001@example.test');
  expect(changes[1]!.lastModifiedTime).toBe(new Date(baseTime + 2_000).toISOString().replace('.000Z', 'Z'));

  table.renameField('Updated At');
  table.put(fakeRecord('recRenamed0000001', new Date(baseTime + 3_000)));
  first.wakePoller(RecordChangedTriggerBinding);
  intakes = await outcomes(3);
  changes = intakes.map(intake => intake.change);
  expect(changes[2]!.recordId).toBe('recRenamed0000001');
  expect(changes[2]!.fields['Updated At']).toBeTruthy();
  expect(intakes[2]!.currentFields?.['Updated At']).toBeTruthy();
  await stop();

  table.put(fakeRecord('recWhileStopped01', new Date(baseTime + 4_000)));
  table.script({ status: 503, errorType: 'SERVICE_UNAVAILABLE' });
  const second = newRunner();
  stop = startTriggerProcess(second);
  second.wakePoller(RecordChangedTriggerBinding);
  changes = (await outcomes(4)).map(intake => intake.change);
  expect(changes[3]!.recordId).toBe('recWhileStopped01');
  await stop();
  expect(delivered.size, 'no change is delivered twice and existing records never').toBe(4);
  expect(table.statuses, 'the restarted poller recovered from a failed request').toContain(503);
  expect(await new TriggerInbox(objects, scope, inboxKey).pending()).toEqual([]);
}, 120_000);

it('the example runner polls the binding saved in the project', async () => {
  const baseTime = uniqueTime();
  const table = new AirtableTableFake(fakeRecord('recExisting000001', new Date(baseTime)));
  const connectionsPath = join(directory, `connections-${baseTime}.json`);
  await writeFile(connectionsPath, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{
      connectorId: 'airtable', connectionName: ConnectionName, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/airtable',
      provider: 'airtable', authMethodId: 'api-key-only', configuration: { endpoint: Endpoint, pollInterval: 1_000_000_000 },
      credentials: { personal_access_token: AccessToken },
    }],
    triggerBindings: [{
      connectorId: 'airtable', connectionName: ConnectionName, triggerName: 'recordChanged', bindingName: RecordChangedTriggerBinding,
      configuration: { baseId: BaseID, baseName: 'Intake', tableId: TableID, tableName: 'Leads', fieldId: FieldID, fieldName: 'Last Modified' },
    }],
  }));
  await chmod(connectionsPath, 0o600);
  process.env.DEX_CONNECTOR_CONFIG_FILE = connectionsPath;
  const project = await loadFromEnvironment();
  const flow = new RecordChangedIntakeFlow(newProjectConnection(project, ConnectionName, { send: table.send }));
  const dex = await startDex(flow);
  const stop = startTriggerProcess(newRecordChangedIntakeRunner(project, dex, flow, { send: table.send }));
  try {
    await eventually(() => table.listRequests.length >= 1);
    table.put(fakeRecord('recNewLead0000001', new Date(baseTime + 1_000)));
    const eventTime = new Date(baseTime + 1_000).toISOString().replace(/[-:]/g, '').replace(/\.\d{3}Z$/, '');
    const intake = await waitForStartedFlow(dex, `airtable-record-change-recNewLead0000001-${eventTime}000000000Z`);
    expect([intake.change.recordId, intake.isCompleted]).toEqual(['recNewLead0000001', true]);
    expect(intake.currentFields?.Email).toBe('recNewLead0000001@example.test');
  } finally {
    await stop();
    delete process.env.DEX_CONNECTOR_CONFIG_FILE;
  }
}, 120_000);
