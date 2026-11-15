// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Registry, Worker as DexWorker, openBlobCache, type Client, type Worker } from '@superdurable/dex';
import { CredentialMaterial, ConnectionStore, InMemoryObjectStore, LoadedProject, TriggerInbox } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { afterEach, expect, it } from 'vitest';
import { runButtonApprovalApplication } from '../../examples/button-approval/app.js';
import { ButtonTriggerBinding, ConnectionName, StartTriggerBinding, type ApprovalState, type ButtonApprovalFlow } from '../../examples/button-approval/flow/workflow.js';
import { buttonClickPayload, interactiveEnvelope } from '../button_support.js';
import { FakeSocket, eventsEnvelope, newRecordingLogger, rootMessage } from '../trigger_support.js';

/** Slack's interactive acknowledgement deadline. */
const acknowledgementLimitMs = 3_000;
const scope = { projectId: 'buttons', kind: 'live' } as const;

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

/**
 * Fake Slack Web API recording every post. A button message in thread T gets timestamp T + "01", and a
 * decision reply T + "02", so every approval has its own message timestamps.
 */
function newSlackWebAPI(channelId: string) {
  const posts: Array<Record<string, unknown>> = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const path = new URL(request.url).pathname;
    const json = (body: unknown): ProviderResponse => ({ status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) });
    if (path.endsWith('/apps.connections.open')) return json({ ok: true, url: 'wss://wss-primary.slack.com/link/?ticket=SENTINEL-TICKET' });
    if (path.endsWith('/chat.postMessage')) {
      const payload = JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as Record<string, unknown>;
      posts.push(payload);
      const ts = `${String(payload.thread_ts)}${payload.blocks ? '01' : '02'}`;
      return json({ ok: true, channel: channelId, ts, message: { ts, thread_ts: payload.thread_ts, user: 'UBOT', text: payload.text } });
    }
    return { status: 404, headers: {}, body: Buffer.from('{"ok":false,"error":"unknown_method"}') };
  };
  const repliesIn = (threadTimestamp: string) => posts.filter(post => post.thread_ts === threadTimestamp && !post.blocks).map(post => post.text);
  return { send, posts, repliesIn };
}

/** One running button-approval application on a real Dex server, with a fake Slack socket and Web API. */
interface ButtonApprovalSetup {
  channelId: string; socket: FakeSocket; slack: ReturnType<typeof newSlackWebAPI>; logger: ReturnType<typeof newRecordingLogger>;
  client: Client; flow: ButtonApprovalFlow; worker: Worker;
  /** Event IDs still in the button binding's durable inbox. */ pendingClickIds(): Promise<string[]>;
  /** Stop the running Worker, as a deploy or crash would. */ stopWorker(): Promise<void>;
  /** Start a replacement Worker for the same Flow. */ startWorker(): Promise<void>;
  /** Push an envelope and return how long Slack waited for its acknowledgement. */ send(envelope: Record<string, unknown>): Promise<number>;
  /** Stop a Flow that the test leaves waiting. */ stopFlowAtCleanup(flowId: string): void;
}

const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup();
});

async function startButtonApproval(): Promise<ButtonApprovalSetup> {
  const channelId = `C${randomUUID().replaceAll('-', '').slice(0, 10).toUpperCase()}`;
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'slack', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ bot_token: 'xoxb-SENTINEL-BOT', user_token: 'xoxp-SENTINEL-USER', app_token: 'xapp-SENTINEL-APP' }, undefined, 'oauth2'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'slack', connectionName: ConnectionName, modulePath: 'slack', provider: 'slack', configuration: { endpoint: 'http://127.0.0.1:9/api' } }],
    triggerBindings: [
      { connectorId: 'slack', connectionName: ConnectionName, triggerName: 'channelThreadCreated', bindingName: StartTriggerBinding, configuration: { channelId, threadTriggerMatcher: { messageContains: 'request approval' } } },
      { connectorId: 'slack', connectionName: ConnectionName, triggerName: 'buttonClicked', bindingName: ButtonTriggerBinding, configuration: { channelId, clickerUserIds: ['U2'] } },
    ] }, undefined, objects, undefined, undefined);
  const slack = newSlackWebAPI(channelId);
  const socket = new FakeSocket();
  const logger = newRecordingLogger();
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; flow: ButtonApprovalFlow; worker: Worker }) => void;
  const ready = new Promise<{ client: Client; flow: ButtonApprovalFlow; worker: Worker }>(resolve => { resolveReady = resolve; });
  const running = runButtonApprovalApplication(project, { serverAddress: dexAddress(), logger, onReady: resolveReady,
    slackOptions: { send: slack.send, socketDialer: async () => socket, triggerTimings: { reconnectDelayMs: 5, reconnectMaxDelayMs: 20, readTimeoutMs: 60_000 } } }, stop.signal);
  const replacements: Array<{ worker: Worker; close(): Promise<void> }> = [];
  const flowsToStop: string[] = [];
  cleanups.push(async () => {
    stop.abort();
    await running;
    for (const replacement of replacements) await replacement.close();
  });
  const { client, flow, worker } = await ready;
  cleanups.push(async () => {
    for (const flowId of flowsToStop) await client.stopFlow(flowId).catch(() => undefined);
  });
  const clickInbox = new TriggerInbox(objects, scope, { connectorId: 'slack', connectionName: ConnectionName, triggerName: 'buttonClicked', bindingName: ButtonTriggerBinding });
  return {
    channelId, socket, slack, logger, client, flow, worker,
    pendingClickIds: async () => (await clickInbox.pending()).map(event => event.id),
    stopWorker: () => worker.close(),
    startWorker: async () => {
      const directory = await mkdtemp(join(tmpdir(), 'dex-slack-button-approval-test-'));
      const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
      const replacement = new DexWorker(new Registry([flow]), cache, { serverAddress: dexAddress() });
      replacements.push({ worker: replacement, close: async () => { await replacement.close(); cache.close(); await rm(directory, { recursive: true, force: true }); } });
      await replacement.start();
      await replacement.waitPullRegistered({ timeoutMs: 30_000 });
    },
    send: async envelope => {
      const sentAt = Date.now();
      const envelopeId = String(envelope.envelope_id);
      socket.push(envelope);
      await expect.poll(() => socket.acknowledged.includes(envelopeId), { interval: 5, timeout: 10_000 }).toBe(true);
      return Date.now() - sentAt;
    },
    stopFlowAtCleanup: flowId => { flowsToStop.push(flowId); },
  };
}

async function waitForStatus(setup: ButtonApprovalSetup, flowId: string, status: string): Promise<ApprovalState> {
  const deadline = Date.now() + 30_000;
  let last: unknown;
  for (;;) {
    try {
      const state = await setup.client.invokeRPC(setup.flow.getApprovalStatus, flowId) as unknown as ApprovalState;
      last = state;
      if (state.status === status) return state;
    } catch (error) { last = error; }
    if (Date.now() > deadline) throw new Error(`Flow ${flowId} did not reach ${status}: ${last instanceof Error ? last.message : JSON.stringify(last)}`);
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

const approvalFlowId = (channelId: string, threadTimestamp: string) => `slack-button-approval-T1-${channelId}-${threadTimestamp}`;
const clickId = (channelId: string, messageTimestamp: string, actionId: string, actionTimestamp: string) => `T1:${channelId}:${messageTimestamp}:${actionId}:${actionTimestamp}`;

it('decides once through the shared socket: filters a non-decider, lets a copied button reach the RPC without deciding, and consumes a late click', async () => {
  const setup = await startButtonApproval();
  const { channelId, socket, slack, logger } = setup;
  const flowId = approvalFlowId(channelId, '10.0');
  await setup.send(eventsEnvelope('e-root', `EvRoot-${channelId}`, rootMessage('10.0', { channel: channelId })));
  const waiting = await waitForStatus(setup, flowId, 'waitingForDecision');
  const buttonTimestamp = '10.001';
  expect(waiting.buttonMessageTimestamp).toBe(buttonTimestamp);
  expect(slack.posts[0]).toEqual({ channel: channelId, thread_ts: '10.0', text: 'Approve this request?', client_msg_id: expect.any(String), blocks: [
    { type: 'section', text: { type: 'mrkdwn', text: 'Approve this request?' } },
    { type: 'actions', elements: [
      { type: 'button', action_id: 'approve', text: { type: 'plain_text', text: 'Approve', emoji: true }, value: '10.0', style: 'primary' },
      { type: 'button', action_id: 'reject', text: { type: 'plain_text', text: 'Reject', emoji: true }, value: '10.0', style: 'danger' },
    ] },
  ] });

  // The click of a member who may not decide is filtered by the connector without reaching the Flow.
  await setup.send(interactiveEnvelope('e-other', buttonClickPayload(channelId, buttonTimestamp, '10.0', 'U3', 'approve', '10.0', '11.0')));
  expect(logger.find('trigger event ignored', { envelope_id: 'e-other', reason: 'clicker_mismatch' })).toHaveLength(1);
  // A matching button on another message in the thread, such as a copy, reaches the RPC but does not decide.
  const foreignClickId = clickId(channelId, '10.999999', 'approve', '11.5');
  await setup.send(interactiveEnvelope('e-foreign', buttonClickPayload(channelId, '10.999999', '10.0', 'U2', 'approve', '10.0', '11.5')));
  await expect.poll(() => logger.find('trigger event delivered', { event_id: foreignClickId, flow_id: flowId }).length, { timeout: 20_000 }).toBe(1);
  expect((await waitForStatus(setup, flowId, 'waitingForDecision')).decision).toBeUndefined();

  await setup.send(interactiveEnvelope('e-reject', buttonClickPayload(channelId, buttonTimestamp, '10.0', 'U2', 'reject', '10.0', '12.0')));
  const completed = await setup.client.waitForFlow(flowId, 60_000);
  expect(completed.status, completed.errorMessage).toBe('completed');
  expect(completed.singleOutput()).toMatchObject({ status: 'completed', decision: 'rejected', deciderUserId: 'U2', decisionEventId: clickId(channelId, buttonTimestamp, 'reject', '12.0') });
  expect(slack.repliesIn('10.0')).toEqual(['Rejected by <@U2>.']);

  // A later click on the completed approval is consumed as undeliverable and does not stop the shared socket.
  const lateClickId = clickId(channelId, buttonTimestamp, 'approve', '13.0');
  await setup.send(interactiveEnvelope('e-late', buttonClickPayload(channelId, buttonTimestamp, '10.0', 'U2', 'approve', '10.0', '13.0')));
  await expect.poll(() => logger.find('trigger event skipped: undeliverable', { event_id: lateClickId }).length, { timeout: 20_000 }).toBe(1);
  const secondFlowId = approvalFlowId(channelId, '20.0');
  setup.stopFlowAtCleanup(secondFlowId);
  await setup.send(eventsEnvelope('e-root-2', `EvRoot2-${channelId}`, rootMessage('20.0', { channel: channelId })));
  await waitForStatus(setup, secondFlowId, 'waitingForDecision');
  expect(slack.repliesIn('10.0'), 'the late click posts nothing').toEqual(['Rejected by <@U2>.']);
  expect(await setup.pendingClickIds()).toEqual([]);
  expect(socket.acknowledged).toEqual(['e-root', 'e-other', 'e-foreign', 'e-reject', 'e-late', 'e-root-2']);
  expect(logger.find('slack socket mode connected'), 'message and button routes share one connection').toHaveLength(1);
  expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
}, 120_000);

it('acknowledges a click and a later root at once while the Worker is down, and completes from the recorded click after it returns', async () => {
  const setup = await startButtonApproval();
  const { channelId, slack, logger } = setup;
  const flowId = approvalFlowId(channelId, '30.0');
  await setup.send(eventsEnvelope('e-root', `EvRoot-${channelId}`, rootMessage('30.0', { channel: channelId })));
  const { buttonMessageTimestamp } = await waitForStatus(setup, flowId, 'waitingForDecision');
  expect(buttonMessageTimestamp).toBe('30.001');

  await setup.stopWorker();
  const approveClickId = clickId(channelId, '30.001', 'approve', '31.0');
  expect(await setup.send(interactiveEnvelope('e-approve', buttonClickPayload(channelId, '30.001', '30.0', 'U2', 'approve', '30.0', '31.0')))).toBeLessThan(acknowledgementLimitMs);
  await expect.poll(() => logger.find('trigger delivery failed; retrying', { event_id: approveClickId }).length, { timeout: 30_000 }).toBeGreaterThan(0);
  expect(await setup.pendingClickIds(), 'the acknowledged click is recorded in its durable inbox').toEqual([approveClickId]);
  // The read loop keeps reading while that delivery retries.
  const laterFlowId = approvalFlowId(channelId, '40.0');
  const laterEventId = `EvRoot2-${channelId}`;
  setup.stopFlowAtCleanup(laterFlowId);
  expect(await setup.send(eventsEnvelope('e-root-2', laterEventId, rootMessage('40.0', { channel: channelId })))).toBeLessThan(acknowledgementLimitMs);

  await setup.startWorker();
  const completed = await setup.client.waitForFlow(flowId, 60_000);
  expect(completed.status, completed.errorMessage).toBe('completed');
  expect(completed.singleOutput()).toMatchObject({ decision: 'approved', deciderUserId: 'U2', decisionEventId: approveClickId });
  expect(slack.repliesIn('30.0')).toEqual(['Approved by <@U2>.']);
  await expect.poll(() => logger.find('trigger event delivered', { event_id: laterEventId, flow_id: laterFlowId }).length, { timeout: 20_000 }).toBe(1);
  await waitForStatus(setup, laterFlowId, 'waitingForDecision');
  expect(logger.find('trigger delivered after retry', { event_id: approveClickId })).toHaveLength(1);
  expect(logger.find('slack interactive envelope acknowledged late')).toEqual([]);
  await expect.poll(() => setup.pendingClickIds(), { timeout: 10_000 }).toEqual([]);
  expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
}, 180_000);
