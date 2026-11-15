// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runEmployeeChangeWorker } from '../../examples/employee-changed/app.js';
import { ConnectionName, EmployeeChangedTriggerBinding, resolveFlowId, type EmployeeChangeOutcome } from '../../examples/employee-changed/flow.js';
import { EmployeeChangeSettleWindowMs, type EmployeeChangedEvent } from '../../src/index.js';
import { BambooHRFake, newTestProject } from '../bamboohr_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'bamboohr', connectionName: ConnectionName, triggerName: 'employeeChanged', bindingName: EmployeeChangedTriggerBinding };
type Bindings = Parameters<typeof newTestProject>[1];

/** One Worker and Trigger process of the example over shared project storage. */
async function startProcess(fake: BambooHRFake, delivered: Map<string, TriggerEvent<EmployeeChangedEvent>>, bindings: Bindings = [], objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, bindings, objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runEmployeeChangeWorker(project, {
    serverAddress: dexAddress(), bambooHROptions: { send: fake.send }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<EmployeeChangedEvent>>, count: number): Promise<Map<string, EmployeeChangeOutcome>> {
  await expect.poll(() => delivered.size, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes = new Map<string, EmployeeChangeOutcome>();
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<EmployeeChangeOutcome>();
    expect(outcome.employeeId).toBe(event.payload.employeeId);
    outcomes.set(outcome.employeeId, outcome);
  }
  return outcomes;
}

// A unique clock and employee IDs keep Trigger event IDs, and so Flow IDs, unique on a shared server.
const newFake = () => new BambooHRFake(new Date(Date.now() - 86_400_000));
const employeeIds = (count: number) => { const base = 1_000_000 + (Date.now() % 1_000_000_000) * 10; return Array.from({ length: count }, (_, offset) => String(base + offset)); };

it('starts one Flow per employee change across a restart with real Dex', async () => {
  const fake = newFake(), delivered = new Map<string, TriggerEvent<EmployeeChangedEvent>>();
  const [existing, hire, mover, leaver, offline] = employeeIds(5) as [string, string, string, string, string];
  fake.insert(existing, { firstName: 'Ava' }); fake.insert(mover, { firstName: 'Sam', department: 'Sales' }); fake.insert(leaver, { firstName: 'Lee' });
  fake.advance(1000);
  const first = await startProcess(fake, delivered);
  await expect.poll(async () => (await cursorState(first.project))?.token, { timeout: 30_000 }).toBeTruthy();
  fake.insert(hire, { firstName: 'Noa', lastName: 'Kim', workEmail: 'noa@example.com' }); fake.update(mover, { department: 'Engineering' }); fake.delete(leaver);
  fake.advance(EmployeeChangeSettleWindowMs);
  first.runner.wakePoller(EmployeeChangedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 3);
  expect(outcomes.get(hire)?.action).toBe('Inserted');
  expect(outcomes.get(hire)?.current?.fields?.workEmail, 'the Flow reads the employee back').toBe('noa@example.com');
  expect(outcomes.get(mover)?.current?.fields?.department).toBe('Engineering');
  expect(outcomes.get(leaver)).toMatchObject({ action: 'Deleted', isGone: true });
  expect(outcomes.get(leaver)?.current).toBeUndefined();
  await first.stop();

  fake.insert(offline, { firstName: 'Planned while stopped' });
  fake.advance(EmployeeChangeSettleWindowMs);
  const second = await startProcess(fake, delivered, [], first.objects);
  try {
    second.runner.wakePoller(EmployeeChangedTriggerBinding);
    outcomes = await waitForOutcomes(second.client, delivered, 4);
    expect(outcomes.get(offline)?.current?.fields?.firstName).toBe('Planned while stopped');
    expect(outcomes.has(existing), 'the change before the seed never starts a Flow').toBe(false);
    expect(delivered.size, 'no change twice').toBe(4);
    await expect.poll(async () => JSON.parse((await cursorState(second.project))!.token).afterEmployeeId, { timeout: 30_000 }).toBe(offline);
  } finally { await second.stop(); }
}, 120_000);

it('filters by the saved change type and reseeds after a rejected cursor, with real Dex', async () => {
  const fake = newFake(), delivered = new Map<string, TriggerEvent<EmployeeChangedEvent>>();
  const [hire, edited, lost, later] = employeeIds(4) as [string, string, string, string];
  fake.insert(edited, { firstName: 'Ava' });
  const process = await startProcess(fake, delivered, [{ bindingName: EmployeeChangedTriggerBinding, configuration: { changeType: 'inserted' } }]);
  try {
    await expect.poll(async () => (await cursorState(process.project))?.token, { timeout: 30_000 }).toBeTruthy();
    fake.insert(hire, { firstName: 'Noa' }); fake.update(edited, { department: 'Sales' });
    fake.advance(EmployeeChangeSettleWindowMs);
    process.runner.wakePoller(EmployeeChangedTriggerBinding);
    expect([...(await waitForOutcomes(process.client, delivered, 1)).keys()], 'the saved inserted type skips the update').toEqual([hire]);

    fake.insert(lost, { firstName: 'Lost in the gap' });
    fake.advance(EmployeeChangeSettleWindowMs);
    fake.rejectChangeLists(1);
    process.runner.wakePoller(EmployeeChangedTriggerBinding);
    await expect.poll(async () => (await cursorState(process.project))?.resetCount, { timeout: 30_000 }).toBe(1);
    fake.insert(later, { firstName: 'After the reset' });
    fake.advance(EmployeeChangeSettleWindowMs);
    process.runner.wakePoller(EmployeeChangedTriggerBinding);
    const outcomes = await waitForOutcomes(process.client, delivered, 2);
    expect([...outcomes.keys()].sort(), 'the change in the gap is not delivered').toEqual([hire, later].sort());
    expect(fake.changeListQueries.every(query => query.get('type') === 'inserted')).toBe(true);
  } finally { await process.stop(); }
}, 120_000);
