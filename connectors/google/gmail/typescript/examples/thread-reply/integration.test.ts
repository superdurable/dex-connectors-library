// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import type { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { FakeGmail, RecordingLogger, gmailProject, replyMessage, rootMessage } from '../../test/trigger_support.js';
import { runThreadReplyApplication } from './app.js';
import { ReplyTriggerBinding, StartTriggerBinding, type GmailThreadReplyFlow, type ThreadState } from './flow/workflow.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

const replyBody = 'Your order question is resolved.';
// The completion reply that Dex Web saves for the reply Step.
const replyMessageConfiguration = [{ connectorId: 'gmail', connectionName: 'gmail-inbox', operationId: 'replyToMessage', flowType: 'GmailThreadReplyFlow', stepType: 'ReplyToReceivedEmail', configuration: { textBody: replyBody } }];

async function waitForFlowToExist(client: Client, flowId: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    try { await client.describeFlow(flowId); return; }
    catch (error) { if (Date.now() > deadline) throw error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

it('starts one Flow per Gmail root, replies after the matching reply, and consumes an orphan reply', async () => {
  const suffix = randomUUID().replaceAll('-', '').slice(0, 12);
  const thread = `thread-${suffix}`;
  const project = await gmailProject([
    { triggerName: 'messageReceived', bindingName: StartTriggerBinding, configuration: { messageMatcher: { messageContains: 'order', senderEmails: ['ada@example.com'] } } },
    { triggerName: 'replyReceived', bindingName: ReplyTriggerBinding, configuration: { replyMatcher: { messageContains: 'approved' } } },
  ], undefined, replyMessageConfiguration);
  const gmail = new FakeGmail();
  const logger = new RecordingLogger();
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; flow: GmailThreadReplyFlow }) => void;
  const ready = new Promise<{ client: Client; flow: GmailThreadReplyFlow }>(resolve => { resolveReady = resolve; });
  // A root with a non-matching sender and a reply in a thread whose Flow never started arrive first.
  gmail.receive(rootMessage(`ignored-${suffix}`, `ignored-thread-${suffix}`, { from: 'Eve <eve@example.com>' }));
  gmail.receive(replyMessage(`orphan-${suffix}`, `orphan-thread-${suffix}`));
  gmail.receive(rootMessage(`root-${suffix}`, thread));
  const running = runThreadReplyApplication(project, { serverAddress: dexAddress(), logger, gmailOptions: { transport: gmail.transport }, onReady: resolveReady }, stop.signal);
  try {
    const { client, flow } = await ready;
    const flowId = `gmail-thread-reply-gmail-inbox-${thread}`;
    await waitForFlowToExist(client, flowId);
    await expect.poll(async () => (await client.invokeRPC(flow.getThreadStatus, flowId) as unknown as ThreadState).status, { timeout: 30_000 }).toBe('waitingForReply');
    gmail.receive(replyMessage(`reply-${suffix}`, thread, { snippet: 'Not yet' }));
    gmail.receive(replyMessage(`approved-${suffix}`, thread));
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    const state = completed.singleOutput() as ThreadState;
    expect([state.replyEventId, state.replyMessageId, state.rootMessage?.messageId]).toEqual([`approved-${suffix}`, `approved-${suffix}`, `root-${suffix}`]);
    expect(gmail.sent).toHaveLength(1);
    expect(gmail.sent[0]).toContain(`In-Reply-To: <approved-${suffix}@mail.example.com>`);
    expect(gmail.sent[0]).toContain(replyBody);
    await expect.poll(() => logger.find('trigger event skipped: undeliverable', { event_id: `orphan-${suffix}` }).length, { timeout: 30_000 }).toBe(1);
    expect(logger.find('trigger event ignored', { event_id: `ignored-${suffix}`, reason: 'matcher_mismatch' })).toHaveLength(1);
    expect(logger.find('trigger event ignored', { event_id: `reply-${suffix}`, reason: 'matcher_mismatch' })).toHaveLength(1);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL|ada@example|Order question/);
  } finally {
    stop.abort();
    await running;
  }
}, 120_000);

it('retries a reply that arrives while its Flow reads the root, and absorbs the rescan after a restart', async () => {
  const suffix = randomUUID().replaceAll('-', '').slice(0, 12);
  const thread = `thread-${suffix}`;
  const project = await gmailProject([
    { triggerName: 'messageReceived', bindingName: StartTriggerBinding, configuration: {} },
    { triggerName: 'replyReceived', bindingName: ReplyTriggerBinding, configuration: {} },
  ], undefined, replyMessageConfiguration);
  const gmail = new FakeGmail();
  // The Flow's getMessage read of the root waits, so the reply from the same poll reaches a Flow still reading it.
  let releaseRootRead!: () => void;
  gmail.fullReadGates.set(`root-${suffix}`, new Promise<void>(resolve => { releaseRootRead = resolve; }));
  gmail.receive(rootMessage(`root-${suffix}`, thread));
  gmail.receive(replyMessage(`reply-${suffix}`, thread));
  const flowId = `gmail-thread-reply-gmail-inbox-${thread}`;
  const start = () => {
    const logger = new RecordingLogger();
    const stop = new AbortController();
    let resolveReady!: (client: Client) => void;
    const ready = new Promise<Client>(resolve => { resolveReady = resolve; });
    const running = runThreadReplyApplication(project, { serverAddress: dexAddress(), logger, gmailOptions: { transport: gmail.transport }, onReady: ({ client }) => resolveReady(client) }, stop.signal);
    return { logger, stop, ready, running };
  };
  const first = start();
  try {
    const client = await first.ready;
    await waitForFlowToExist(client, flowId);
    await expect.poll(() => first.logger.find('trigger delivery failed; retrying', { event_id: `reply-${suffix}`, attempt: 1 }).length, { timeout: 30_000 }).toBe(1);
    releaseRootRead();
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect((completed.singleOutput() as ThreadState).replyEventId).toBe(`reply-${suffix}`);
    await expect.poll(() => first.logger.find('trigger delivered after retry', { event_id: `reply-${suffix}` }).length, { timeout: 30_000 }).toBe(1);
    await expect.poll(() => first.logger.find('trigger event delivered', { event_id: `reply-${suffix}` }).length, { timeout: 30_000 }).toBe(1);
  } finally {
    releaseRootRead();
    first.stop.abort();
    await first.running;
  }
  // The restarted process rescans the page: the Flow start is a duplicate and the reply RPC repeats its request ID.
  const second = start();
  try {
    await second.ready;
    await expect.poll(() => second.logger.find('trigger event delivered', { event_id: `root-${suffix}`, duplicate: true }).length, { timeout: 30_000 }).toBe(1);
    // The reply keeps its RPC request ID, so Dex answers the redelivery from the recorded reply.
    await expect.poll(() => second.logger.find('trigger event delivered', { event_id: `reply-${suffix}`, flow_id: flowId }).length, { timeout: 30_000 }).toBe(1);
    expect(gmail.sent).toHaveLength(1);
    expect(gmail.sent[0]).toContain(replyBody);
  } finally {
    second.stop.abort();
    await second.running;
  }
}, 120_000);
