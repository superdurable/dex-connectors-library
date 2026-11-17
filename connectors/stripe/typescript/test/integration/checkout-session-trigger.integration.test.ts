// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomUUID } from 'node:crypto';
import { Client } from '@superdurable/dex';
import { type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runCheckoutSessionApp } from '../../examples/checkout-session-events/app.js';
import { BindingName, ConnectionName } from '../../examples/checkout-session-events/flow/workflow.js';
import type { CheckoutSessionEvent } from '../../src/index.js';

const signingSecret = 'whsec_SENTINEL_INTEGRATION_SECRET';
const signedAt = new Date(1_700_000_000_000);
const inboxKey = { connectorId: 'stripe', connectionName: ConnectionName, triggerName: 'checkoutSessionUpdated', bindingName: BindingName };

function dexAddress(): string {
  if (!process.env.DEX_FLOW_SERVICE_ADDRESS) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return process.env.DEX_FLOW_SERVICE_ADDRESS;
}

function logger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); } };
}

async function projectFor(restartedObjects?: InMemoryObjectStore) {
  const scope = { projectId: 'stripe-ts-trigger-integration', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'stripe', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ auth_method: 'api-key-with-webhook', secret_key: 'rk_test_STANDIN', webhook_secret: signingSecret }, undefined, 'api-key-with-webhook'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'stripe', connectionName: ConnectionName, modulePath: 'stripe', provider: 'stripe', authMethodId: 'api-key-with-webhook', configuration: {} }],
    triggerBindings: [{ connectorId: 'stripe', connectionName: ConnectionName, triggerName: 'checkoutSessionUpdated', bindingName: BindingName, configuration: {} }],
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
  const running = runCheckoutSessionApp(project, { serverAddress, webhookHost: '127.0.0.1', webhookPort: 0, logger: records,
    stripeOptions: { now: () => signedAt }, onListening: listeningResolve, onReady: readyResolve }, stop.signal);
  return { stop, records, listening, ready, running };
}

function bodyFor(eventId: string, eventType = 'checkout.session.async_payment_succeeded'): string {
  return JSON.stringify({ id: eventId, object: 'event', type: eventType, created: 1_700_000_000,
    data: { object: { id: 'cs_test_123', object: 'checkout.session', client_reference_id: 'registration-123', payment_status: 'paid', status: 'complete', payment_intent: 'pi_123', currency: 'usd', amount_total: 7500 } } });
}

async function deliver(url: string, body: string, signatureBody = body): Promise<number> {
  const timestamp = Math.floor(signedAt.getTime() / 1000);
  const digest = createHmac('sha256', signingSecret).update(`${timestamp}.${signatureBody}`).digest('hex');
  const response = await fetch(url, { method: 'POST', body, headers: { 'Stripe-Signature': `t=${timestamp},v1=${digest}` } });
  await response.text();
  return response.status;
}

async function waitReady(url: string): Promise<void> {
  await expect.poll(async () => (await fetch(url.replace('/webhooks/stripe', '/readyz'))).status, { timeout: 30_000 }).toBe(200);
}

async function waitForFlow(client: Client, flowId: string): Promise<void> {
  await expect.poll(async () => { try { await client.describeFlow(flowId); return true; } catch { return false; } }, { timeout: 30_000 }).toBe(true);
}

it('starts a real Dex Flow from a signed settled payment and deduplicates Stripe redelivery', async () => {
  const { project } = await projectFor();
  const app = startApp(project, dexAddress());
  try {
    const url = await app.listening;
    await waitReady(url);
    const { client } = await app.ready;
    const eventId = `evt_${randomUUID().replaceAll('-', '').slice(0, 16)}`;
    const body = bodyFor(eventId);
    expect(await deliver(url, body, body.replace('paid', 'unpaid'))).toBe(400);
    expect(await deliver(url, body)).toBe(200);
    const flowId = `stripe-${eventId}`;
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<CheckoutSessionEvent>()).toMatchObject({ type: 'checkout.session.async_payment_succeeded', session: { id: 'cs_test_123', paymentStatus: 'paid' } });
    expect(await deliver(url, body)).toBe(200);
    await expect.poll(() => app.records.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate === true).length, { timeout: 30_000 }).toBe(1);
    expect(await deliver(url, bodyFor(`evt_${randomUUID().replaceAll('-', '').slice(0, 16)}`, 'checkout.session.expired'))).toBe(200);
    expect(JSON.stringify(app.records.records)).not.toContain(signingSecret);
    await expect.poll(async () => (await project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { app.stop.abort(); await app.running; }
}, 120_000);

it('records while Dex is unavailable and starts the Flow after process restart', async () => {
  const { project, objects } = await projectFor();
  const eventId = `evt_${randomUUID().replaceAll('-', '').slice(0, 16)}`;
  const body = bodyFor(eventId);
  const offline = startApp(project, '127.0.0.1:9');
  try {
    const url = await offline.listening;
    await waitReady(url);
    expect(await deliver(url, body)).toBe(200);
    expect((await project.triggerInbox(inboxKey).pending()).map(event => event.id)).toEqual([eventId]);
  } finally { offline.stop.abort(); await offline.running.catch(() => undefined); }
  const reopened = await projectFor(objects);
  const online = startApp(reopened.project, dexAddress());
  try {
    const { client } = await online.ready;
    const flowId = `stripe-${eventId}`;
    await waitForFlow(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await reopened.project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally { online.stop.abort(); await online.running; }
}, 120_000);
