// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runNewConversationWorker } from '../../examples/new-conversation/app.js';
import { ConnectionName, ConversationCreatedTriggerBinding, resolveFlowId, type Intake } from '../../examples/new-conversation/flow/workflow.js';
import type { ConversationCreatedEvent } from '../../src/index.js';
import { FakeEmail, FakeToken, ReamazeConversationFake, newTestProject } from '../reamaze_conversations_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'reamaze', connectionName: ConnectionName, triggerName: 'conversationCreated', bindingName: ConversationCreatedTriggerBinding };
type Delivered = Map<string, TriggerEvent<ConversationCreatedEvent>>;

/** One Worker and Trigger process of the example over shared project storage, watching channel. */
async function startProcess(fake: ReamazeConversationFake, channel: string, delivered: Delivered, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [{ bindingName: ConversationCreatedTriggerBinding, configuration: channel ? { channel } : {} }], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runNewConversationWorker(project, {
    serverAddress: dexAddress(), reamazeOptions: { send: fake.send, apiBaseURL: fake.baseURL }, onReady: resolveReady,
    onDelivered: event => { if (!delivered.has(event.payload.conversation.id)) delivered.set(event.payload.conversation.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForWatermark(project: LoadedProject, milliseconds: number): Promise<void> {
  await expect.poll(async () => (await cursorState(project))?.token.includes(`"watermark":${milliseconds},`), { timeout: 30_000 }).toBe(true);
}

async function waitForIntakes(client: Client, delivered: Delivered, count: number, slugs: string[]): Promise<Map<string, Intake>> {
  await expect.poll(() => delivered.size, { timeout: 60_000 }).toBeGreaterThanOrEqual(count);
  const intakes = new Map<string, Intake>();
  for (const slug of slugs) {
    const event = delivered.get(slug);
    expect(event, `${slug} was not delivered`).toBeDefined();
    const result = await client.waitForFlow(resolveFlowId(event!), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const intake = result.singleOutput<Intake>();
    expect(intake.conversationId).toBe(slug);
    intakes.set(slug, intake);
  }
  return intakes;
}

// A unique prefix per run keeps slugs, and so Flow IDs, unique on a shared Dex Server.
const runPrefix = () => `run${Date.now().toString(36)}`;

it('starts one Flow per new conversation that is not spam across a restart, with real Dex', async () => {
  const fake = new ReamazeConversationFake(FakeEmail, FakeToken), delivered: Delivered = new Map(), prefix = runPrefix();
  const start = Date.now(), at = (offsetMs: number) => new Date(start + offsetMs), slug = (name: string) => `${prefix}-${name}`;
  fake.insert(slug('existing-1'), 'Existing', { createdAt: at(-7_200_000) });
  fake.insert(slug('existing-2'), 'Existing', { createdAt: at(-3_600_000) });
  const first = await startProcess(fake, '', delivered);
  let intakes: Map<string, Intake>;
  try {
    await waitForWatermark(first.project, start - 3_600_000);
    fake.insert(slug('refund'), 'Refund request', { createdAt: at(0) });
    fake.insert(slug('spam'), 'Cheap watches', { status: 3, createdAt: at(1_000) });
    fake.insert(slug('closed'), 'Closed on arrival', { status: 4, createdAt: at(2_000) });
    fake.rateLimitNextList('1');
    first.runner.wakePoller(ConversationCreatedTriggerBinding);
    intakes = await waitForIntakes(first.client, delivered, 3, [slug('refund'), slug('closed')]);
  } finally { await first.stop(); }
  expect(intakes.get(slug('refund'))?.subject).toBe('Refund request');
  expect(intakes.get(slug('refund'))?.messageCount, 'the Flow reads the conversation back from Re:amaze').toBe(1);
  expect(intakes.get(slug('closed'))?.status).toBe(4);
  expect(fake.listRequestCount(), 'the rate-limited poll was retried').toBeGreaterThanOrEqual(3);

  fake.insert(slug('offline'), 'Opened while stopped', { createdAt: at(3_000) });
  const second = await startProcess(fake, '', delivered, first.objects);
  try {
    second.runner.wakePoller(ConversationCreatedTriggerBinding);
    intakes = await waitForIntakes(second.client, delivered, 4, [slug('refund'), slug('closed'), slug('offline')]);
    await waitForWatermark(second.project, start + 3_000);
  } finally { await second.stop(); }
  expect(intakes.get(slug('offline'))?.subject).toBe('Opened while stopped');
  expect(delivered.size, 'spam is delivered once and starts no Flow; nothing else repeats').toBe(4);
}, 180_000);

it('records a new baseline after the binding changes channel, with real Dex', async () => {
  const fake = new ReamazeConversationFake(FakeEmail, FakeToken), delivered: Delivered = new Map(), prefix = runPrefix();
  const start = Date.now(), slug = (name: string) => `${prefix}-${name}`;
  fake.insert(slug('existing'), 'Existing', { createdAt: new Date(start - 3_600_000) });
  const first = await startProcess(fake, '', delivered);
  try { await waitForWatermark(first.project, start - 3_600_000); } finally { await first.stop(); }

  fake.insert(slug('gap'), 'Lost in the gap', { channel: 'billing', createdAt: new Date(start) });
  const second = await startProcess(fake, 'billing', delivered, first.objects);
  try {
    await expect.poll(async () => (await cursorState(second.project))?.resetCount, { timeout: 30_000 }).toBe(1);
    fake.insert(slug('after-reset'), 'After the reset', { channel: 'billing', createdAt: new Date(start + 1_000) });
    second.runner.wakePoller(ConversationCreatedTriggerBinding);
    const intakes = await waitForIntakes(second.client, delivered, 1, [slug('after-reset')]);
    expect(intakes.get(slug('after-reset'))?.channel).toBe('billing');
    expect(delivered.size, 'the conversation before the new baseline is not delivered').toBe(1);
  } finally { await second.stop(); }
}, 180_000);
