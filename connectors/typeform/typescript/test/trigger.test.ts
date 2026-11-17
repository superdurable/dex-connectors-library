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
import { FormResponseEventFlow } from '../examples/response-recorder/flow/workflow.js';
import { TypeformClient, ResponseSubmittedTriggerConfigurationSchema, newConnection, newProjectResponseSubmittedEndpointRunner, newResponseSubmittedTrigger, responseSubmittedWebhookHandler, type FormResponseEvent } from '../src/index.js';

const secret = 'SENTINEL-TYPEFORM-WEBHOOK-SECRET';
const connectionName = 'typeform-workspace';
const bindingName = 'submitted-responses';
const formId = 'lT4Z3j';
const responseToken = 'a3a12ec67a1365927098a606107fac15';
const body = JSON.stringify({ event_id: 'LtWXD3crgy', event_type: 'form_response', form_response: {
  form_id: formId, token: responseToken, landed_at: '2026-09-30T11:50:00Z', submitted_at: '2026-09-30T11:58:59Z',
  hidden: { campaign: 7 }, definition: { id: formId, title: 'Webhooks example', fields: [{ id: 'field1', ref: 'first_name', title: 'First name' }] },
  answers: [{ type: 'text', text: 'Ada', field: { id: 'field1', type: 'short_text' } }],
} });
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

async function serverFor(handler: { serveHTTP: (request: import('node:http').IncomingMessage, response: import('node:http').ServerResponse) => Promise<void> }) {
  const server = createServer((request, response) => { void handler.serveHTTP(request, response); });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('no address');
  cleanups.push(() => new Promise<void>(resolve => server.close(() => resolve())));
  return `http://127.0.0.1:${address.port}/webhooks/typeform`;
}
async function post(url: string, payload = body, signingKey = secret, signature?: string, method = 'POST') {
  const signed = signature ?? (signingKey ? `sha256=${createHmac('sha256', signingKey).update(payload).digest('base64')}` : '');
  const response = await fetch(url, { method, ...(method === 'POST' ? { body: payload } : {}), headers: { ...(signed ? { 'Typeform-Signature': signed } : {}) } });
  return { status: response.status, text: await response.text() };
}
async function projectFor(objects = new InMemoryObjectStore(), initialize = true) {
  const scope = { projectId: 'typeform-ts-trigger-test', kind: 'live' } as const;
  if (initialize) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'typeform', connectionName }, 0,
    new CredentialMaterial({ access_token: 'tfp_TEST0123456789', webhook_secret: secret }, undefined, 'personal-access-token'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'typeform', connectionName, modulePath: 'typeform', provider: 'typeform', configuration: {} }],
    triggerBindings: [{ connectorId: 'typeform', connectionName, triggerName: 'responseSubmitted', bindingName, configuration: { formId } }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
it('verifies raw body, fills definition refs, and uses the Go submission ID', async () => {
  const events: Array<TriggerEvent<FormResponseEvent>> = [];
  const provider = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('tfp_TEST0123456789'), webhookSecret: new SecretString(secret) }) });
  const connection = newConnection(provider, { provider: 'typeform', name: connectionName });
  const handler = responseSubmittedWebhookHandler(connection);
  const stop = new AbortController();
  const running = newResponseSubmittedTrigger({ connection, connectionName, bindingName, configuration: { formId }, target: { handleTrigger: async (_context, event) => { events.push(event); } } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  const url = await serverFor(handler); await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url)).status).toBe(200); await expect.poll(() => events.length).toBe(1);
  expect(events[0]!.id).toBe(`${formId}:${responseToken}`);
  expect(events[0]!.occurredAt.toISOString()).toBe('2026-09-30T11:58:59.000Z');
  expect(events[0]!.payload).toMatchObject({ formTitle: 'Webhooks example', response: { answers: [{ fieldRef: 'first_name', fieldTitle: 'First name', text: 'Ada' }], hiddenFields: { campaign: '7' } } });
  expect((await post(url, body.replace('LtWXD3crgy', 'SecondHook1'))).status).toBe(200);
  await expect.poll(() => events.length).toBe(2); expect(events[1]!.id).toBe(events[0]!.id);
  expect((await post(url, body.replaceAll(formId, 'anotherForm'))).status).toBe(200);
  expect(events).toHaveLength(2);
});
it('rejects forged and malformed deliveries, and answers 503 without a receiver', async () => {
  const provider = new TypeformClient({ webhookMaxBodyBytes: 1024 }, { resolve: async () => ({ accessToken: new SecretString('tfp_TEST0123456789'), webhookSecret: new SecretString(secret) }) });
  const connection = newConnection(provider, { provider: 'typeform', name: connectionName });
  const handler = responseSubmittedWebhookHandler(connection); const url = await serverFor(handler);
  expect((await post(url)).status).toBe(503);
  const stop = new AbortController(); const running = newResponseSubmittedTrigger({ connection, connectionName, bindingName, configuration: {}, target: { handleTrigger: async () => undefined } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, body, 'wrong-secret')).status).toBe(400);
  expect((await post(url, body, '')).status).toBe(400);
  expect((await post(url, '{', secret)).status).toBe(400);
  expect((await post(url, JSON.stringify({ event_type: 'form_response_partial' }))).status).toBe(200);
  expect((await post(url, body, secret, undefined, 'GET')).status).toBe(405);
  expect((await post(url, body + ' '.repeat(1024))).status).toBe(413);
});
it('replays a durable submission after restart', async () => {
  const { project, objects } = await projectFor(); const received: string[] = []; let attempts = 0;
  const failing = newProjectResponseSubmittedEndpointRunner(project, connectionName, [{ bindingName, target: { handleTrigger: async () => { attempts++; throw new Error('target unavailable'); } } }]);
  const stop = new AbortController(); const firstRun = failing.run(newTriggerContext(stop.signal)); const url = await serverFor(failing);
  await expect.poll(() => failing.runningSourceCount()).toBe(1); expect((await post(url)).status).toBe(200); await expect.poll(() => attempts).toBeGreaterThan(0);
  stop.abort(); await expect(firstRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
  const reopened = await projectFor(objects, false);
  const recovered = newProjectResponseSubmittedEndpointRunner(reopened.project, connectionName, [{ bindingName, target: { handleTrigger: async (_context, event) => { received.push(event.id); } } }]);
  const secondStop = new AbortController(); const secondRun = recovered.run(newTriggerContext(secondStop.signal));
  cleanups.push(async () => { secondStop.abort(); await expect(secondRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => received.length).toBe(1); expect(received[0]).toBe(`${formId}:${responseToken}`);
});
it('validates form filter and exposes provider-derived configuration guidance', () => {
  expect(() => ResponseSubmittedTriggerConfigurationSchema.parse({ formId: 'bad form' })).toThrow();
  const binding = new FormResponseEventFlow().getConnectorTriggerBindings()[0]!;
  expect(binding.configurationUI?.units[0]).toMatchObject({ unitId: 'formPicker', description: expect.stringContaining('https://admin.typeform.com'), bindings: [{ port: 'formId', jsonPointer: '/formId' }] });
  expect(binding.configurationUI?.units[0]?.description).toContain('Leave blank');
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { methods: Array<{ fields: Array<{ name: string; type: string; description: string }>; guide: { startURL: string; steps: string[] } }> }; configuration: { fields: Array<{ name: string; description: string; default: unknown }> }; studio: { units: Array<{ id: string; outputs: Array<{ name: string }> }> } } };
  const authorization = manifest.spec.auth.methods[0]!;
  expect(authorization.guide.startURL).toBe('https://admin.typeform.com/user/tokens');
  expect(authorization.guide.steps.join(' ')).toContain('webhook_secret');
  expect(authorization.fields.map(field => field.name)).toEqual(['access_token', 'webhook_secret']);
  for (const field of authorization.fields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://'); }
  expect(authorization.fields[1]!.description).toContain('Blank');
  expect(manifest.spec.configuration.fields.map(field => field.name)).toEqual(['dataCenter', 'maxResponseBytes', 'webhookMaxBodyBytes']);
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeDefined(); expect(field.description).toContain('Blank'); }
  expect(manifest.spec.studio.units[0]).toMatchObject({ id: 'formPicker', outputs: [{ name: 'formId' }] });
});
