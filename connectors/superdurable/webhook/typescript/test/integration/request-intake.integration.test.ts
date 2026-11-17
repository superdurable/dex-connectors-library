// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runWebhookRequestIntake } from '../../examples/request-intake/app.js';
import { ConnectionName, SubmissionBinding, type RecordedRequest } from '../../examples/request-intake/flow.js';
import { hmac, newTestProject, post } from '../trigger_support.js';

const serverAddress = () => {
  if (!process.env.DEX_FLOW_SERVICE_ADDRESS) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return process.env.DEX_FLOW_SERVICE_ADDRESS;
};
const signedPost = (url: string, id: string, kind = 'form_response') => { const body = JSON.stringify({ event_id: id, event_type: kind }); return post(url, body, { 'X-Signature-256': hmac(body) }); };
const key = { connectorId: 'webhook', connectionName: ConnectionName, triggerName: 'requestReceived', bindingName: SubmissionBinding };

function start(project: Awaited<ReturnType<typeof newTestProject>>['project'], address: string) {
  const stop = new AbortController();
  let resolveListening!: (url: string) => void;
  let resolveReady!: (value: { client: Client }) => void;
  const listening = new Promise<string>(resolve => { resolveListening = resolve; });
  const ready = new Promise<{ client: Client }>(resolve => { resolveReady = resolve; });
  const running = runWebhookRequestIntake(project, { serverAddress: address, webhookHost: '127.0.0.1', webhookPort: 0,
    logger: { isEnabled: () => false, log() {} }, onListening: resolveListening, onReady: resolveReady }, stop.signal);
  return { stop, listening, ready, running };
}
async function untilReady(url: string) {
  await expect.poll(async () => (await fetch(url.replace('/webhooks/receiver', '/readyz'))).status, { timeout: 30_000 }).toBe(200);
  return url;
}

it('delivers one verified request and deduplicates redelivery', async () => {
  const { project } = await newTestProject(ConnectionName, { eventIdPointer: '/event_id' }, { matchPointer: '/event_type', matchValues: ['form_response'] }, undefined, SubmissionBinding);
  const app = start(project, serverAddress());
  const id = randomUUID();
  const flowId = `webhook-request-${Buffer.from(id).toString('hex')}`;
  try {
    const url = await untilReady(await app.listening);
    const { client } = await app.ready;
    expect((await signedPost(url, id)).status).toBe(200);
    await expect.poll(async () => { try { await client.describeFlow(flowId); return true; } catch { return false; } }, { timeout: 30_000 }).toBe(true);
    const completed = await client.waitForFlow(flowId, 30_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<RecordedRequest>()).toMatchObject({ eventId: id, contentType: 'application/json' });
    expect((await signedPost(url, id)).status).toBe(200);
    expect((await signedPost(url, randomUUID(), 'other')).status).toBe(200);
    await expect.poll(async () => (await project.triggerInbox(key).pending()).length).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 90_000);

it('acknowledges while Dex is offline and replays after restart', async () => {
  const { project, objects } = await newTestProject(ConnectionName, { eventIdPointer: '/event_id' }, {}, undefined, SubmissionBinding);
  const offline = start(project, '127.0.0.1:9');
  const id = randomUUID();
  try {
    const url = await untilReady(await offline.listening);
    expect((await signedPost(url, id)).status).toBe(200);
    expect((await project.triggerInbox(key).pending()).map(event => event.id)).toEqual([id]);
  } finally { offline.stop.abort(); await offline.running.catch(() => undefined); }
  const restarted = (await newTestProject(ConnectionName, { eventIdPointer: '/event_id' }, {}, objects, SubmissionBinding)).project;
  const app = start(restarted, serverAddress());
  try {
    await untilReady(await app.listening);
    const { client } = await app.ready;
    const flowId = `webhook-request-${Buffer.from(id).toString('hex')}`;
    const completed = await client.waitForFlow(flowId, 30_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await restarted.triggerInbox(key).pending()).length).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 90_000);
