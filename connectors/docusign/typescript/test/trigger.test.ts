// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import type { Server } from 'node:http';
import { newTriggerContext, TriggerDeliveryAbortedError, type TriggerTarget } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, ObjectNotFoundError, TriggerInbox, newDurableTriggerTarget, type Configuration, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import {
  ConnectEventEnvelopeCompleted, ConnectEventEnvelopeDeclined, ConnectEventEnvelopeVoided, EnvelopeEventReceivedTriggerDefinition, EnvelopeEventSchema,
  defineEnvelopeEventReceivedTriggerBinding, envelopeEventCustomFieldValue, envelopeEventReceivedWebhookEndpoint, newEnvelopeEventReceivedEndpointRunner,
  newEnvelopeEventReceivedTrigger, type EnvelopeEvent, type EnvelopeEventReceivedEndpointRunner,
} from '../src/index.js';
import {
  CollectingTarget, connectBody, connectSignature, deliverConnect, newTestConnection, sentinelHMACKey, serveConnect, testAccountId, testConnection, testCredentials, testEnvelopeId,
} from './trigger_support.js';

const outcomesKey: TriggerInboxKey = { connectorId: 'docusign', connectionName: testConnection.name, triggerName: 'envelopeEventReceived', bindingName: 'outcomes' };
const scope = { projectId: 'docusign-endpoint-runner-tests', kind: 'live' } as const;

function outcomesConfiguration(configuration: unknown): Configuration {
  return { schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope, revision: 1, connections: [], operationConfigurations: [],
    triggerBindings: [{ ...outcomesKey, configuration }] } as unknown as Configuration;
}

const servers: Server[] = [];
const stops: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const stop of stops.splice(0)) await stop();
  for (const server of servers.splice(0)) await new Promise<void>(resolve => server.close(() => resolve()));
});

/** Run the endpoint runner with in-memory durable inboxes until the test ends. */
async function startOutcomesRunner(configuration: unknown, target: TriggerTarget<EnvelopeEvent>, credentials = testCredentials()): Promise<{ runner: EnvelopeEventReceivedEndpointRunner; url: string; objects: InMemoryObjectStore }> {
  const objects = new InMemoryObjectStore();
  const runner = newEnvelopeEventReceivedEndpointRunner(newTestConnection(credentials), outcomesConfiguration(configuration), [{ bindingName: 'outcomes', target }],
    (key, wrapped) => newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, wrapped, { decodePayload: (_json, bytes) => EnvelopeEventSchema.parse(JSON.parse(Buffer.from(bytes).toString('utf8'))) }));
  const url = await serveConnect(runner, servers);
  const abort = new AbortController();
  const running = runner.run(newTriggerContext(abort.signal));
  stops.push(async () => { abort.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await expect.poll(() => runner.runningSourceCount()).toBe(1);
  return { runner, url, objects };
}

describe('envelopeEventReceived Connect endpoint', () => {
  it('records a verified envelope outcome', async () => {
    const target = new CollectingTarget();
    const { url } = await startOutcomesRunner({}, target);
    const body = connectBody(ConnectEventEnvelopeVoided, testEnvelopeId.toUpperCase(), 'opp-123');
    // DocuSign sends one header per account key; the connection's key may be the second one.
    expect(await deliverConnect(url, body, { 'X-DocuSign-Signature-1': connectSignature('another-account-key', body), 'X-DocuSign-Signature-2': connectSignature(sentinelHMACKey, body) })).toBe(200);
    await expect.poll(() => target.events.length).toBe(1);
    const event = target.events[0]!;
    expect(event.id).toBe(`${testEnvelopeId}:envelope-voided`);
    expect(event.occurredAt).toEqual(new Date(Date.UTC(2026, 9, 4, 13, 0, 0, 123)));
    expect(event.payload).toEqual({
      event: ConnectEventEnvelopeVoided, envelopeId: testEnvelopeId, accountId: testAccountId, generatedAt: '2026-10-04T13:00:00.123Z',
      status: 'voided', voidedReason: 'Signer left the company', customFields: [{ name: 'dexSigningRequestId', value: 'opp-123' }],
    });
    expect(Object.keys(event.payload)).toEqual(['event', 'envelopeId', 'accountId', 'generatedAt', 'status', 'voidedReason', 'customFields']);
    expect(envelopeEventCustomFieldValue(event.payload, 'dexSigningRequestId')).toBe('opp-123');
    expect(envelopeEventCustomFieldValue(event.payload, 'missing')).toBeUndefined();
  });

  it('rejects forgeries and acknowledges other events', async () => {
    const target = new CollectingTarget();
    const { url } = await startOutcomesRunner({ events: [ConnectEventEnvelopeCompleted] }, target);
    const completed = connectBody(ConnectEventEnvelopeCompleted, testEnvelopeId, 'opp-123');
    const signed = (body: string, key = sentinelHMACKey) => ({ 'X-DocuSign-Signature-1': connectSignature(key, body) });

    expect(await deliverConnect(url, completed), 'an unsigned delivery is refused').toBe(400);
    expect(await deliverConnect(url, completed, signed(completed, 'not-the-key'))).toBe(400);
    expect(await deliverConnect(url, completed.replace('opp-123', 'opp-999'), signed(completed)), 'a tampered body is refused').toBe(400);
    expect(await deliverConnect(url, completed, { 'X-DocuSign-Signature': connectSignature(sentinelHMACKey, completed) }), 'only numbered signature headers count').toBe(400);
    expect(await deliverConnect(url, completed, { 'X-DocuSign-Signature-70000': connectSignature(sentinelHMACKey, completed) }), 'the number is a 16-bit unsigned integer').toBe(400);

    const recipientEvent = completed.replace('"event":"envelope-completed"', '"event":"recipient-completed"');
    expect(await deliverConnect(url, recipientEvent, signed(recipientEvent)), 'an event the Trigger does not handle is acknowledged').toBe(200);
    const declined = connectBody(ConnectEventEnvelopeDeclined, testEnvelopeId, 'opp-123');
    expect(await deliverConnect(url, declined, signed(declined)), 'the binding filters a declined event but still acknowledges it').toBe(200);
    const xmlBody = '<DocuSignEnvelopeInformation/>';
    expect(await deliverConnect(url, xmlBody, signed(xmlBody)), 'a legacy XML configuration is refused').toBe(400);
    expect(await deliverConnect(url, ' '.repeat(5000) + completed)).toBe(413);

    expect(await deliverConnect(url, completed, signed(completed))).toBe(200);
    await expect.poll(() => target.events.map(event => event.id)).toEqual([`${testEnvelopeId}:envelope-completed`]);
  });

  it('answers retryable without an HMAC key', async () => {
    const { url } = await startOutcomesRunner({}, new CollectingTarget(), testCredentials('""'));
    const body = connectBody(ConnectEventEnvelopeCompleted, testEnvelopeId, 'opp-123');
    expect(await deliverConnect(url, body, { 'X-DocuSign-Signature-1': connectSignature('', body) })).toBe(503);
  });

  it('accepts an event without envelope data', async () => {
    const target = new CollectingTarget();
    const { url } = await startOutcomesRunner({}, target);
    const body = `{"event":"envelope-completed","generatedDateTime":"","data":{"accountId":"${testAccountId}","envelopeId":"${testEnvelopeId}","envelopeSummary":""}}`;
    const before = Date.now();
    expect(await deliverConnect(url, body, { 'X-DocuSign-Signature-1': connectSignature(sentinelHMACKey, body) })).toBe(200);
    await expect.poll(() => target.events.length).toBe(1);
    const event = target.events[0]!;
    expect(event.payload.customFields).toBeUndefined();
    expect(event.payload.status).toBeUndefined();
    expect(event.occurredAt.getTime(), 'a blank generation time falls back to the receipt time').toBeGreaterThanOrEqual(before - 1);
  });

  it('keeps an undelivered event in the durable inbox after acknowledging it', async () => {
    const failing: TriggerTarget<EnvelopeEvent> = { handleTrigger: async () => { throw new Error('Dex is unreachable'); } };
    const { url, objects } = await startOutcomesRunner({}, failing);
    const body = connectBody(ConnectEventEnvelopeCompleted, testEnvelopeId, 'opp-123');
    expect(await deliverConnect(url, body, { 'X-DocuSign-Signature-1': connectSignature(sentinelHMACKey, body) })).toBe(200);
    const pending = await new TriggerInbox(objects, scope, outcomesKey).pending();
    expect(pending.map(event => event.id)).toEqual([`${testEnvelopeId}:envelope-completed`]);
  });

  it('answers 503 while no binding runs', async () => {
    const connection = newTestConnection();
    const url = await serveConnect(envelopeEventReceivedWebhookEndpoint(connection), servers);
    const body = connectBody(ConnectEventEnvelopeCompleted, testEnvelopeId, 'opp-123');
    expect(await deliverConnect(url, body, { 'X-DocuSign-Signature-1': connectSignature(sentinelHMACKey, body) })).toBe(503);
    expect(envelopeEventReceivedWebhookEndpoint(connection), 'one endpoint per connection').toBe(envelopeEventReceivedWebhookEndpoint(connection));
  });
});

describe('envelopeEventReceived wiring', () => {
  const routes = [{ bindingName: 'outcomes', target: new CollectingTarget() }];
  const passThrough = (_key: TriggerInboxKey, target: TriggerTarget<EnvelopeEvent>) => target;

  it('requires a valid binding in the project configuration', () => {
    const connection = newTestConnection();
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, { ...outcomesConfiguration({}), triggerBindings: [] }, routes, passThrough)).toThrow(ObjectNotFoundError);
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({ events: ['envelope-sent'] }), routes, passThrough)).toThrow('binding "outcomes"');
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({ events: ['envelope-voided', 'envelope-voided'] }), routes, passThrough)).toThrow('distinct');
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({ eventz: [] }), routes, passThrough)).toThrow('binding "outcomes"');
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({}), [...routes, routes[0]!], passThrough)).toThrow('duplicated');
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({}), [], passThrough)).toThrow('routes are required');
    const inboxFailure = new Error('project trigger inbox is unavailable');
    expect(() => newEnvelopeEventReceivedEndpointRunner(connection, outcomesConfiguration({}), routes, () => { throw inboxFailure; })).toThrow(inboxFailure);
  });

  it('builds a standalone Trigger and the static binding definition', () => {
    const connection = newTestConnection();
    const runner = newEnvelopeEventReceivedTrigger({ connection, connectionName: testConnection.name, bindingName: 'outcomes', configuration: { events: [ConnectEventEnvelopeVoided] }, target: new CollectingTarget() });
    expect(runner.binding()).toEqual({ connection: testConnection, trigger: { connectorId: 'docusign', triggerName: 'envelopeEventReceived' }, name: 'outcomes' });
    expect(() => newEnvelopeEventReceivedTrigger({ connection, connectionName: 'other', bindingName: 'outcomes', configuration: {}, target: new CollectingTarget() })).toThrow('must equal');
    expect(defineEnvelopeEventReceivedTriggerBinding({ connectionName: testConnection.name, bindingName: 'envelope-outcomes' })).toEqual({
      definition: EnvelopeEventReceivedTriggerDefinition, connectionName: testConnection.name, bindingName: 'envelope-outcomes',
    });
    expect(() => defineEnvelopeEventReceivedTriggerBinding({ connectionName: '', bindingName: 'x' })).toThrow();
  });

  it('declares the manifest Trigger', () => {
    const manifest = parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { triggers: Array<{ name: string; description: string }>; languages: { typescript: { triggers: string[] } } } };
    expect(manifest.spec.languages.typescript.triggers).toEqual(['envelopeEventReceived']);
    expect(manifest.spec.triggers.find(trigger => trigger.name === 'envelopeEventReceived')?.description).toBe(EnvelopeEventReceivedTriggerDefinition.description);
  });
});
