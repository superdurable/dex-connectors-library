// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, describe, expect, it } from 'vitest';
import {
  CalendlyClient, InviteeEventReceivedTriggerConfigurationSchema, defineInviteeEventReceivedTriggerBinding, inviteeEventReceivedWebhookHandler, newConnection,
  newInviteeEventReceivedTrigger, newProjectInviteeEventReceivedEndpointRunner, type Config, type InviteeEvent, type InviteeEventReceivedTriggerConfiguration,
} from '../src/index.js';
import { calendlySignature, deliverWebhook, fixedNow, inviteeWebhookBody, newRecordingLogger, newTestProject, otherEventTypeURI, sentinelSigningKey, serveListener, testEventTypeURI } from './trigger_support.js';

const connectionRef = { provider: 'calendly', name: 'calendly-test' };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

/** One connection whose bookings binding runs until the test ends, behind a real HTTP server. */
async function startWebhookFixture(config: Config = {}, signingKey = sentinelSigningKey, configuration: InviteeEventReceivedTriggerConfiguration = {}) {
  const client = new CalendlyClient(config, new StaticCredentialProvider([[connectionRef, { authMethodId: 'personal-access-token', accessToken: new SecretString('stand-in-token'), webhookSigningKey: new SecretString(signingKey) }]]), { now: () => fixedNow });
  const connection = newConnection(client, connectionRef);
  const handler = inviteeEventReceivedWebhookHandler(connection);
  const events: Array<TriggerEvent<InviteeEvent>> = [];
  const runner = newInviteeEventReceivedTrigger({ connection, connectionName: connectionRef.name, bindingName: 'bookings', configuration, target: { handleTrigger: async (_context, event) => { events.push(event); } } });
  const stop = new AbortController();
  const running = runner.run(newTriggerContext(stop.signal));
  const server = await serveListener((request, response) => handler.serveHTTP(request, response));
  cleanups.push(async () => {
    stop.abort();
    await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    await server.close();
  });
  await expect.poll(() => handler.runningSourceCount()).toBe(1);
  return { url: server.url, events };
}

describe('inviteeEventReceived webhook', () => {
  it('verifies the signature and delivers the invitee with its stable ID', async () => {
    const fixture = await startWebhookFixture();
    const body = inviteeWebhookBody('invitee.created', 'INVITEE01');
    const first = await deliverWebhook(fixture.url, body);
    expect(first.status).toBe(200);
    await expect.poll(() => fixture.events.length).toBe(1);
    const event = fixture.events[0]!;
    expect(event.id).toBe('invitee.created:EVENT0001:INVITEE01');
    expect(event.occurredAt.toISOString()).toBe('2026-09-30T11:59:00.000Z');
    expect(event.payload).toMatchObject({ event: 'invitee.created', createdAt: '2026-09-30T11:59:00Z',
      invitee: { uri: 'https://api.calendly.com/scheduled_events/EVENT0001/invitees/INVITEE01', email: 'ada@example.com', status: 'active', questionsAndAnswers: [{ question: 'Topic', answer: 'Engines', position: 0 }] },
      scheduledEvent: { uri: 'https://api.calendly.com/scheduled_events/EVENT0001', eventTypeUri: testEventTypeURI } });
    expect(JSON.stringify(event)).not.toContain(sentinelSigningKey);
    expect(first.text).not.toContain(sentinelSigningKey);

    expect((await deliverWebhook(fixture.url, body, sentinelSigningKey, new Date(fixedNow.getTime() - 60_000))).status).toBe(200);
    await expect.poll(() => fixture.events.length).toBe(2);
    expect(fixture.events[1]!.id, 'a redelivery keeps its event ID').toBe(event.id);
    expect((await deliverWebhook(fixture.url, inviteeWebhookBody('invitee.canceled', 'INVITEE01'))).status).toBe(200);
    await expect.poll(() => fixture.events.length).toBe(3);
    expect(fixture.events[2]!.id, 'the cancellation has its own ID').toBe('invitee.canceled:EVENT0001:INVITEE01');
  });

  it('rejects forged, stale, and unsigned deliveries', async () => {
    const fixture = await startWebhookFixture();
    const body = inviteeWebhookBody('invitee.created', 'INVITEE01');
    const seconds = String(Math.floor(fixedNow.getTime() / 1000));
    const cases: Array<{ name: string; header: string; body: string }> = [
      { name: 'wrong key', header: calendlySignature('another-key', fixedNow, body), body },
      { name: 'tampered body', header: calendlySignature(sentinelSigningKey, fixedNow, body), body: body.replace('ada@', 'eve@') },
      { name: 'stale timestamp', header: calendlySignature(sentinelSigningKey, new Date(fixedNow.getTime() - 240_000), body), body },
      { name: 'future timestamp', header: calendlySignature(sentinelSigningKey, new Date(fixedNow.getTime() + 240_000), body), body },
      { name: 'missing header', header: '', body },
      { name: 'no v1', header: `t=${seconds}`, body },
      { name: 'no timestamp', header: `v1=${'ab'.repeat(32)}`, body },
    ];
    for (const test of cases) {
      const response = await deliverWebhook(fixture.url, test.body, test.header ? sentinelSigningKey : '', fixedNow, test.header || undefined);
      expect(response.status, test.name).toBe(400);
      expect(response.text, test.name).not.toContain('Calendly-Webhook-Signature is');
    }
    expect(fixture.events).toEqual([]);
    expect((await deliverWebhook(fixture.url, body, sentinelSigningKey, new Date(fixedNow.getTime() - 170_000))).status, 'within the three-minute default').toBe(200);
    await expect.poll(() => fixture.events.length).toBe(1);
  });

  it('answers 503 without a signing key, acknowledges other events, and enforces the body limit', async () => {
    const body = inviteeWebhookBody('invitee.created', 'INVITEE01');
    const unsigned = await startWebhookFixture({}, '');
    expect((await deliverWebhook(unsigned.url, body)).status, 'Calendly keeps retrying').toBe(503);

    const fixture = await startWebhookFixture();
    const routing = '{"event":"routing_form_submission.created","created_at":"2026-09-30T11:59:00.000000Z","payload":{"uri":"https://api.calendly.com/routing_form_submissions/X"}}';
    expect((await deliverWebhook(fixture.url, routing)).status, 'another event family is acknowledged').toBe(200);
    const routingWithFractionalPosition = routing.replace('"uri"', '"questions_and_answers":[{"position":1.5}],"uri"');
    expect((await deliverWebhook(fixture.url, routingWithFractionalPosition)).status, 'its payload is not decoded, as in Go').toBe(200);
    const fractionalAnswer = body.replace('"position":0', '"position":0.5');
    expect((await deliverWebhook(fixture.url, fractionalAnswer)).status, 'an invitee answer position must be an integer').toBe(400);
    expect((await deliverWebhook(fixture.url, '{"event":"invitee.created","payload":{}}')).status).toBe(400);
    expect((await deliverWebhook(fixture.url, 'not json')).status).toBe(400);
    expect((await deliverWebhook(fixture.url, body.replace('"event":"https://api.calendly.com/scheduled_events/EVENT0001"', '"event":"https://api.calendly.com/scheduled_events/EVENT0002"'))).status, 'the invitee and its event disagree').toBe(400);
    expect(fixture.events).toEqual([]);

    const small = await startWebhookFixture({ webhookMaxBodyBytes: 64 });
    expect((await deliverWebhook(small.url, body)).status).toBe(413);
    expect((await fetch(small.url)).status).toBe(405);
  });

  it('filters a binding by event and event type', async () => {
    const fixture = await startWebhookFixture({}, sentinelSigningKey, { events: ['invitee.created'], eventTypeUri: testEventTypeURI });
    for (const body of [inviteeWebhookBody('invitee.canceled', 'INVITEE01'), inviteeWebhookBody('invitee.created', 'INVITEE02', otherEventTypeURI)]) {
      expect((await deliverWebhook(fixture.url, body)).status, 'a filtered event is still acknowledged').toBe(200);
    }
    expect((await deliverWebhook(fixture.url, inviteeWebhookBody('invitee.created', 'INVITEE03'))).status).toBe(200);
    await expect.poll(() => fixture.events.map(event => event.id)).toEqual(['invitee.created:EVENT0001:INVITEE03']);
  });

  it('validates the binding configuration', () => {
    for (const configuration of [{ events: ['invitee_no_show.created'] }, { events: ['invitee.created', 'invitee.created'] }, { eventTypeUri: 'https://api.calendly.com/users/USER0001' }, { unknown: true }]) {
      expect(InviteeEventReceivedTriggerConfigurationSchema.safeParse(configuration).success, JSON.stringify(configuration)).toBe(false);
    }
    expect(InviteeEventReceivedTriggerConfigurationSchema.parse({})).toEqual({});
    expect(InviteeEventReceivedTriggerConfigurationSchema.safeParse({ events: ['invitee.created', 'invitee.canceled'], eventTypeUri: testEventTypeURI }).success).toBe(true);
  });

  it('declares a static binding and requires a matching connection name', () => {
    const binding = defineInviteeEventReceivedTriggerBinding({ connectionName: 'calendly-test', bindingName: 'bookings', configurationUI: { units: [] } });
    expect(binding.definition.trigger).toEqual({ connectorId: 'calendly', triggerName: 'inviteeEventReceived' });
    const client = new CalendlyClient({}, new StaticCredentialProvider([]));
    expect(() => newInviteeEventReceivedTrigger({ connection: newConnection(client, connectionRef), connectionName: 'other', bindingName: 'bookings', configuration: {}, target: { handleTrigger: async () => undefined } })).toThrow(/must equal/);
  });
});

describe('newProjectInviteeEventReceivedEndpointRunner', () => {
  const connectionName = 'calendly-test';
  const inboxKey = { connectorId: 'calendly', connectionName, triggerName: 'inviteeEventReceived', bindingName: 'bookings' };

  async function startProjectRunner(project: Awaited<ReturnType<typeof newTestProject>>['project'], handleTrigger: (event: TriggerEvent<InviteeEvent>) => Promise<void>) {
    const runner = newProjectInviteeEventReceivedEndpointRunner(project, connectionName, [{ bindingName: 'bookings', target: { handleTrigger: (_context, event) => handleTrigger(event) } }], { now: () => fixedNow, logger: newRecordingLogger() });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    const server = await serveListener((request, response) => runner.serveHTTP(request, response));
    await expect.poll(() => runner.runningSourceCount()).toBe(1);
    return { url: server.url, async stop() { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); await server.close(); } };
  }

  it('stores each event before acknowledging and replays it after a restart', async () => {
    const bindings = [{ bindingName: 'bookings', configuration: { eventTypeUri: testEventTypeURI } }];
    const { project, objects } = await newTestProject(connectionName, bindings);
    const pendingIds = async () => (await project.triggerInbox(inboxKey).pending()).map(event => event.id);
    const booked = 'invitee.created:EVENT0001:INVITEE01';

    const firstRun = await startProjectRunner(project, async () => { throw new Error('the Dex Server is unreachable'); });
    expect((await deliverWebhook(firstRun.url, inviteeWebhookBody('invitee.created', 'INVITEE01'))).status).toBe(200);
    expect(await pendingIds(), 'the 200 came after the inbox write').toEqual([booked]);
    expect((await deliverWebhook(firstRun.url, inviteeWebhookBody('invitee.created', 'INVITEE02', otherEventTypeURI))).status, 'the binding filters another event type but still acknowledges it').toBe(200);
    expect(await pendingIds()).toEqual([booked]);
    await firstRun.stop();
    expect(await pendingIds(), 'an undelivered event survives the restart').toEqual([booked]);

    const { project: restarted } = await newTestProject(connectionName, bindings, objects);
    const delivered: TriggerEvent<InviteeEvent>[] = [];
    const secondRun = await startProjectRunner(restarted, async event => { delivered.push(event); });
    cleanups.push(() => secondRun.stop());
    await expect.poll(() => delivered.map(event => event.id)).toEqual([booked]);
    expect(delivered[0]!.payload.invitee.email, 'the replayed payload decodes').toBe('ada@example.com');
    await expect.poll(pendingIds).toEqual([]);
  });

  it('requires each route binding in the project configuration', async () => {
    const target = { handleTrigger: async () => undefined };
    const { project: missing } = await newTestProject(connectionName, []);
    expect(() => newProjectInviteeEventReceivedEndpointRunner(missing, connectionName, [{ bindingName: 'bookings', target }])).toThrow(/binding "bookings"/);
    try { newProjectInviteeEventReceivedEndpointRunner(missing, connectionName, [{ bindingName: 'bookings', target }]); }
    catch (error) { expect((error as Error).cause).toBeInstanceOf(ObjectNotFoundError); }
    const { project: invalid } = await newTestProject(connectionName, [{ bindingName: 'bookings', configuration: { eventTypeUri: 'not-a-uri' } }]);
    expect(() => newProjectInviteeEventReceivedEndpointRunner(invalid, connectionName, [{ bindingName: 'bookings', target }])).toThrow(/binding "bookings"/);
    const { project: valid } = await newTestProject(connectionName, [{ bindingName: 'bookings', configuration: {} }]);
    expect(() => newProjectInviteeEventReceivedEndpointRunner(valid, connectionName, [])).toThrow(/routes are required/);
    expect(() => newProjectInviteeEventReceivedEndpointRunner(valid, connectionName, [{ bindingName: 'bookings', target }, { bindingName: 'bookings', target }])).toThrow(/duplicated/);
  });
});

it('answers 503 while no binding of the connection runs, so Calendly retries', async () => {
  const client = new CalendlyClient({}, new StaticCredentialProvider([[connectionRef, { authMethodId: 'personal-access-token', accessToken: new SecretString('stand-in-token'), webhookSigningKey: new SecretString(sentinelSigningKey) }]]), { now: () => fixedNow });
  const handler = inviteeEventReceivedWebhookHandler(newConnection(client, connectionRef));
  const server = await serveListener((request, response) => handler.serveHTTP(request, response));
  cleanups.push(() => server.close());
  expect(handler.runningSourceCount()).toBe(0);
  expect((await deliverWebhook(server.url, inviteeWebhookBody('invitee.created', 'INVITEE01'))).status).toBe(503);
});
