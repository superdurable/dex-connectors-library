// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomUUID } from 'node:crypto';
import { Client } from '@superdurable/dex';
import { type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runTaskEventApp } from '../../examples/task-event/app.js';
import { BindingName, ConnectionName, resolveFlowId } from '../../examples/task-event/flow/workflow.js';
import type { TaskEvent } from '../../src/index.js';

const secret = 'SENTINEL-CLICKUP-SIGNING-SECRET';
const inboxKey = { connectorId: 'clickup', connectionName: ConnectionName, triggerName: 'taskEvent', bindingName: BindingName };

function dexAddress(): string {
  if (!process.env.DEX_FLOW_SERVICE_ADDRESS) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return process.env.DEX_FLOW_SERVICE_ADDRESS;
}

function logger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); } };
}

async function projectFor(restartedObjects?: InMemoryObjectStore) {
  const scope = { projectId: 'clickup-ts-integration', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'clickup', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ api_token: 'pk_STANDIN0123456789', webhook_secret: secret }, undefined, 'api-key'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'clickup', connectionName: ConnectionName, modulePath: 'clickup', provider: 'clickup', configuration: {} }],
    triggerBindings: [{ connectorId: 'clickup', connectionName: ConnectionName, triggerName: 'taskEvent', bindingName: BindingName, configuration: { events: ['taskStatusUpdated'] } }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}

function startApp(project: LoadedProject, serverAddress: string) {
  const stop = new AbortController();
  const records = logger();
  let listeningResolve!: (url: string) => void;
  let readyResolve!: (ready: { client: Client }) => void;
  const listening = new Promise<string>(resolve => { listeningResolve = resolve; });
  const ready = new Promise<{ client: Client }>(resolve => { readyResolve = resolve; });
  const running = runTaskEventApp(project, { serverAddress, webhookHost: '127.0.0.1', webhookPort: 0, logger: records,
    onListening: listeningResolve, onReady: readyResolve }, stop.signal);
  return { stop, records, listening, ready, running };
}

function bodyFor(historyId: string, event = 'taskStatusUpdated'): string {
  return JSON.stringify({ event, webhook_id: 'webhook-01', task_id: '86b2x4k7q', history_items: [{ id: historyId, field: 'status', date: '1642734631523', before: { status: 'in progress' }, after: { status: 'blocked' } }] });
}

async function deliver(url: string, body: string, signatureBody = body): Promise<number> {
  const signature = createHmac('sha256', secret).update(signatureBody).digest('hex');
  const response = await fetch(url, { method: 'POST', body, headers: { 'X-Signature': signature } });
  await response.text();
  return response.status;
}

async function waitReady(url: string): Promise<void> {
  await expect.poll(async () => (await fetch(url.replace('/webhooks/clickup', '/readyz'))).status, { timeout: 30_000 }).toBe(200);
}

async function waitForFlow(client: Client, flowId: string): Promise<void> {
  await expect.poll(async () => { try { await client.describeFlow(flowId); return true; } catch { return false; } }, { timeout: 30_000 }).toBe(true);
}

it('delivers one signed task status event to a real Dex Flow and deduplicates redelivery', async () => {
  const { project } = await projectFor();
  const app = startApp(project, dexAddress());
  try {
    const url = await app.listening;
    await waitReady(url);
    const { client } = await app.ready;
    const historyId = randomUUID().replaceAll('-', '').slice(0, 16);
    const body = bodyFor(historyId);
    expect(await deliver(url, body, body.replace('blocked', 'complete'))).toBe(400);
    expect(await deliver(url, body)).toBe(200);
    const eventId = `webhook-01:taskStatusUpdated:${historyId}`;
    const flowId = resolveFlowId({ id: eventId, occurredAt: new Date(), payload: {} as TaskEvent });
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<TaskEvent>()).toMatchObject({ eventId, event: 'taskStatusUpdated', taskId: '86b2x4k7q' });
    expect(await deliver(url, body)).toBe(200);
    await expect.poll(() => app.records.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate === true).length, { timeout: 30_000 }).toBe(1);
    expect(JSON.stringify(app.records.records)).not.toContain(secret);
    await expect.poll(async () => (await project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 120_000);

it('records while Dex is unavailable and replays the event to a real Flow after restart', async () => {
  const { project, objects } = await projectFor();
  const historyId = randomUUID().replaceAll('-', '').slice(0, 16);
  const body = bodyFor(historyId);
  const offline = startApp(project, '127.0.0.1:9');
  try {
    const url = await offline.listening;
    await waitReady(url);
    expect(await deliver(url, body)).toBe(200);
    expect((await project.triggerInbox(inboxKey).pending()).map(event => event.id)).toEqual([`webhook-01:taskStatusUpdated:${historyId}`]);
  } finally { offline.stop.abort(); await offline.running.catch(() => undefined); }
  const restarted = await projectFor(objects);
  const online = startApp(restarted.project, dexAddress());
  try {
    const { client } = await online.ready;
    const flowId = `clickup-webhook-01-taskStatusUpdated-${historyId}`;
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await restarted.project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { online.stop.abort(); await online.running; }
}, 120_000);
