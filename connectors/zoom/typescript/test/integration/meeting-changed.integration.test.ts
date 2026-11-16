// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runMeetingChangeWorker } from '../../examples/meeting-changed/app.js';
import { ConnectionName, MeetingChangedTriggerBinding, resolveFlowId, type MeetingChangeOutcome } from '../../examples/meeting-changed/flow.js';
import type { MeetingChangedEvent } from '../../src/index.js';
import { ZoomMeetingsFake, newTestProject } from '../zoom_meetings_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'zoom', connectionName: ConnectionName, triggerName: 'meetingChanged', bindingName: MeetingChangedTriggerBinding };

/** One Worker and Trigger process of the example over shared project storage. */
async function startProcess(fake: ZoomMeetingsFake, delivered: Map<string, TriggerEvent<MeetingChangedEvent>>, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runMeetingChangeWorker(project, {
    serverAddress: dexAddress(), zoomOptions: { send: fake.send }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForSnapshot(project: LoadedProject, meetingCount: number): Promise<void> {
  await expect.poll(async () => (await cursorState(project))?.snapshot?.entries.length, { timeout: 30_000 }).toBe(meetingCount);
}

async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<MeetingChangedEvent>>, count: number): Promise<Map<number, MeetingChangeOutcome>> {
  await expect.poll(() => delivered.size, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes = new Map<number, MeetingChangeOutcome>();
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<MeetingChangeOutcome>();
    expect(outcome.meetingId).toBe(event.payload.meeting.id);
    outcomes.set(outcome.meetingId, outcome);
  }
  return outcomes;
}

const firstMeetingId = () => 10_000_000_000 + (Date.now() % 1_000_000) * 10;

it('starts one Flow per meeting change across a restart with real Dex', async () => {
  const fake = new ZoomMeetingsFake('zoom-host-1'), delivered = new Map<string, TriggerEvent<MeetingChangedEvent>>();
  const base = firstMeetingId(), [consultation, review, kickoff, offline] = [base, base + 1, base + 2, base + 3];
  fake.schedule(consultation, 'Consultation', 1); fake.schedule(review, 'Team review', 2);
  const first = await startProcess(fake, delivered);
  await waitForSnapshot(first.project, 2);
  fake.schedule(kickoff, 'Kickoff', 3); fake.reschedule(consultation, 2); fake.delete(review);
  first.runner.wakePoller(MeetingChangedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 3);
  expect(outcomes.get(kickoff)?.changeKind).toBe('scheduled');
  expect(outcomes.get(kickoff)?.current?.settings.waitingRoom, 'the Flow reads the full meeting back').toBe(true);
  expect(outcomes.get(consultation)?.changeKind).toBe('updated');
  expect(outcomes.get(review)?.changeKind).toBe('removed');
  expect(outcomes.get(review)?.current, 'a removed meeting is not read back').toBeUndefined();
  await first.stop();

  fake.schedule(offline, 'Planned while stopped', 4); fake.rename(kickoff, 'Kickoff moved');
  const second = await startProcess(fake, delivered, first.objects);
  try {
    second.runner.wakePoller(MeetingChangedTriggerBinding);
    outcomes = await waitForOutcomes(second.client, delivered, 5);
    expect(outcomes.get(offline)?.current?.topic).toBe('Planned while stopped');
    expect(outcomes.get(kickoff)?.changeKind).toBe('updated');
    expect(outcomes.get(kickoff)?.current?.topic).toBe('Kickoff moved');
    await waitForSnapshot(second.project, 3);
    expect(delivered.size, 'no change twice and never the baseline').toBe(5);
  } finally { await second.stop(); }
}, 120_000);

it("records a new baseline when the listing belongs to another Zoom user, with real Dex", async () => {
  const fake = new ZoomMeetingsFake('zoom-host-1'), delivered = new Map<string, TriggerEvent<MeetingChangedEvent>>();
  const base = firstMeetingId() + 5;
  fake.schedule(base, 'Consultation', 1);
  const first = await startProcess(fake, delivered);
  await waitForSnapshot(first.project, 1);
  await first.stop();

  fake.hostId = 'zoom-host-2';
  fake.schedule(base + 1, 'Lost in the gap', 2);
  const second = await startProcess(fake, delivered, first.objects);
  try {
    await expect.poll(async () => (await cursorState(second.project))?.resetCount, { timeout: 30_000 }).toBe(1);
    await waitForSnapshot(second.project, 2);
    fake.schedule(base + 2, 'After the reset', 3);
    second.runner.wakePoller(MeetingChangedTriggerBinding);
    const outcomes = await waitForOutcomes(second.client, delivered, 1);
    expect([...outcomes.keys()], 'meetings in the gap are part of the new baseline').toEqual([base + 2]);
    expect(outcomes.get(base + 2)?.current?.topic).toBe('After the reset');
    expect(JSON.parse((await cursorState(second.project))!.token)).toEqual({ version: 1, hostId: 'zoom-host-2', sequence: 1 });
  } finally { await second.stop(); }
}, 120_000);
