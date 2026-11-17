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
import { ClickUpClient, TaskEventTriggerConfigurationSchema, TaskEvents, newConnection, newProjectTaskEventEndpointRunner, newTaskEventTrigger, taskEventWebhookHandler, type TaskEvent } from '../src/index.js';
import { TaskEventFlow } from '../examples/task-event/flow/workflow.js';
import { taskEvents as studioTaskEvents } from '../../ui/src/events.js';

const secret = 'SENTINEL-CLICKUP-WEBHOOK-SECRET';
const connectionName = 'clickup-workspace';
const bindingName = 'task-status';
const reference = { provider: 'clickup', name: connectionName };
const bodies = {
  status: JSON.stringify({ event: 'taskStatusUpdated', webhook_id: 'webhook-01', task_id: '86b2x4k7q', history_items: [{ id: 'history-01', field: 'status', date: '1642734631523', user: { id: 183 }, parent_id: 162641062, before: { status: 'in progress' }, after: { status: 'blocked' } }] }),
  deleted: JSON.stringify({ event: 'taskDeleted', webhook_id: 'webhook-01', task_id: '86b2x4k7q' }),
};
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

async function serverFor(handler: { serveHTTP: (request: import('node:http').IncomingMessage, response: import('node:http').ServerResponse) => Promise<void> }) {
  const server = createServer((request, response) => { void handler.serveHTTP(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('no test server address');
  cleanups.push(() => new Promise<void>(resolve => server.close(() => resolve())));
  return `http://127.0.0.1:${address.port}/webhooks/clickup`;
}

async function post(url: string, body: string, signingKey = secret, signature?: string, method = 'POST') {
  const signed = signature ?? (signingKey ? createHmac('sha256', signingKey).update(body).digest('hex') : '');
  const response = await fetch(url, { method, ...(method === 'POST' ? { body } : {}), headers: { ...(signed ? { 'X-Signature': signed } : {}) } });
  return { status: response.status, text: await response.text() };
}

async function projectFor(restartedObjects?: InMemoryObjectStore, signingKey = secret, configuration: Record<string, unknown> = {}) {
  const scope = { projectId: 'clickup-ts-trigger-test', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'clickup', connectionName }, 0,
    new CredentialMaterial({ api_token: 'pk_TEST0123456789', webhook_secret: signingKey }, undefined, 'api-key'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'clickup', connectionName, modulePath: 'clickup', provider: 'clickup', configuration: {} }],
    triggerBindings: [{ connectorId: 'clickup', connectionName, triggerName: 'taskEvent', bindingName, configuration }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}

it('verifies signed status changes, uses Go event IDs, and filters event names', async () => {
  const events: Array<TriggerEvent<TaskEvent>> = [];
  const provider = new ClickUpClient({}, { resolve: async () => ({ apiToken: new SecretString('pk_TEST0123456789'), webhookSecret: new SecretString(secret) }) });
  const connection = newConnection(provider, reference);
  const handler = taskEventWebhookHandler(connection);
  const stop = new AbortController();
  const running = newTaskEventTrigger({ connection, connectionName, bindingName, configuration: { events: ['taskStatusUpdated'] }, target: { handleTrigger: async (_context, event) => { events.push(event); } } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  const url = await serverFor(handler);
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, bodies.status, secret, createHmac('sha256', secret).update(bodies.status).digest('hex').toUpperCase())).status).toBe(200);
  await expect.poll(() => events.length).toBe(1);
  expect(events[0]!.id).toBe('webhook-01:taskStatusUpdated:history-01');
  expect(events[0]!.payload).toMatchObject({ taskId: '86b2x4k7q', occurredAt: '2022-01-21T03:10:31.523Z', historyItems: [{ userId: 183, parentId: '162641062', statusBefore: 'in progress', statusAfter: 'blocked' }] });
  expect((await post(url, bodies.deleted)).status).toBe(200);
  expect(events).toHaveLength(1);
  expect(JSON.stringify(events)).not.toContain(secret);
});

it('rejects invalid signatures and bodies, and asks ClickUp to retry when unavailable', async () => {
  const provider = new ClickUpClient({ webhookMaxBodyBytes: 512 }, { resolve: async () => ({ apiToken: new SecretString('pk_TEST0123456789'), webhookSecret: new SecretString(secret) }) });
  const connection = newConnection(provider, reference);
  const handler = taskEventWebhookHandler(connection);
  const url = await serverFor(handler);
  expect((await post(url, bodies.deleted)).status).toBe(503);
  const stop = new AbortController();
  const running = newTaskEventTrigger({ connection, connectionName, bindingName, configuration: {}, target: { handleTrigger: async () => undefined } }).run(newTriggerContext(stop.signal));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  expect((await post(url, bodies.deleted, 'wrong-secret')).status).toBe(400);
  expect((await post(url, bodies.deleted, '')).status).toBe(400);
  expect((await post(url, '{"event":', secret)).status).toBe(400);
  expect((await post(url, JSON.stringify({ event: 'taskUpdated', webhook_id: 'webhook-01', task_id: '86b2x4k7q', history_items: [{ id: 'h1', field: 12 }] }))).status).toBe(400);
  expect((await post(url, bodies.deleted, secret, undefined, 'GET')).status).toBe(405);
  expect((await post(url, bodies.deleted + ' '.repeat(512))).status).toBe(413);
  expect((await post(url, JSON.stringify({ event: 'listCreated', webhook_id: 'webhook-01' }))).status).toBe(200);
  expect((await post(url, bodies.deleted)).status).toBe(200);
  expect((await post(url, bodies.deleted)).text).not.toContain(secret);
});

it('replays a stored delivery after restart before reporting readiness', async () => {
  const { project, objects } = await projectFor();
  const received: string[] = [];
  let failedAttempts = 0;
  const failing = newProjectTaskEventEndpointRunner(project, connectionName, [{ bindingName, target: { handleTrigger: async () => { failedAttempts++; throw new Error('target unavailable'); } } }]);
  const stop = new AbortController();
  const firstRun = failing.run(newTriggerContext(stop.signal));
  const url = await serverFor(failing);
  await expect.poll(() => failing.runningSourceCount()).toBe(1);
  expect((await post(url, bodies.deleted)).status).toBe(200);
  await expect.poll(() => failedAttempts).toBeGreaterThan(0);
  stop.abort();
  await expect(firstRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
  const reopened = await projectFor(objects);
  const recovered = newProjectTaskEventEndpointRunner(reopened.project, connectionName, [{ bindingName, target: { handleTrigger: async (_context, event) => { received.push(event.id); } } }]);
  const secondStop = new AbortController();
  const secondRun = recovered.run(newTriggerContext(secondStop.signal));
  cleanups.push(async () => { secondStop.abort(); await expect(secondRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => received.length).toBe(1);
  expect(received[0]).toBe('webhook-01:taskDeleted:86b2x4k7q');
});

it('validates configured events and exposes the picker binding', () => {
  expect(() => TaskEventTriggerConfigurationSchema.parse({ events: ['taskCreated', 'taskCreated'] })).toThrow();
  expect(() => TaskEventTriggerConfigurationSchema.parse({ events: ['listCreated'] })).toThrow();
  expect(TaskEvents).toEqual(studioTaskEvents.map(item => item.event));
  const binding = new TaskEventFlow().getConnectorTriggerBindings()[0]!;
  expect(binding.configurationUI?.units).toHaveLength(1);
  expect(binding.configurationUI?.units[0]).toMatchObject({ unitId: 'taskEventPicker', description: expect.stringContaining('Leave it empty'), bindings: [{ port: 'events', jsonPointer: '/events' }] });
  expect(binding.configurationUI?.units[0]?.description).toContain('https://developer.clickup.com/docs/webhooks');
  const manifest = YAML.parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { auth: { fields: Array<{ name: string; description: string; type: string }>; guide: { startURL: string; steps: string[] } }; configuration: { fields: Array<{ name: string; description: string; default: number }> } } };
  expect(manifest.spec.auth.guide.startURL).toBe('https://app.clickup.com/settings/apps');
  expect(manifest.spec.auth.guide.steps.join(' ')).toContain('webhook.secret');
  const credentialFields = manifest.spec.auth.fields;
  expect(credentialFields.map(field => field.name)).toEqual(['api_token', 'webhook_secret']);
  for (const field of credentialFields) { expect(field.type).toBe('secretString'); expect(field.description).toContain('https://'); }
  expect(credentialFields[1]!.description).toContain('Leave it blank');
  for (const field of manifest.spec.configuration.fields) { expect(field.default).toBeGreaterThan(0); expect(field.description).toContain('bytes'); expect(field.description).toContain('Blank'); }
});
