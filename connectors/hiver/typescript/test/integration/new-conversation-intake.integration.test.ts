// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runConversationIntakeWorker } from '../../examples/new-conversation-intake/app.js';
import { ConnectionName, ConversationReceivedTriggerBinding, resolveFlowId, type IntakeOutcome } from '../../examples/new-conversation-intake/flow/workflow.js';
import type { ConversationReceivedEvent } from '../../src/index.js';
import { APIBaseURL, HiverConversationsFake, newTestProject } from '../hiver_conversations_fake.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const cursorKey = { connectorId: 'hiver', connectionName: ConnectionName, triggerName: 'conversationReceived', bindingName: ConversationReceivedTriggerBinding };

/** One Worker and Trigger process of the example over shared project storage, polling inboxId. */
async function startProcess(fake: HiverConversationsFake, inboxId: string, delivered: Map<string, TriggerEvent<ConversationReceivedEvent>>, objects?: InMemoryObjectStore) {
  const { project, objects: storage } = await newTestProject(ConnectionName, [{ bindingName: ConversationReceivedTriggerBinding, configuration: { inboxId } }], objects);
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runConversationIntakeWorker(project, {
    serverAddress: dexAddress(), hiverOptions: { send: fake.send, apiBaseURL: APIBaseURL }, onReady: resolveReady,
    onFlowStarted: event => { if (!delivered.has(event.id)) delivered.set(event.id, event); },
  }, stop.signal);
  const started = await Promise.race([ready, running.then(() => { throw new Error('the worker stopped before it was ready'); })]);
  return { ...started, project, objects: storage, stop: async () => { stop.abort(); await running; } };
}

async function cursorState(project: LoadedProject) { return (await project.pollCursorStore(cursorKey).readPollCursor()).state; }

async function waitForBaseline(project: LoadedProject, inboxId: string, resetCount = 0): Promise<void> {
  await expect.poll(async () => {
    const state = await cursorState(project);
    return state?.resetCount === resetCount && state.token.includes(`"inboxId":"${inboxId}","baselineComplete":true`);
  }, { timeout: 30_000 }).toBe(true);
}

async function waitForOutcomes(client: Client, delivered: Map<string, TriggerEvent<ConversationReceivedEvent>>, count: number): Promise<Map<string, IntakeOutcome>> {
  await expect.poll(() => delivered.size, { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
  const outcomes = new Map<string, IntakeOutcome>();
  for (const event of delivered.values()) {
    const result = await client.waitForFlow(resolveFlowId(event), 30_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<IntakeOutcome>();
    expect(outcome.conversationId).toBe(event.payload.conversation.id);
    outcomes.set(outcome.conversationId, outcome);
  }
  return outcomes;
}

// A unique inbox per run keeps Flow IDs unique on a shared Dex Server.
const uniqueInboxId = () => String(100_000 + (Date.now() % 900_000_000));

it('starts one Flow per new conversation across a restart whose first request answers 429, with real Dex', async () => {
  const inboxId = uniqueInboxId();
  const fake = new HiverConversationsFake(inboxId, 'support@acme.example.com'), delivered = new Map<string, TriggerEvent<ConversationReceivedEvent>>();
  fake.addConversations(inboxId, '573741001', '573741002');
  const first = await startProcess(fake, inboxId, delivered);
  await waitForBaseline(first.project, inboxId);
  fake.addConversations(inboxId, '573741003', '573741004');
  first.runner.wakePoller(ConversationReceivedTriggerBinding);
  let outcomes = await waitForOutcomes(first.client, delivered, 2);
  expect([...outcomes.keys()].sort(), 'the baseline starts no Flow').toEqual(['573741003', '573741004']);
  const outcome = outcomes.get('573741003')!;
  expect([outcome.inboxId, outcome.status, outcome.privatePermalink]).toEqual([inboxId, 'open', 'https://app.hiverhq.com/conversation/573741003']);
  expect(outcome.messages.map(message => message.hiverMessageId)).toEqual(['8573741003']);
  await first.stop();

  fake.addConversations(inboxId, '573741005');
  fake.rateLimitNext(1);
  const second = await startProcess(fake, inboxId, delivered, first.objects);
  try {
    second.runner.wakePoller(ConversationReceivedTriggerBinding);
    outcomes = await waitForOutcomes(second.client, delivered, 3);
    expect([...outcomes.keys()].sort()).toEqual(['573741003', '573741004', '573741005']);
    expect(delivered.size, 'no conversation twice and never the baseline').toBe(3);
  } finally { await second.stop(); }
}, 120_000);

it('records a new baseline after the binding is re-pointed at another inbox, with real Dex', async () => {
  const firstInbox = uniqueInboxId(), secondInbox = String(Number(firstInbox) + 1);
  const fake = new HiverConversationsFake(firstInbox, 'support@acme.example.com'), delivered = new Map<string, TriggerEvent<ConversationReceivedEvent>>();
  fake.addConversations(firstInbox, '573741001');
  const first = await startProcess(fake, firstInbox, delivered);
  await waitForBaseline(first.project, firstInbox);
  await first.stop();

  fake.addInbox(secondInbox, 'billing@acme.example.com');
  fake.addConversations(secondInbox, '673741001');
  const second = await startProcess(fake, secondInbox, delivered, first.objects);
  try {
    await waitForBaseline(second.project, secondInbox, 1);
    fake.addConversations(secondInbox, '673741002');
    second.runner.wakePoller(ConversationReceivedTriggerBinding);
    const outcomes = await waitForOutcomes(second.client, delivered, 1);
    expect([...outcomes.keys()], "the new inbox's existing conversations are its baseline").toEqual(['673741002']);
    expect(outcomes.get('673741002')?.inboxId).toBe(secondInbox);
  } finally { await second.stop(); }
}, 120_000);
