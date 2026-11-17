// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

/** Sentinel secret that must never reach a Trigger payload or response. */ export const signingSecret = 'SENTINEL-WEBHOOK-SIGNING-SECRET';
/** Deterministic receipt clock. */ export const fixedNow = new Date('2026-10-04T12:00:00Z');
/** Go-tested submission JSON. */ export const submissionBody = '{"event_id":"evt_1","event_type":"form_response","email":"ada@example.com"}';
/** Sign raw HMAC-SHA256 bytes. */
export function hmac(body: string, secret = signingSecret, encoding: 'hex' | 'base64' = 'hex') { return createHmac('sha256', secret).update(body).digest(encoding); }
/** Sign a Standard Webhooks message with an unprefixed or whsec_ decoded key. */
export function standardHeaders(body: string, id = 'msg_1', when = fixedNow, secret = signingSecret): Record<string, string> {
  const timestamp = String(Math.floor(when.getTime() / 1000));
  const key = secret.startsWith('whsec_') ? Buffer.from(secret.slice(6), 'base64') : Buffer.from(secret);
  return { 'webhook-id': id, 'webhook-timestamp': timestamp,
    'webhook-signature': `v1,${createHmac('sha256', key).update(`${id}.${timestamp}.`).update(body).digest('base64')}` };
}
/** HTTP listener around one webhook endpoint. */
export async function serve(listener: (request: IncomingMessage, response: ServerResponse) => Promise<void>) {
  const server = createServer((request, response) => { void listener(request, response); });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('test server address is unavailable');
  return { url: `http://127.0.0.1:${address.port}/webhooks/receiver`, close: () => new Promise<void>(resolve => server.close(() => resolve())) };
}
/** POST a raw body with supplied headers. */
export async function post(url: string, body: string, headers: Record<string, string> = {}, contentType = 'application/json') {
  const response = await fetch(url, { method: 'POST', body, headers: { 'Content-Type': contentType, ...headers } });
  return { status: response.status, text: await response.text() };
}
/** Reopen one project and its durable Trigger inbox over the same object store. */
export async function newTestProject(connectionName: string, connectionConfig: Record<string, unknown>, bindingConfig: Record<string, unknown>, restartedObjects?: InMemoryObjectStore, bindingName = 'submissions') {
  const scope = { projectId: 'webhook-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'webhook', connectionName }, 0,
    new CredentialMaterial({ signing_secret: signingSecret }, undefined, 'webhook-signing-secret'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'webhook', connectionName, modulePath: 'webhook', provider: 'webhook', configuration: connectionConfig }],
    triggerBindings: [{ connectorId: 'webhook', connectionName, triggerName: 'requestReceived', bindingName, configuration: bindingConfig }],
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
