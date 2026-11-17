// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomUUID } from 'node:crypto';
import { Client } from '@superdurable/dex';
import { type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runFormResponseEventApp } from '../../examples/response-recorder/app.js';
import { BindingName, ConnectionName, resolveFlowId } from '../../examples/response-recorder/flow/workflow.js';
import type { FormResponseEvent } from '../../src/index.js';

const secret = 'SENTINEL-TYPEFORM-SIGNING-SECRET';
const inboxKey = { connectorId: 'typeform', connectionName: ConnectionName, triggerName: 'responseSubmitted', bindingName: BindingName };

function dexAddress(): string {
  if (!process.env.DEX_FLOW_SERVICE_ADDRESS) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return process.env.DEX_FLOW_SERVICE_ADDRESS;
}

function logger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); } };
}

async function projectFor(restartedObjects?: InMemoryObjectStore) {
  const scope = { projectId: 'typeform-ts-integration', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'typeform', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ access_token: 'tfp_STANDIN0123456789', webhook_secret: secret }, undefined, 'personal-access-token'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'typeform', connectionName: ConnectionName, modulePath: 'typeform', provider: 'typeform', configuration: {} }],
    triggerBindings: [{ connectorId: 'typeform', connectionName: ConnectionName, triggerName: 'responseSubmitted', bindingName: BindingName, configuration: { formId: 'lT4Z3j' } }],
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
  const running = runFormResponseEventApp(project, { serverAddress, webhookHost: '127.0.0.1', webhookPort: 0, logger: records,
    onListening: listeningResolve, onReady: readyResolve }, stop.signal);
  return { stop, records, listening, ready, running };
}

function bodyFor(token: string): string {
  return JSON.stringify({ event_id: 'hook-event', event_type: 'form_response', form_response: { form_id: 'lT4Z3j', token,
    submitted_at: '2026-09-30T11:58:59Z', answers: [{ type: 'text', text: 'Ada', field: { id: 'field1', type: 'short_text', ref: 'first_name' } }] } });
}

async function deliver(url: string, body: string, signatureBody = body): Promise<number> {
  const signature = `sha256=${createHmac('sha256', secret).update(signatureBody).digest('base64')}`;
  const response = await fetch(url, { method: 'POST', body, headers: { 'Typeform-Signature': signature } });
  await response.text();
  return response.status;
}

async function waitReady(url: string): Promise<void> {
  await expect.poll(async () => (await fetch(url.replace('/webhooks/typeform', '/readyz'))).status, { timeout: 30_000 }).toBe(200);
}

async function waitForFlow(client: Client, flowId: string): Promise<void> {
  await expect.poll(async () => { try { await client.describeFlow(flowId); return true; } catch { return false; } }, { timeout: 30_000 }).toBe(true);
}

it('delivers one signed form response to a real Dex Flow and deduplicates redelivery', async () => {
  const { project } = await projectFor();
  const app = startApp(project, dexAddress());
  try {
    const url = await app.listening;
    await waitReady(url);
    const { client } = await app.ready;
    const token = randomUUID().replaceAll('-', '').slice(0, 16);
    const body = bodyFor(token);
    expect(await deliver(url, body, body.replace('Ada', 'Eve'))).toBe(400);
    expect(await deliver(url, body)).toBe(200);
    const eventId = `lT4Z3j:${token}`;
    const flowId = resolveFlowId({ id: eventId, occurredAt: new Date(), payload: {} as FormResponseEvent });
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<FormResponseEvent>()).toMatchObject({ formId: 'lT4Z3j', response: { token, answers: [{ text: 'Ada' }] } });
    expect(await deliver(url, body)).toBe(200);
    await expect.poll(() => app.records.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate === true).length, { timeout: 30_000 }).toBe(1);
    expect(JSON.stringify(app.records.records)).not.toContain(secret);
    await expect.poll(async () => (await project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 120_000);

it('records while Dex is unavailable and replays the event to a real Flow after restart', async () => {
  const { project, objects } = await projectFor();
  const token = randomUUID().replaceAll('-', '').slice(0, 16);
  const body = bodyFor(token);
  const offline = startApp(project, '127.0.0.1:9');
  try {
    const url = await offline.listening;
    await waitReady(url);
    expect(await deliver(url, body)).toBe(200);
    expect((await project.triggerInbox(inboxKey).pending()).map(event => event.id)).toEqual([`lT4Z3j:${token}`]);
  } finally { offline.stop.abort(); await offline.running.catch(() => undefined); }
  const restarted = await projectFor(objects);
  const online = startApp(restarted.project, dexAddress());
  try {
    const { client } = await online.ready;
    const flowId = `typeform-lT4Z3j-${token}`;
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await restarted.project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { online.stop.abort(); await online.running; }
}, 120_000);
