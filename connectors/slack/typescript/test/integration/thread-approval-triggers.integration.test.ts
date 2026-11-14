// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import type { Client } from '@superdurable/dex';
import { CredentialMaterial, InMemoryObjectStore, LoadedProject, ConnectionStore } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { ConnectionName, ReplyTriggerBinding, StartTriggerBinding, type ThreadApprovalFlow, type ThreadState } from '../../examples/thread-approval/flow/workflow.js';
import { runThreadApprovalApplication } from '../../examples/thread-approval/app.js';
import { FakeSocket, RelayHost, eventsEnvelope, eventsPayload, newRecordingLogger, postRelayed, replyMessage, rootMessage } from '../trigger_support.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const timings = { reconnectDelayMs: 5, reconnectMaxDelayMs: 20, readTimeoutMs: 60_000 };
const bindings = (channelId: string) => [
  { connectorId: 'slack', connectionName: ConnectionName, triggerName: 'channelThreadCreated', bindingName: StartTriggerBinding, configuration: { channelId, threadTriggerMatcher: { messageContains: 'request' } } },
  { connectorId: 'slack', connectionName: ConnectionName, triggerName: 'threadReplyCreated', bindingName: ReplyTriggerBinding, configuration: { channelId, threadReplyMatcher: { messageContains: 'approved', posterUserIds: ['U2'] } } },
];

/** Fake Slack Web API recording the completion posts. */
function newSlackWebAPI(channelId: string) {
  const posts: Array<Record<string, unknown>> = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const path = new URL(request.url).pathname;
    const json = (body: unknown): ProviderResponse => ({ status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) });
    if (path.endsWith('/apps.connections.open')) return json({ ok: true, url: 'wss://wss-primary.slack.com/link/?ticket=SENTINEL-TICKET' });
    if (path.endsWith('/conversations.replies')) return json({ ok: true, messages: [{ ts: '1.0', user: 'U1', text: 'please request approval' }] });
    if (path.endsWith('/chat.postMessage')) {
      const payload = JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as Record<string, unknown>;
      posts.push(payload);
      return json({ ok: true, channel: channelId, ts: '1.9', message: { ts: '1.9', thread_ts: payload.thread_ts, text: payload.text } });
    }
    return { status: 404, headers: {}, body: Buffer.from('{"ok":false,"error":"unknown_method"}') };
  };
  return { send, posts };
}

async function waitForStatus(client: Client, flow: ThreadApprovalFlow, flowId: string, status: string): Promise<ThreadState> {
  const deadline = Date.now() + 25_000;
  let last: unknown;
  for (;;) {
    try {
      const state = await client.invokeRPC(flow.getThreadStatus, flowId) as unknown as ThreadState;
      last = state;
      if (state.status === status) return state;
    } catch (error) { last = error; if (Date.now() > deadline) throw error; }
    if (Date.now() > deadline) throw new Error(`Flow ${flowId} did not reach ${status}: ${JSON.stringify(last)}`);
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

async function waitForFlowToExist(client: Client, flowId: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    try { await client.describeFlow(flowId); return; }
    catch (error) { if (Date.now() > deadline) throw error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

function startApplication(project: LoadedProject, slackOptions: Parameters<typeof runThreadApprovalApplication>[1]['slackOptions']) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveReady!: (ready: { client: Client; flow: ThreadApprovalFlow; relayedTriggerURL: string }) => void;
  const ready = new Promise<{ client: Client; flow: ThreadApprovalFlow; relayedTriggerURL: string }>(resolve => { resolveReady = resolve; });
  const running = runThreadApprovalApplication(project, { serverAddress: dexAddress(), triggerHost: '127.0.0.1', triggerPort: 0, logger, ...(slackOptions ? { slackOptions } : {}), onReady: resolveReady }, stop.signal);
  return { stop, logger, ready, running };
}

it('starts one Flow per Socket Mode root, approves it from a reply, and consumes an orphan reply', async () => {
  const channelId = `C${randomUUID().replaceAll('-', '').slice(0, 10).toUpperCase()}`;
  const objects = new InMemoryObjectStore();
  const scope = { projectId: 'socket', kind: 'live' } as const;
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'slack', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ bot_token: 'xoxb-SENTINEL-BOT', user_token: 'xoxp-SENTINEL-USER', app_token: 'xapp-SENTINEL-APP' }, undefined, 'oauth2'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'slack', connectionName: ConnectionName, modulePath: 'slack', provider: 'slack', configuration: { endpoint: 'http://127.0.0.1:9/api' } }], triggerBindings: bindings(channelId) }, undefined, objects, undefined, undefined);
  const slack = newSlackWebAPI(channelId);
  const socket = new FakeSocket();
  const application = startApplication(project, { send: slack.send, socketDialer: async () => socket, triggerTimings: timings });
  try {
    const { client } = await application.ready;
    const root = { channel: channelId };
    socket.push(eventsEnvelope('e1', `EvRoot-${channelId}`, rootMessage('1.0', root)));
    socket.push(eventsEnvelope('e2', `EvRoot-${channelId}`, rootMessage('1.0', root)));
    socket.push(eventsEnvelope('e3', `EvReply-${channelId}`, replyMessage('1.1', '1.0', { ...root, text: 'approved' })));
    socket.push(eventsEnvelope('e4', `EvReply-${channelId}`, replyMessage('1.1', '1.0', { ...root, text: 'approved' })));
    socket.push(eventsEnvelope('e5', `EvOrphan-${channelId}`, replyMessage('7.1', '7.0', { ...root, text: 'approved' })));
    const flowId = `slack-thread-approval-T1-${channelId}-1.0`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect((completed.singleOutput() as ThreadState).replyEventId).toBe(`EvReply-${channelId}`);
    await expect.poll(() => application.logger.find('trigger event skipped: undeliverable', { event_id: `EvOrphan-${channelId}` }).length, { timeout: 30_000 }).toBe(1);
    expect(slack.posts).toEqual([expect.objectContaining({ channel: channelId, thread_ts: '1.0', text: 'Processing complete.' })]);
    expect(socket.acknowledged).toEqual(['e1', 'e2', 'e3', 'e4', 'e5']);
    expect(JSON.stringify(application.logger.records)).not.toMatch(/SENTINEL/);
  } finally {
    application.stop.abort();
    await application.running;
  }
}, 120_000);

it('starts one Flow per relayed event and approves it from a relayed reply, refusing a forged callback', async () => {
  const channelId = `C${randomUUID().replaceAll('-', '').slice(0, 10).toUpperCase()}`;
  const host = new RelayHost(undefined, { connectionName: ConnectionName, startBinding: StartTriggerBinding, replyBinding: ReplyTriggerBinding, slackEndpoint: 'http://127.0.0.1:9/api', triggerBindings: bindings(channelId) });
  const slack = newSlackWebAPI(channelId);
  const application = startApplication(host.project, { send: slack.send, triggerTimings: timings });
  try {
    const { client, relayedTriggerURL } = await application.ready;
    await expect.poll(() => host.registrations.length, { timeout: 30_000 }).toBe(1);
    const root = eventsPayload(`EvRoot-${channelId}`, rootMessage('1.0', { channel: channelId }));
    expect(await postRelayed(relayedTriggerURL, root, new Date(), 'forged-token')).toBe(400);
    expect(await postRelayed(relayedTriggerURL, root)).toBe(200);
    expect(await postRelayed(relayedTriggerURL, root)).toBe(200);
    const flowId = `slack-thread-approval-T1-${channelId}-1.0`;
    const { flow } = await application.ready;
    await waitForStatus(client, flow, flowId, 'waitingForReply');
    expect(await postRelayed(relayedTriggerURL, eventsPayload(`EvReply-${channelId}`, replyMessage('1.1', '1.0', { channel: channelId, text: 'approved' })))).toBe(200);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(slack.posts).toHaveLength(1);
    expect(host.credentialReads).toBeGreaterThanOrEqual(1);
    await expect.poll(() => host.pendingEventIds()).toEqual([]);
    expect(JSON.stringify(application.logger.records)).not.toMatch(/SENTINEL/);
  } finally {
    application.stop.abort();
    await application.running;
  }
}, 120_000);
