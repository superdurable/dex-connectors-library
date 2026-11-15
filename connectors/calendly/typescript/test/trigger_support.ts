// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { TriggerLogAttributes, TriggerLogLevel, TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';

/** A webhook signing key that must never appear in a response, record, or event. */
export const sentinelSigningKey = 'SENTINEL-SIGNING-KEY';
/** The clock of every deterministic webhook test. */
export const fixedNow = new Date('2026-09-30T12:00:00Z');
/** The event type the fixture bookings belong to. */
export const testEventTypeURI = 'https://api.calendly.com/event_types/TYPE0001';
/** Another event type, which a filtered binding ignores. */
export const otherEventTypeURI = 'https://api.calendly.com/event_types/TYPE0002';

/** Sign as Calendly documents: hex HMAC-SHA256 of "<t>.<body>" in "t=<t>,v1=<hex>". */
export function calendlySignature(signingKey: string, signedAt: Date, body: string): string {
  const timestamp = String(Math.floor(signedAt.getTime() / 1000));
  return `t=${timestamp},v1=${createHmac('sha256', signingKey).update(`${timestamp}.${body}`).digest('hex')}`;
}

/** One invitee.created or invitee.canceled delivery of scheduled event eventId, shaped as Calendly sends it. */
export function inviteeWebhookBody(event: string, inviteeId: string, eventTypeURI = testEventTypeURI, eventId = 'EVENT0001'): string {
  const eventURI = `https://api.calendly.com/scheduled_events/${eventId}`;
  return JSON.stringify({
    event, created_at: '2026-09-30T11:59:00.000000Z', created_by: 'https://api.calendly.com/users/USER0001',
    payload: {
      uri: `${eventURI}/invitees/${inviteeId}`, event: eventURI, email: 'ada@example.com', name: 'Ada Lovelace',
      status: event === 'invitee.canceled' ? 'canceled' : 'active', timezone: 'Europe/London', questions_and_answers: [{ question: 'Topic', answer: 'Engines', position: 0 }],
      rescheduled: false, old_invitee: null, new_invitee: null, created_at: '2026-09-30T11:58:00.000000Z',
      scheduled_event: {
        uri: eventURI, name: '30 Minute Meeting', status: 'active', start_time: '2026-10-02T17:00:00.000000Z', end_time: '2026-10-02T17:30:00.000000Z',
        event_type: eventTypeURI, location: { type: 'physical', location: 'Office' }, invitees_counter: { total: 1, active: 1, limit: 1 },
      },
    },
  });
}

/** A node:http server around one request listener, closed by close. */
export async function serveListener(listener: (request: IncomingMessage, response: ServerResponse) => Promise<void>): Promise<{ url: string; close(): Promise<void> }> {
  const server = createServer((request, response) => { void listener(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('test server address is unavailable');
  return { url: `http://127.0.0.1:${address.port}/webhooks/calendly`, close: () => new Promise<void>(resolve => server.close(() => resolve())) };
}

/** POST body signed with signingKey at signedAt, or unsigned for a blank key; returns the status and response text. */
export async function deliverWebhook(url: string, body: string, signingKey = sentinelSigningKey, signedAt = fixedNow, header?: string): Promise<{ status: number; text: string }> {
  const signature = header ?? (signingKey ? calendlySignature(signingKey, signedAt, body) : undefined);
  const response = await fetch(url, { method: 'POST', body, headers: { 'Content-Type': 'application/json', ...(signature ? { 'Calendly-Webhook-Signature': signature } : {}) } });
  return { status: response.status, text: await response.text() };
}

/** A loaded project with one personal access token connection and the given Trigger bindings, kept in memory; pass restartedObjects to reopen it. */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore, signingKey = sentinelSigningKey): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'calendly-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  // A restarted process reopens the project storage the first one wrote.
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'calendly', connectionName }, 0,
    new CredentialMaterial({ auth_method: 'personal-access-token', access_token: 'stand-in-token', webhook_signing_key: signingKey }, undefined, 'personal-access-token'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'calendly', connectionName, modulePath: 'calendly', provider: 'calendly', configuration: {} }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'calendly', connectionName, triggerName: 'inviteeEventReceived', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}

/** A Trigger logger that keeps every record for assertions. */
export function newRecordingLogger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: TriggerLogAttributes }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); } };
}
