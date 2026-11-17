// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { Client } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { runLinearIssueIntake } from '../../examples/issue-event-intake/app.js';
import { ConnectionName, IssueCreatedBinding, type RecordedIssueEvent } from '../../examples/issue-event-intake/flow.js';
import { deliver, issueBody, newTestProject, otherTeamId, teamId } from '../trigger_support.js';

const serverAddress = () => {
  if (!process.env.DEX_FLOW_SERVICE_ADDRESS) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return process.env.DEX_FLOW_SERVICE_ADDRESS;
};
const key = { connectorId: 'linear', connectionName: ConnectionName, triggerName: 'issueEventReceived', bindingName: IssueCreatedBinding };

function start(project: Awaited<ReturnType<typeof newTestProject>>['project'], address: string) {
  const stop = new AbortController();
  let resolveListening!: (url: string) => void;
  let resolveReady!: (value: { client: Client }) => void;
  const listening = new Promise<string>(resolve => { resolveListening = resolve; });
  const ready = new Promise<{ client: Client }>(resolve => { resolveReady = resolve; });
  const running = runLinearIssueIntake(project, { serverAddress: address, webhookHost: '127.0.0.1', webhookPort: 0,
    logger: { isEnabled: () => false, log() {} }, onListening: resolveListening, onReady: resolveReady }, stop.signal);
  return { stop, listening, ready, running };
}
async function untilReady(url: string) {
  await expect.poll(async () => (await fetch(url.replace('/webhooks/linear', '/readyz'))).status, { timeout: 30_000 }).toBe(200);
  return url;
}

it('starts one Flow for a signed Issue, dedupes a redelivery, and filters another team', async () => {
  const { project } = await newTestProject(ConnectionName, { teamId }, undefined, undefined, IssueCreatedBinding);
  const app = start(project, serverAddress());
  try {
    const url = await untilReady(await app.listening);
    const { client } = await app.ready;
    const issueId = randomUUID();
    const body = issueBody('create', teamId, Date.now()).replace('44444444-4444-4444-8444-444444444444', issueId);
    expect((await deliver(url, body)).status).toBe(200);
    const eventId = `create:${issueId}:1791115198123`;
    const flowId = `linear-issue-${eventId.replaceAll(':', '-')}`;
    await expect.poll(async () => { try { await client.describeFlow(flowId); return true; } catch { return false; } }, { timeout: 30_000 }).toBe(true);
    const completed = await client.waitForFlow(flowId, 30_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<RecordedIssueEvent>()).toMatchObject({ eventId, issueId, teamId, title: 'Fire panel wiring' });
    expect((await deliver(url, issueBody('create', teamId, Date.now()).replace('44444444-4444-4444-8444-444444444444', issueId))).status).toBe(200);
    expect((await deliver(url, issueBody('create', otherTeamId, Date.now()))).status).toBe(200);
    await expect.poll(async () => (await project.triggerInbox(key).pending()).length).toBe(0);
    await expect(client.describeFlow(flowId)).resolves.toBeDefined();
  } finally { app.stop.abort(); await app.running; }
}, 90_000);

it('acknowledges while Dex is offline, then replays the inbox into a Flow after restart', async () => {
  const { project, objects } = await newTestProject(ConnectionName, {}, undefined, undefined, IssueCreatedBinding);
  const offline = start(project, '127.0.0.1:9');
  const issueId = randomUUID();
  const body = issueBody('create', teamId, Date.now()).replace('44444444-4444-4444-8444-444444444444', issueId);
  const eventId = `create:${issueId}:1791115198123`;
  try {
    const url = await untilReady(await offline.listening);
    expect((await deliver(url, body)).status, '200 follows durable inbox write').toBe(200);
    expect((await project.triggerInbox(key).pending()).map(event => event.id)).toEqual([eventId]);
  } finally { offline.stop.abort(); await offline.running.catch(() => undefined); }
  const restarted = (await newTestProject(ConnectionName, {}, objects, undefined, IssueCreatedBinding)).project;
  const app = start(restarted, serverAddress());
  try {
    await untilReady(await app.listening);
    const { client } = await app.ready;
    const completed = await client.waitForFlow(`linear-issue-${eventId.replaceAll(':', '-')}`, 30_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await restarted.triggerInbox(key).pending()).length).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 90_000);
