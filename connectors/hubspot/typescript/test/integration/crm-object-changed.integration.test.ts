// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, newPollCursorStore } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import { runCRMObjectChangeWatcher } from '../../examples/crm-object-changed/app.js';
import { CRMObjectChangedTriggerBinding, ConnectionName, DeliveredProperties, type ChangeOutcome } from '../../examples/crm-object-changed/flow/workflow.js';
import { FakeAccessToken, HubSpotSearchFake } from '../support/hubspot_search_fake.js';

const scope = { projectId: 'hubspot-crm-object-changed', kind: 'live' } as const;
const inboxKey = { connectorId: 'hubspot', connectionName: ConnectionName, triggerName: 'crmObjectChanged', bindingName: CRMObjectChangedTriggerBinding };
const fakes: HubSpotSearchFake[] = [];
afterEach(async () => { await Promise.all(fakes.splice(0).map(fake => fake.close())); });

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

/** One project over storage that survives a process restart, with a deals binding and a 5s poll interval. */
async function newEnvironment() {
  // A start time unique to the run keeps Trigger event IDs, and so Flow IDs, unique on a shared Dex.
  const fake = await HubSpotSearchFake.start('deals', new Date());
  fakes.push(fake);
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'hubspot', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ auth_method: 'private-app-token', access_token: FakeAccessToken }, undefined, 'private-app-token'));
  const project = () => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'hubspot', connectionName: ConnectionName, modulePath: 'hubspot', provider: 'hubspot', configuration: { endpoint: fake.url, pollInterval: 5_000_000_000 } }],
    triggerBindings: [{ connectorId: 'hubspot', connectionName: ConnectionName, triggerName: 'crmObjectChanged', bindingName: CRMObjectChangedTriggerBinding, configuration: { objectType: 'deals' } }],
  }, undefined, objects, undefined, undefined);
  const logger = newRecordingLogger();
  const deliveredFlowIds = () => [...new Set(logger.records.filter(record => record.message === 'trigger event delivered').map(record => String(record.attributes.flow_id)))];
  const cursorState = async () => (await newPollCursorStore(objects, scope, inboxKey).readPollCursor()).state;
  return { fake, project, logger, deliveredFlowIds, cursorState };
}

/** One run of the application; stopping it models a process exit. */
async function startProcess(environment: Awaited<ReturnType<typeof newEnvironment>>) {
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runCRMObjectChangeWatcher(environment.project(), { serverAddress: dexAddress(), logger: environment.logger, onReady: resolveReady }, stop.signal);
  running.catch(() => undefined);
  const started = await Promise.race([ready, running.then(() => { throw new Error('watcher stopped before it was ready'); })]);
  return { ...started, stop: async () => { stop.abort(); await running; } };
}

/** Wait for count delivered changes, then for each change's Flow, keyed by record ID; a later change replaces an earlier one. */
async function waitForOutcomes(environment: Awaited<ReturnType<typeof newEnvironment>>, client: Client, count: number): Promise<Record<string, ChangeOutcome>> {
  await expect.poll(() => environment.deliveredFlowIds().length, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes: Record<string, ChangeOutcome> = {};
  for (const flowId of environment.deliveredFlowIds()) {
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, `${flowId}: ${result.errorMessage}`).toBe('completed');
    const outcome = result.singleOutput<ChangeOutcome>();
    outcomes[outcome.objectId] = outcome;
  }
  return outcomes;
}

it('starts one Flow per change, never for the baseline, across a restart with real Dex', async () => {
  const environment = await newEnvironment();
  const renewal = environment.fake.create({ dealname: 'Renewal', dealstage: 'appointmentscheduled' });
  environment.fake.create({ dealname: 'Untouched' });
  const first = await startProcess(environment);
  try {
    await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined();
    const expansion = environment.fake.create({ dealname: 'Expansion', dealstage: 'appointmentscheduled' });
    environment.fake.update(renewal, { dealstage: 'qualifiedtobuy' });
    first.runner.wakePoller(CRMObjectChangedTriggerBinding);
    let outcomes = await waitForOutcomes(environment, first.client, 2);
    expect(outcomes[expansion]).toMatchObject({ changeKind: 'created', current: { properties: { dealname: 'Expansion' } } });
    expect(outcomes[renewal], 'the Flow reads the record back from HubSpot').toMatchObject({ changeKind: 'updated', current: { properties: { dealstage: 'qualifiedtobuy' } } });
    await first.stop();

    const offline = environment.fake.create({ dealname: 'Created while stopped' });
    environment.fake.update(expansion, { dealstage: 'closedwon' });
    const second = await startProcess(environment);
    try {
      second.runner.wakePoller(CRMObjectChangedTriggerBinding);
      outcomes = await waitForOutcomes(environment, second.client, 4);
      expect(outcomes[offline]?.current.properties?.dealname).toBe('Created while stopped');
      expect(outcomes[expansion]).toMatchObject({ changeKind: 'updated', current: { properties: { dealstage: 'closedwon' } } });
      const searches = environment.fake.searchRequestCount();
      second.runner.wakePoller(CRMObjectChangedTriggerBinding);
      await expect.poll(() => environment.fake.searchRequestCount(), { timeout: 30_000 }).toBeGreaterThan(searches);
      expect(environment.deliveredFlowIds(), 'no change is delivered twice and the baseline is never delivered').toHaveLength(4);
      expect(DeliveredProperties).toContain('dealstage');
    } finally { await second.stop(); }
  } finally { await first.stop().catch(() => undefined); }
}, 120_000);

it('pauses polling after a 429 and then delivers the change with real Dex', async () => {
  const environment = await newEnvironment();
  environment.fake.create({ dealname: 'Baseline' });
  const process = await startProcess(environment);
  try {
    await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined();
    environment.fake.failNextSearches({ status: 429, headers: { 'Retry-After': '1' }, body: '{"category":"RATE_LIMITS","policyName":"SECONDLY"}' });
    const limited = environment.fake.searchRequestCount();
    const created = environment.fake.create({ dealname: 'After the pause' });
    process.runner.wakePoller(CRMObjectChangedTriggerBinding);
    await expect.poll(() => environment.fake.searchRequestCount(), { timeout: 30_000 }).toBeGreaterThan(limited);
    const outcomes = await waitForOutcomes(environment, process.client, 1);
    expect(outcomes[created]?.current.properties?.dealname).toBe('After the pause');
    expect(environment.fake.searchRequestCount(), 'the poll after the pause searched again').toBeGreaterThan(limited + 1);
    expect(environment.logger.records.some(record => record.message === 'poll rate limited; pausing connection')).toBe(true);
  } finally { await process.stop(); }
}, 120_000);
