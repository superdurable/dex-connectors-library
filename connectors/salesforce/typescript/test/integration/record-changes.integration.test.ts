// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, newPollCursorStore } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runRecordChangeWatcher } from '../../examples/record-changes/app.js';
import { ConnectionName, RecordModifiedTriggerBinding, type ChangeOutcome } from '../../examples/record-changes/flow/workflow.js';
import { FakeInstanceURL, SalesforceOrgFake } from '../support/salesforce_org_fake.js';

const scope = { projectId: 'salesforce-record-changes', kind: 'live' } as const;
const inboxKey = { connectorId: 'salesforce', connectionName: ConnectionName, triggerName: 'recordModifiedPolled', bindingName: RecordModifiedTriggerBinding };
// An inert fixture credential the fake org accepts; it is not a Salesforce session.
const accessToken = 'inert-salesforce-' + 'session';

function dexAddress(): string {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
}

/** A Trigger logger that keeps every record, so the test can count Flow-start deliveries. */
function newRecordingLogger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); } };
}

/** One project over storage that survives a process restart, watching a custom object unique to the run. */
async function newEnvironment() {
  const fake = new SalesforceOrgFake(accessToken);
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'salesforce', connectionName: ConnectionName }, 0, new CredentialMaterial({
    auth_method: 'salesforce-oauth', oauth_client_id: 'inert-consumer-key', oauth_client_secret: 'inert-consumer-' + 'secret',
    access_token: accessToken, refresh_token: 'inert-refresh-' + 'grant', instance_url: FakeInstanceURL,
  }, undefined, 'salesforce-oauth'));
  // A custom object unique to the run keeps Trigger event IDs, and so Flow IDs, unique on a shared Dex.
  const environment = {
    fake, sObjectType: `Deal${process.hrtime.bigint()}__c`, logger: newRecordingLogger(),
    project: () => new LoadedProject({
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
      connections: [{ connectorId: 'salesforce', connectionName: ConnectionName, modulePath: 'salesforce', provider: 'salesforce', configuration: { pollInterval: 5_000_000_000, pollPageSize: 2 } }],
      triggerBindings: [{
        connectorId: 'salesforce', connectionName: ConnectionName, triggerName: 'recordModifiedPolled', bindingName: RecordModifiedTriggerBinding,
        configuration: { sObjectType: environment.sObjectType, fieldNames: ['Name', 'StageName'] },
      }],
    }, undefined, objects, undefined, undefined),
    deliveredFlowIds: () => [...new Set(environment.logger.records.filter(record => record.message === 'trigger event delivered').map(record => String(record.attributes.flow_id)))],
    cursorState: async () => (await newPollCursorStore(objects, scope, inboxKey).readPollCursor()).state,
  };
  return environment;
}
type Environment = Awaited<ReturnType<typeof newEnvironment>>;

/** One run of the application; stopping it models a process exit. */
async function startProcess(environment: Environment) {
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runRecordChangeWatcher(environment.project(), {
    serverAddress: dexAddress(), logger: environment.logger, salesforceOptions: { send: environment.fake.send }, onReady: resolveReady,
  }, stop.signal);
  running.catch(() => undefined);
  const started = await Promise.race([ready, running.then(() => { throw new Error('watcher stopped before it was ready'); })]);
  return { ...started, stop: async () => { stop.abort(); await running; } };
}

/** Wait for count delivered changes, then for each change's Flow, keyed by record ID; a later change replaces an earlier one. */
async function waitForOutcomes(environment: Environment, client: Client, count: number): Promise<Record<string, ChangeOutcome>> {
  await expect.poll(() => environment.deliveredFlowIds().length, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes: Record<string, ChangeOutcome> = {};
  for (const flowId of environment.deliveredFlowIds()) {
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, `${flowId}: ${result.errorMessage}`).toBe('completed');
    const outcome = result.singleOutput<ChangeOutcome>();
    outcomes[outcome.recordId] = outcome;
  }
  return outcomes;
}

it('starts one Flow per change across a bulk load, a rate limit, a deletion, and a restart with real Dex', async () => {
  const environment = await newEnvironment();
  const { fake, sObjectType } = environment;
  const renewal = fake.insert(sObjectType, { Name: 'Renewal', StageName: 'Prospecting' });
  const doomed = fake.insert(sObjectType, { Name: 'Doomed', StageName: 'Prospecting' });
  const first = await startProcess(environment);
  let expansion: string;
  try {
    await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined();
    expansion = fake.insert(sObjectType, { Name: 'Expansion', StageName: 'Qualification' });
    fake.update(renewal, { StageName: 'Closed Won' });
    fake.update(doomed, { StageName: 'Closed Lost' });
    fake.deleteAfterListing.add(doomed);
    first.runner.wakePoller(RecordModifiedTriggerBinding);
    let outcomes = await waitForOutcomes(environment, first.client, 3);
    expect(outcomes[expansion]).toMatchObject({ changeKind: 'created', isDeleted: false, current: { fields: { Name: 'Expansion' } } });
    expect(outcomes[renewal], 'the Flow reads the record back from Salesforce').toMatchObject({ changeKind: 'updated', current: { fields: { StageName: 'Closed Won' } } });
    expect(outcomes[doomed], 'a record deleted before the read-back completes as deleted').toMatchObject({ isDeleted: true });
    expect(outcomes[doomed]?.current).toBeUndefined();

    const bulk = fake.insertBatch(sObjectType, 5, index => ({ Name: `Bulk ${index}`, StageName: 'Prospecting' }));
    fake.rateLimitNext(1, '1');
    const limited = fake.queryCount;
    first.runner.wakePoller(RecordModifiedTriggerBinding);
    outcomes = await waitForOutcomes(environment, first.client, 8);
    for (const id of bulk) expect(outcomes[id]?.changeKind, 'every record of a bulk load sharing one stamp arrives').toBe('created');
    expect(fake.queryCount, 'the poll after the pause queried again').toBeGreaterThan(limited + 1);
    expect(environment.logger.records.some(record => record.message === 'poll rate limited; pausing connection')).toBe(true);
  } finally { await first.stop(); }

  const offline = fake.insert(sObjectType, { Name: 'Created while stopped' });
  fake.update(expansion, { StageName: 'Negotiation' });
  const second = await startProcess(environment);
  try {
    second.runner.wakePoller(RecordModifiedTriggerBinding);
    const outcomes = await waitForOutcomes(environment, second.client, 10);
    expect(outcomes[offline]?.current?.fields?.Name).toBe('Created while stopped');
    expect(outcomes[expansion]?.current?.fields?.StageName).toBe('Negotiation');
    const queries = fake.queryCount;
    second.runner.wakePoller(RecordModifiedTriggerBinding);
    await expect.poll(() => fake.queryCount, { timeout: 30_000 }).toBeGreaterThan(queries);
    expect(environment.deliveredFlowIds(), 'no change is delivered twice and the baseline is never delivered').toHaveLength(10);
    expect((await environment.cursorState())?.resetCount).toBe(0);
  } finally { await second.stop(); }
}, 180_000);

it('records a new baseline after the binding changes object with real Dex', async () => {
  const environment = await newEnvironment();
  environment.fake.insert(environment.sObjectType, { Name: 'First object' });
  const first = await startProcess(environment);
  try { await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined(); } finally { await first.stop(); }

  environment.sObjectType = `Other${process.hrtime.bigint()}__c`;
  const existing = environment.fake.insert(environment.sObjectType, { Name: 'Already there' });
  const second = await startProcess(environment);
  try {
    await expect.poll(async () => (await environment.cursorState())?.resetCount, { timeout: 30_000 }).toBe(1);
    const afterReset = environment.fake.insert(environment.sObjectType, { Name: 'After the reset' });
    second.runner.wakePoller(RecordModifiedTriggerBinding);
    const outcomes = await waitForOutcomes(environment, second.client, 1);
    expect(outcomes[afterReset]?.current?.fields?.Name).toBe('After the reset');
    expect(outcomes, "the new object's existing records are its baseline").not.toHaveProperty(existing);
  } finally { await second.stop(); }
}, 120_000);
