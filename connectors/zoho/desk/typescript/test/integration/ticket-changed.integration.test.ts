// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import type { TriggerLogAttributes, TriggerLogger, TriggerLogLevel } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, newPollCursorStore } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import { runTicketChangeWatcher } from '../../examples/ticket-changed/app.js';
import { ConnectionName, TicketChangedTriggerBinding, type ChangeOutcome } from '../../examples/ticket-changed/flow/workflow.js';
import { DeskSearchFake, FakeAccessToken, FakeOrganizationID } from '../support/desk_search_fake.js';

const scope = { projectId: 'zoho-desk-ticket-changed', kind: 'live' } as const;
const departmentId = '1892000000006907';
const inboxKey = { connectorId: 'zoho-desk', connectionName: ConnectionName, triggerName: 'ticketChanged', bindingName: TicketChangedTriggerBinding };
const fakes: DeskSearchFake[] = [];
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

/** One project over storage that survives a process restart, with a Billing binding and a 15s poll interval. */
async function newEnvironment() {
  const fake = await DeskSearchFake.start();
  fakes.push(fake);
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'zoho-desk', connectionName: ConnectionName }, 0, new CredentialMaterial({
    auth_method: 'zoho-us-oauth', access_token: FakeAccessToken, oauth_client_id: 'fake-client', oauth_client_secret: 'fake-secret', refresh_token: 'fake-refresh',
  }, new Date(Date.now() + 3_600_000), 'zoho-us-oauth'));
  const project = () => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'zoho-desk', connectionName: ConnectionName, modulePath: 'zoho-desk', provider: 'zoho', configuration: { orgId: FakeOrganizationID, pollInterval: 15_000_000_000 } }],
    triggerBindings: [{ connectorId: 'zoho-desk', connectionName: ConnectionName, triggerName: 'ticketChanged', bindingName: TicketChangedTriggerBinding, configuration: { departmentId, departmentName: 'Billing' } }],
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
  const running = runTicketChangeWatcher(environment.project(), {
    serverAddress: dexAddress(), logger: environment.logger, zohoDeskOptions: { apiBaseURL: environment.fake.baseURL }, onReady: resolveReady,
  }, stop.signal);
  running.catch(() => undefined);
  const started = await Promise.race([ready, running.then(() => { throw new Error('watcher stopped before it was ready'); })]);
  return { ...started, stop: async () => { stop.abort(); await running; } };
}

/** Wait for count delivered changes, then for each change's Flow, keyed by ticket ID; a later version replaces an earlier one. */
async function waitForOutcomes(environment: Awaited<ReturnType<typeof newEnvironment>>, client: Client, count: number): Promise<Record<string, ChangeOutcome>> {
  await expect.poll(() => environment.deliveredFlowIds().length, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes: Record<string, ChangeOutcome> = {};
  for (const flowId of environment.deliveredFlowIds()) {
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, `${flowId}: ${result.errorMessage}`).toBe('completed');
    const outcome = result.singleOutput<ChangeOutcome>();
    outcomes[outcome.ticketId] = outcome;
  }
  return outcomes;
}

it('starts one Flow per ticket version, never for the baseline, across a restart with real Dex', async () => {
  const environment = await newEnvironment();
  const existing = environment.fake.create('Existing before the baseline', departmentId);
  environment.fake.create('Other department', '1892000000082069');
  const first = await startProcess(environment);
  try {
    await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined();
    const created = environment.fake.create('Refund request', departmentId);
    environment.fake.update(existing, 'On Hold');
    first.runner.wakePoller(TicketChangedTriggerBinding);
    let outcomes = await waitForOutcomes(environment, first.client, 2);
    expect(outcomes[created], 'the Flow reads the full ticket back').toMatchObject({ changeKind: 'created', current: { ticket: { subject: 'Refund request', descriptionHtml: '<div>Refund request</div>' } } });
    expect(outcomes[existing]).toMatchObject({ changeKind: 'updated', current: { ticket: { status: 'On Hold' } } });
    await first.stop();

    const offline = environment.fake.create('Opened while stopped', departmentId);
    environment.fake.update(created, 'Closed');
    // The first poll after the restart is rate limited and retried after Retry-After.
    environment.fake.rateLimitNext(1, '1');
    const second = await startProcess(environment);
    try {
      second.runner.wakePoller(TicketChangedTriggerBinding);
      outcomes = await waitForOutcomes(environment, second.client, 4);
      expect(outcomes[offline]).toMatchObject({ changeKind: 'created', current: { ticket: { subject: 'Opened while stopped' } } });
      expect(outcomes[created]).toMatchObject({ changeKind: 'updated', current: { ticket: { status: 'Closed' } } });
      expect(environment.logger.records.some(record => record.message === 'poll rate limited; pausing connection')).toBe(true);
      const searches = environment.fake.searches.length;
      second.runner.wakePoller(TicketChangedTriggerBinding);
      await expect.poll(() => environment.fake.searches.length, { timeout: 30_000 }).toBeGreaterThan(searches);
      expect(environment.deliveredFlowIds(), 'no ticket version is delivered twice and the baseline is never delivered').toHaveLength(4);
      expect(new Set(environment.fake.searches.map(search => search.departmentId))).toEqual(new Set([departmentId]));
    } finally { await second.stop(); }
  } finally { await first.stop().catch(() => undefined); }
}, 120_000);

it('records a new baseline when Zoho Desk rejects the time range, then delivers later tickets with real Dex', async () => {
  const environment = await newEnvironment();
  const process = await startProcess(environment);
  try {
    await expect.poll(environment.cursorState, { timeout: 30_000 }).toBeDefined();
    environment.fake.rejectNextRanges(1);
    process.runner.wakePoller(TicketChangedTriggerBinding);
    await expect.poll(async () => (await environment.cursorState())?.resetCount, { timeout: 30_000 }).toBe(1);
    const afterReset = environment.fake.create('After the reset', departmentId);
    process.runner.wakePoller(TicketChangedTriggerBinding);
    const outcomes = await waitForOutcomes(environment, process.client, 1);
    expect(outcomes[afterReset]?.current.ticket.subject).toBe('After the reset');
  } finally { await process.stop(); }
}, 120_000);
