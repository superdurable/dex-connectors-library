// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import type { TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runEventChangeWatcher } from '../../examples/event-changed/app.js';
import { ConnectionName, EventChangedTriggerBinding, type ChangeOutcome } from '../../examples/event-changed/flow/workflow.js';
import { CalendarSyncFake } from '../calendar_sync_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const scope = { projectId: 'google-calendar-event-changed', kind: 'live' } as const;
const inboxKey = { connectorId: 'google-calendar', connectionName: ConnectionName, triggerName: 'eventChanged', bindingName: EventChangedTriggerBinding };
/** Keeps the Flow ID of every "trigger event delivered" record, with whether the start was a duplicate. */
function newDeliveryLogger(): TriggerLogger & { deliveries: Array<{ flowId: string; isDuplicate: boolean }> } {
  const deliveries: Array<{ flowId: string; isDuplicate: boolean }> = [];
  return {
    deliveries, isEnabled: () => true,
    log: (_level, message, attributes) => { if (message === 'trigger event delivered') deliveries.push({ flowId: String(attributes.flow_id), isDuplicate: attributes.duplicate === true }); },
  };
}

/** Project storage that survives the application process, with a fresh stand-in access token and a unique calendar. */
async function newEventChangedProject() {
  const fake = new CalendarSyncFake(`team-${process.hrtime.bigint()}@group.calendar.google.com`);
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'google-calendar', connectionName: ConnectionName }, 0, new CredentialMaterial({
    auth_method: 'google-oauth', oauth_client_id: 'stand-in-client', oauth_client_secret: 'stand-in-secret', access_token: 'calendar-token', refresh_token: 'stand-in-refresh',
  }, new Date(Date.now() + 3_600_000), 'google-oauth'));
  const project = () => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'google-calendar', connectionName: ConnectionName, modulePath: 'google-calendar', provider: 'google', configuration: { endpoint: 'http://127.0.0.1/calendar/v3', pollInterval: 5_000_000_000 } }],
    triggerBindings: [{ connectorId: 'google-calendar', connectionName: ConnectionName, triggerName: 'eventChanged', bindingName: EventChangedTriggerBinding, configuration: { calendarId: fake.calendarId } }],
  }, undefined, objects, undefined, undefined);
  const committedToken = async () => (await project().pollCursorStore(inboxKey).readPollCursor()).state;
  const waitForSyncToken = async () => {
    const expected = `{"calendarId":"${fake.calendarId}","syncToken":"${fake.syncToken()}"}`;
    await expect.poll(async () => (await committedToken())?.token, { timeout: 30_000 }).toBe(expected);
  };
  return { fake, project, committedToken, waitForSyncToken };
}

/** One run of the example application; stopping it models a process exit. */
async function startWatcher(project: LoadedProject, send: CalendarSyncFake['send'], logger: TriggerLogger) {
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runEventChangeWatcher(project, { serverAddress: dexAddress(), logger, calendarOptions: { send }, onReady: resolveReady }, stop.signal);
  const { client, runner } = await ready;
  return { client, runner, stop: async () => { stop.abort(); await running; } };
}

/** Wait for every delivered Flow to complete and return the outcomes keyed by event ID; a later change replaces an earlier one. */
async function outcomesOf(client: Client, flowIds: string[]): Promise<Map<string, ChangeOutcome>> {
  const outcomes = new Map<string, ChangeOutcome>();
  for (const flowId of flowIds) {
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<ChangeOutcome>();
    outcomes.set(outcome.eventId, outcome);
  }
  return outcomes;
}

const startedFlowIds = (logger: ReturnType<typeof newDeliveryLogger>) => logger.deliveries.filter(delivery => !delivery.isDuplicate).map(delivery => delivery.flowId);

it('starts one Flow per change after the baseline, and delivers changes made while stopped after a restart', async () => {
  const environment = await newEventChangedProject();
  const { fake } = environment;
  fake.insert('standup0001', 'Standup');
  fake.insert('review00001', 'Review');
  const logger = newDeliveryLogger();
  const first = await startWatcher(environment.project(), fake.send, logger);
  try {
    await environment.waitForSyncToken();
    fake.insert('kickoff0001', 'Kickoff');
    fake.update('standup0001', 'Standup moved');
    fake.cancel('review00001');
    first.runner.wakePoller(EventChangedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(3);
    const outcomes = await outcomesOf(first.client, startedFlowIds(logger));
    expect([outcomes.get('kickoff0001')?.changeKind, outcomes.get('kickoff0001')?.current?.summary]).toEqual(['created', 'Kickoff']);
    expect([outcomes.get('standup0001')?.changeKind, outcomes.get('standup0001')?.current?.summary], 'the Flow reads the event back from Google').toEqual(['updated', 'Standup moved']);
    expect(outcomes.get('review00001')?.changeKind).toBe('cancelled');
    expect(outcomes.get('review00001')?.current, 'a cancelled event is not read back').toBeUndefined();
  } finally { await first.stop(); }

  fake.insert('offline0001', 'Planned while stopped');
  fake.update('kickoff0001', 'Kickoff moved');
  const second = await startWatcher(environment.project(), fake.send, logger);
  try {
    second.runner.wakePoller(EventChangedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(5);
    const outcomes = await outcomesOf(second.client, startedFlowIds(logger).slice(3));
    expect(outcomes.get('offline0001')?.current?.summary).toBe('Planned while stopped');
    expect([outcomes.get('kickoff0001')?.changeKind, outcomes.get('kickoff0001')?.current?.summary]).toEqual(['updated', 'Kickoff moved']);
    await environment.waitForSyncToken();
    expect(new Set(startedFlowIds(logger)).size, 'no change is delivered twice and the baseline is never delivered').toBe(5);
    expect(logger.deliveries.filter(delivery => delivery.isDuplicate)).toEqual([]);
  } finally { await second.stop(); }
}, 120_000);

it('records a new baseline after an expired sync token without delivering the gap', async () => {
  const environment = await newEventChangedProject();
  const { fake } = environment;
  fake.insert('standup0001', 'Standup');
  const logger = newDeliveryLogger();
  const first = await startWatcher(environment.project(), fake.send, logger);
  try { await environment.waitForSyncToken(); } finally { await first.stop(); }

  fake.insert('gapevent001', 'Lost in the gap');
  fake.expireSyncTokens();
  const second = await startWatcher(environment.project(), fake.send, logger);
  try {
    await expect.poll(async () => (await environment.committedToken())?.resetCount, { timeout: 30_000 }).toBe(1);
    await environment.waitForSyncToken();
    fake.insert('afterreset1', 'After the reset');
    second.runner.wakePoller(EventChangedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(1);
    const outcomes = await outcomesOf(second.client, startedFlowIds(logger));
    expect(outcomes.get('afterreset1')?.current?.summary).toBe('After the reset');
    expect(outcomes.has('gapevent001'), 'changes between the expired token and the new baseline are not delivered').toBe(false);
  } finally { await second.stop(); }
}, 120_000);
