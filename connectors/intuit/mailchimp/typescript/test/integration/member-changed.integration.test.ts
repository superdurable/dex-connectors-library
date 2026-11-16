// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runMemberChangeWorker } from '../../examples/member-changed/app.js';
import { ConnectionName, MemberChangedTriggerBinding, resolveFlowId, type MemberChangeOutcome } from '../../examples/member-changed/flow.js';
import type { MemberChangedEvent } from '../../src/index.js';
import { APIBaseURL, MailchimpAudienceFake, newTestProject } from '../mailchimp_audience_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'mailchimp', connectionName: ConnectionName, triggerName: 'memberChanged', bindingName: MemberChangedTriggerBinding };

/** One Worker and Trigger process of the example over shared project storage; a unique audience keeps Flow IDs unique. */
async function startProcess(fake: MailchimpAudienceFake, delivered: Map<string, TriggerEvent<MemberChangedEvent>>, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [{ bindingName: MemberChangedTriggerBinding, configuration: { audienceId: fake.audienceId } }], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runMemberChangeWorker(project, {
    serverAddress: dexAddress(), mailchimpOptions: { apiBaseURL: APIBaseURL, send: fake.send }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

/** Wait until the committed cursor has read the provider's newest change. */
async function waitForWatermark(project: LoadedProject, fake: MailchimpAudienceFake): Promise<void> {
  const newest = `"watermark":"${new Date(fake.clockMs).toISOString().slice(0, 19)}Z"`;
  await expect.poll(async () => (await cursorState(project))?.token ?? '', { timeout: 30_000 }).toContain(newest);
}

async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<MemberChangedEvent>>, count: number): Promise<Map<string, MemberChangeOutcome>> {
  await expect.poll(() => delivered.size, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes = new Map<string, MemberChangeOutcome>();
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<MemberChangeOutcome>();
    outcomes.set(outcome.emailAddress, outcome);
  }
  return outcomes;
}

const newAudience = () => new MailchimpAudienceFake(`it${Date.now()}${Math.floor(Math.random() * 1000)}`);

it('starts one Flow per contact change across a restart with real Dex', async () => {
  const fake = newAudience(), delivered = new Map<string, TriggerEvent<MemberChangedEvent>>();
  fake.upsert('ben@example.com', 'subscribed', 'Ben'); fake.upsert('cara@example.com', 'subscribed', 'Cara');
  const first = await startProcess(fake, delivered);
  await waitForWatermark(first.project, fake);
  fake.upsert('dana@example.com', 'pending', 'Dana'); fake.upsert('ben@example.com', 'unsubscribed', 'Ben');
  first.runner.wakePoller(MemberChangedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 2);
  expect(outcomes.get('dana@example.com')?.current.status).toBe('pending');
  expect(outcomes.get('ben@example.com')?.deliveredStatus).toBe('unsubscribed');
  expect(outcomes.get('ben@example.com')?.current.mergeFields, 'the Flow reads the contact back').toEqual({ FNAME: 'Ben' });
  await first.stop();

  fake.upsert('eli@example.com', 'subscribed', 'Eli'); fake.upsert('dana@example.com', 'subscribed', 'Dana');
  const second = await startProcess(fake, delivered, first.objects);
  try {
    second.runner.wakePoller(MemberChangedTriggerBinding);
    outcomes = await waitForOutcomes(second.client, delivered, 4);
    expect(outcomes.get('eli@example.com')?.current.status).toBe('subscribed');
    expect(outcomes.get('dana@example.com')?.deliveredStatus).toBe('subscribed');
    await waitForWatermark(second.project, fake);
    const requests = fake.requests.length;
    second.runner.wakePoller(MemberChangedTriggerBinding);
    await expect.poll(() => fake.requests.length, { timeout: 30_000 }).toBeGreaterThan(requests);
    expect(delivered.size, 'no version twice and never the baseline').toBe(4);
  } finally { await second.stop(); }
}, 120_000);

it('records a new baseline after a rejected cursor query, with real Dex', async () => {
  const fake = newAudience(), delivered = new Map<string, TriggerEvent<MemberChangedEvent>>();
  fake.upsert('ben@example.com', 'subscribed', 'Ben');
  const first = await startProcess(fake, delivered);
  await waitForWatermark(first.project, fake);
  await first.stop();

  fake.upsert('gap@example.com', 'subscribed', 'Gap');
  fake.failNextList({ status: 400 });
  const second = await startProcess(fake, delivered, first.objects);
  try {
    await expect.poll(async () => (await cursorState(second.project))?.resetCount, { timeout: 30_000 }).toBe(1);
    fake.upsert('after@example.com', 'subscribed', 'After');
    second.runner.wakePoller(MemberChangedTriggerBinding);
    const outcomes = await waitForOutcomes(second.client, delivered, 1);
    expect([...outcomes.keys()], 'changes between the rejection and the reseed are not delivered').toEqual(['after@example.com']);
  } finally { await second.stop(); }
}, 120_000);

it('pauses on a 429 with Retry-After, then delivers the change once, with real Dex', async () => {
  const fake = newAudience(), delivered = new Map<string, TriggerEvent<MemberChangedEvent>>();
  const process = await startProcess(fake, delivered);
  try {
    await expect.poll(() => fake.requests.length, { timeout: 30_000 }).toBeGreaterThanOrEqual(1);
    fake.failNextList({ status: 429, retryAfter: '1' });
    fake.upsert('ben@example.com', 'subscribed', 'Ben');
    process.runner.wakePoller(MemberChangedTriggerBinding);
    const outcomes = await waitForOutcomes(process.client, delivered, 1);
    expect(outcomes.get('ben@example.com')?.current.status).toBe('subscribed');
    expect(delivered.size).toBe(1);
  } finally { await process.stop(); }
}, 120_000);
