// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import YAML from 'yaml';
import { CheckoutSessionEventTypes, CheckoutSessionUpdatedTriggerConfigurationSchema, StripeClient, checkoutSessionWebhookHandler, newConnection, newProjectCheckoutSessionWebhookRuntime, newCheckoutSessionUpdatedTrigger, type CheckoutSessionEvent } from '../src/index.js';
import { CheckoutSessionEventFlow } from '../examples/checkout-session-events/flow/workflow.js';

const signingSecret = 'whsec_SENTINEL_STRIPE_SIGNING_SECRET';
const signedAt = new Date(1_700_000_000_000);
const connectionName = 'stripe-payments';
const bindingName = 'registration-payments';
const reference = { provider: 'stripe', name: connectionName };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

function eventBody(eventType = 'checkout.session.async_payment_succeeded', eventId = 'evt_paid'): string {
  return JSON.stringify({ id: eventId, object: 'event', type: eventType, created: 1_700_000_000,
    data: { object: { id: 'cs_test_123', object: 'checkout.session', client_reference_id: 'registration-123', payment_status: 'paid', status: 'complete', payment_intent: 'pi_123', currency: 'usd', amount_total: 7500, metadata: { registration_id: 'registration-123' } } } });
}

function signature(body: string, secret = signingSecret, timestamp = signedAt): string {
  const seconds = Math.floor(timestamp.getTime() / 1000);
  return `t=${seconds},v1=${createHmac('sha256', secret).update(`${seconds}.${body}`).digest('hex')}`;
}

async function serverFor(handler: { serveHTTP: (request: import('node:http').IncomingMessage, response: import('node:http').ServerResponse) => Promise<void> }) {
  const server = createServer((request, response) => { void handler.serveHTTP(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('no test server address');
  cleanups.push(() => new Promise<void>(resolve => server.close(() => resolve())));
  return `http://127.0.0.1:${address.port}/webhooks/stripe`;
}

async function post(url: string, body: string, header = signature(body), method = 'POST') {
  const response = await fetch(url, { method, ...(method === 'POST' ? { body } : {}), headers: { ...(header ? { 'Stripe-Signature': header } : {}) } });
  return { status: response.status, text: await response.text() };
}

async function projectFor(restartedObjects?: InMemoryObjectStore, configuration: Record<string, unknown> = {}) {
  const scope = { projectId: 'stripe-ts-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'stripe', connectionName }, 0,
    new CredentialMaterial({ auth_method: 'api-key-with-webhook', secret_key: 'rk_test_STANDIN', webhook_secret: signingSecret }, undefined, 'api-key-with-webhook'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'stripe', connectionName, modulePath: 'stripe', provider: 'stripe', authMethodId: 'api-key-with-webhook', configuration }],
    triggerBindings: [{ connectorId: 'stripe', connectionName, triggerName: 'checkoutSessionUpdated', bindingName, configuration: { eventTypes: ['checkout.session.async_payment_succeeded'] } }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}

it('verifies Stripe signature and delivers a normalized session with the Stripe event ID', async () => {
  const events: Array<TriggerEvent<CheckoutSessionEvent>> = [];
  const provider = new StripeClient({}, { resolve: async () => ({ authMethodId: 'api-key-with-webhook', secretKey: new SecretString('rk_test_STANDIN'), webhookSecret: new SecretString(signingSecret) }) }, { now: () => signedAt });
  const connection = newConnection(provider, reference);
  const handler = checkoutSessionWebhookHandler(connection);
  const stop = new AbortController();
  const running = newCheckoutSessionUpdatedTrigger({ connection, connectionName, bindingName, configuration: { eventTypes: ['checkout.session.async_payment_succeeded'] }, target: { handleTrigger: async (_context, event) => { events.push(event); } } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  const url = await serverFor(handler);
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, eventBody(), `${signature(eventBody(), 'wrong')},v1=${signature(eventBody()).split('v1=')[1]}`)).status).toBe(200);
  expect((await post(url, eventBody('checkout.session.async_payment_succeeded', 'evt_spaced'), ` t=+1700000000 , v1=${signature(eventBody('checkout.session.async_payment_succeeded', 'evt_spaced')).split('v1=')[1]} `)).status).toBe(200);
  await expect.poll(() => events.length).toBe(2);
  expect(events[0]!.id).toBe('evt_paid');
  expect(events[0]!.occurredAt.toISOString()).toBe('2023-11-14T22:13:20.000Z');
  expect(events[0]!.payload).toMatchObject({ type: 'checkout.session.async_payment_succeeded', session: { id: 'cs_test_123', clientReferenceId: 'registration-123', paymentIntentId: 'pi_123', amountTotal: 7500 } });
  expect(JSON.stringify(events)).not.toContain(signingSecret);
  expect(JSON.stringify(events)).not.toContain('rk_test_STANDIN');
  expect((await post(url, eventBody('checkout.session.completed', 'evt_completed'))).status).toBe(200);
  expect(events).toHaveLength(2);
});

it('rejects forgery, stale and future signatures, bad envelopes, and oversized bodies', async () => {
  const provider = new StripeClient({ webhookMaxBodyBytes: 512, webhookSignatureToleranceMs: 60_000 }, { resolve: async () => ({ authMethodId: 'api-key-with-webhook', secretKey: new SecretString('rk_test_STANDIN'), webhookSecret: new SecretString(signingSecret) }) }, { now: () => signedAt });
  const connection = newConnection(provider, reference);
  const handler = checkoutSessionWebhookHandler(connection);
  const url = await serverFor(handler);
  expect((await post(url, eventBody())).status).toBe(503);
  const stop = new AbortController();
  const running = newCheckoutSessionUpdatedTrigger({ connection, connectionName, bindingName, configuration: {}, target: { handleTrigger: async () => undefined } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, eventBody(), signature(eventBody(), 'wrong'))).status).toBe(400);
  expect((await post(url, eventBody(), signature(eventBody(), signingSecret, new Date(signedAt.getTime() - 61_000)))).status).toBe(400);
  expect((await post(url, eventBody(), signature(eventBody(), signingSecret, new Date(signedAt.getTime() + 61_000)))).status).toBe(400);
  expect((await post(url, eventBody(), '')).status).toBe(400);
  expect((await post(url, '{"id":', signature('{"id":'))).status).toBe(400);
  expect((await post(url, eventBody().replace('cs_test_123', 'bad'), signature(eventBody().replace('cs_test_123', 'bad')))).status).toBe(400);
  expect((await post(url, eventBody() + ' '.repeat(512))).status).toBe(413);
  expect((await post(url, eventBody(), signature(eventBody()), 'GET')).status).toBe(405);
  expect((await post(url, eventBody('charge.succeeded', 'evt_ignored'))).status).toBe(200);
});

it('replays an acknowledged event from the project inbox after restart', async () => {
  const { project, objects } = await projectFor(undefined, { webhookSignatureTolerance: '5m' });
  let failedAttempts = 0;
  const failing = newProjectCheckoutSessionWebhookRuntime(project, connectionName, [{ bindingName, target: { handleTrigger: async () => { failedAttempts++; throw new Error('target unavailable'); } } }], { now: () => signedAt });
  const stop = new AbortController();
  const firstRun = failing.run(newTriggerContext(stop.signal));
  const url = await serverFor(failing);
  await expect.poll(() => failing.runningSourceCount()).toBe(1);
  expect((await post(url, eventBody())).status).toBe(200);
  await expect.poll(() => failedAttempts).toBeGreaterThan(0);
  stop.abort();
  await expect(firstRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
  const reopened = await projectFor(objects, { webhookSignatureTolerance: '5m' });
  const delivered: string[] = [];
  const recovered = newProjectCheckoutSessionWebhookRuntime(reopened.project, connectionName, [{ bindingName, target: { handleTrigger: async (_context, event) => { delivered.push(event.id); } } }], { now: () => signedAt });
  const secondStop = new AbortController();
  const secondRun = recovered.run(newTriggerContext(secondStop.signal));
  cleanups.push(async () => { secondStop.abort(); await expect(secondRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => delivered.length).toBe(1);
  expect(delivered).toEqual(['evt_paid']);
});

it('asks Stripe to retry when the connection has no webhook signing secret', async () => {
  const provider = new StripeClient({}, { resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_STANDIN') }) }, { now: () => signedAt });
  const connection = newConnection(provider, reference);
  const handler = checkoutSessionWebhookHandler(connection);
  const stop = new AbortController();
  const running = newCheckoutSessionUpdatedTrigger({ connection, connectionName, bindingName, configuration: {}, target: { handleTrigger: async () => undefined } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  const url = await serverFor(handler);
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, eventBody())).status).toBe(503);
});

it('validates event filters, auth methods, and the example binding', async () => {
  expect(() => CheckoutSessionUpdatedTriggerConfigurationSchema.parse({ eventTypes: ['charge.succeeded'] })).toThrow();
  expect(() => CheckoutSessionUpdatedTriggerConfigurationSchema.parse({ eventTypes: [CheckoutSessionEventTypes[0], CheckoutSessionEventTypes[0]] })).toThrow();
  expect(new CheckoutSessionEventFlow().getConnectorTriggerBindings()[0]).toMatchObject({ bindingName, connectionName, configurationUI: { units: [] } });
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { methods: Array<{ id: string; fields: Array<{ name: string; type: string }>; guide: { startURL: string; steps: string[] } }> }; configuration: { fields: Array<{ name: string; description: string; default: unknown }> } } };
  const withWebhook = manifest.spec.auth.methods.find(method => method.id === 'api-key-with-webhook')!;
  expect(withWebhook.guide.startURL).toBe('https://dashboard.stripe.com/apikeys');
  expect(withWebhook.guide.steps.join(' ')).toContain('Developers > Webhooks');
  expect(withWebhook.fields.map(field => field.name)).toEqual(['secret_key', 'webhook_secret']);
  expect(withWebhook.fields.every(field => field.type === 'secretString')).toBe(true);
  expect(manifest.spec.configuration.fields.map(field => field.name)).toContain('webhookSignatureTolerance');
  for (const field of manifest.spec.configuration.fields) expect(field.default).toBeDefined();
  const { project } = await projectFor();
  expect(() => newProjectCheckoutSessionWebhookRuntime(project, connectionName, [])).toThrow();
  expect(() => newProjectCheckoutSessionWebhookRuntime(project, connectionName, [{ bindingName: 'missing', target: { handleTrigger: async () => undefined } }])).toThrow();
  const negative = await projectFor(undefined, { webhookSignatureTolerance: -1 });
  expect(() => newProjectCheckoutSessionWebhookRuntime(negative.project, connectionName, [{ bindingName, target: { handleTrigger: async () => undefined } }])).toThrow();
});
