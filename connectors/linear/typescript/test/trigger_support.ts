// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

/** Obvious stand-in signing secret that must never appear in any event. */
export const signingSecret = 'SENTINEL-LINEAR-SIGNING-SECRET';
/** Fixed webhook receipt clock. */
export const fixedNow = new Date('2026-10-04T12:00:00Z');
/** Engineering team UUID. */
export const teamId = '2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4';
/** Other team UUID. */
export const otherTeamId = 'f6a7b8c9-d0e1-4f2a-9b3c-4d5e6f7a8b9c';
/** Issue UUID. */
export const issueId = '44444444-4444-4444-8444-444444444444';
/** Sign Linear's raw webhook body. */
export function signature(body: string, secret = signingSecret): string { return createHmac('sha256', secret).update(body).digest('hex'); }
/** Go-tested Issue webhook shape, including data the Trigger deliberately drops. */
export function issueBody(action = 'update', team = teamId, webhookTimestamp = fixedNow.getTime(), updatedAt = '2026-10-04T11:59:58.123Z', type = 'Issue'): string {
  return JSON.stringify({ action, type, createdAt: '2026-10-04T11:59:59.000Z', organizationId: 'd4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a',
    webhookId: 'e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b', webhookTimestamp,
    actor: { id: '7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', name: 'Alice Nguyen', email: 'alice@example.com', type: 'user' },
    updatedFrom: { stateId: '22222222-2222-4222-8222-222222222222', description: 'SENTINEL old description', updatedAt: '2026-10-04T11:00:00Z' },
    data: { id: issueId, identifier: 'ENG-7', number: 7, title: 'Fire panel wiring', url: 'https://linear.app/acme/issue/ENG-7', priority: 2,
      description: 'SENTINEL issue description', teamId: team, team: { id: team, key: 'ENG', name: 'Engineering' },
      stateId: '33333333-3333-4333-8333-333333333333', state: { id: '33333333-3333-4333-8333-333333333333', name: 'In Progress', type: 'started' },
      assigneeId: '7c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f', createdAt: '2026-10-01T09:00:00Z', updatedAt, trashed: null } });
}
/** HTTP server around one Trigger endpoint. */
export async function serve(listener: (request: IncomingMessage, response: ServerResponse) => Promise<void>) {
  const server = createServer((request, response) => { void listener(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('test server address is unavailable');
  return { url: `http://127.0.0.1:${address.port}/webhooks/linear`, close: () => new Promise<void>(resolve => server.close(() => resolve())) };
}
/** Deliver one signed body and read the HTTP response. */
export async function deliver(url: string, body: string, secret = signingSecret, header?: string) {
  const response = await fetch(url, { method: 'POST', body, headers: { 'Content-Type': 'application/json', ...(secret || header ? { 'Linear-Signature': header ?? signature(body, secret) } : {}) } });
  return { status: response.status, text: await response.text() };
}
/** Reloadable in-memory Dex project with one static personal API key and Trigger binding. */
export async function newTestProject(connectionName: string, configuration: Record<string, unknown>, restartedObjects?: InMemoryObjectStore, secret = signingSecret, bindingName = 'issues') {
  const scope = { projectId: 'linear-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'linear', connectionName }, 0,
    new CredentialMaterial({ auth_method: 'personal-api-key', api_key: 'lin_api_stand_in', webhook_signing_secret: secret }, undefined, 'personal-api-key'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'linear', connectionName, modulePath: 'linear', provider: 'linear', configuration: {} }],
    triggerBindings: [{ connectorId: 'linear', connectionName, triggerName: 'issueEventReceived', bindingName, configuration }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
