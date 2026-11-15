// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider, newDexFlowTriggerTarget, newTriggerContext } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runInviteeRecorder } from '../../examples/invitee-recorder/app.js';
import { ConnectionName, InviteeCreatedTriggerBinding, InviteeRecorderFlow, acceptBooking, mapToFlowInput, resolveFlowId, type RecordedScheduledEvent } from '../../examples/invitee-recorder/flow/workflow.js';
import { CalendlyClient, inviteeEventReceivedWebhookHandler, newConnection, newInviteeEventReceivedTrigger } from '../../src/index.js';
import { calendlySignature, deliverWebhook, inviteeWebhookBody, newRecordingLogger, newTestProject, otherEventTypeURI, sentinelSigningKey, serveListener, testEventTypeURI } from '../trigger_support.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const inboxKey = { connectorId: 'calendly', connectionName: ConnectionName, triggerName: 'inviteeEventReceived', bindingName: InviteeCreatedTriggerBinding };

/** Stand-in for api.calendly.com that answers getScheduledEvent and counts the reads of each event. */
function newCalendlyAPI() {
  const reads: string[] = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const path = new URL(request.url).pathname;
    const eventId = /^\/scheduled_events\/([A-Za-z0-9_-]+)$/.exec(path)?.[1];
    if (request.method !== 'GET' || !eventId || request.headers?.Authorization !== 'Bearer stand-in-token') return { status: 404, headers: {}, body: Buffer.from('{}') };
    reads.push(eventId);
    return { status: 200, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify({ resource: {
      uri: `https://api.calendly.com/scheduled_events/${eventId}`, name: '30 Minute Meeting', status: 'active', start_time: '2026-10-02T17:00:00.000000Z', end_time: '2026-10-02T17:30:00.000000Z',
      event_type: testEventTypeURI, location: { type: 'physical', location: 'Office' }, invitees_counter: { total: 1, active: 1, limit: 1 },
    } })) };
  };
  return { send, reads };
}

function startRecorder(project: LoadedProject, serverAddress: string, send: ReturnType<typeof newCalendlyAPI>['send']) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveListening!: (url: string) => void;
  let resolveReady!: (ready: { client: Client; flow: InviteeRecorderFlow }) => void;
  const listening = new Promise<string>(resolve => { resolveListening = resolve; });
  const ready = new Promise<{ client: Client; flow: InviteeRecorderFlow }>(resolve => { resolveReady = resolve; });
  const running = runInviteeRecorder(project, { serverAddress, webhookHost: '127.0.0.1', webhookPort: 0, logger, calendlyOptions: { send }, onListening: resolveListening, onReady: resolveReady }, stop.signal);
  return { stop, logger, listening, ready, running };
}

/** Wait until /readyz reports that the binding receives deliveries. */
async function waitUntilReceiving(webhookURL: string): Promise<string> {
  const readiness = webhookURL.replace(/\/webhooks\/calendly$/, '/readyz');
  await expect.poll(async () => (await fetch(readiness)).status, { timeout: 30_000 }).toBe(200);
  return webhookURL;
}

async function waitForFlowToExist(client: Client, flowId: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    try { await client.describeFlow(flowId); return; }
    catch (error) { if (Date.now() > deadline) throw error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

it('starts one Flow per signed booking, dedupes a redelivery, and refuses forged and filtered deliveries', async () => {
  const eventId = `E${randomUUID().replaceAll('-', '').slice(0, 12).toUpperCase()}`;
  const { project } = await newTestProject(ConnectionName, [{ bindingName: InviteeCreatedTriggerBinding, configuration: { eventTypeUri: testEventTypeURI } }]);
  const calendly = newCalendlyAPI();
  const recorder = startRecorder(project, dexAddress(), calendly.send);
  try {
    const url = await waitUntilReceiving(await recorder.listening);
    const { client } = await recorder.ready;
    const body = inviteeWebhookBody('invitee.created', 'INVITEE01', testEventTypeURI, eventId);
    const forged = await deliverWebhook(url, body.replace('ada@', 'eve@'), sentinelSigningKey, new Date(), calendlySignature(sentinelSigningKey, new Date(), body));
    expect(forged.status, 'a tampered delivery is refused').toBe(400);
    expect((await deliverWebhook(url, body, sentinelSigningKey, new Date())).status).toBe(200);
    const flowId = `calendly-invitee.created-${eventId}-INVITEE01`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<RecordedScheduledEvent>()).toMatchObject({ branch: 'found', scheduledEvent: { uri: `https://api.calendly.com/scheduled_events/${eventId}`, name: '30 Minute Meeting' } });

    expect((await deliverWebhook(url, body, sentinelSigningKey, new Date())).status, 'a redelivery is acknowledged').toBe(200);
    await expect.poll(() => recorder.logger.records.filter(record => record.message === 'trigger event delivered' && record.attributes.duplicate === true).length, { timeout: 30_000 }).toBe(1);
    expect((await deliverWebhook(url, inviteeWebhookBody('invitee.canceled', 'INVITEE01', testEventTypeURI, eventId), sentinelSigningKey, new Date())).status).toBe(200);
    await expect.poll(() => recorder.logger.records.filter(record => record.message === 'trigger event skipped: filtered').length, { timeout: 30_000 }).toBe(1);
    expect((await deliverWebhook(url, inviteeWebhookBody('invitee.created', 'INVITEE02', otherEventTypeURI, eventId), sentinelSigningKey, new Date())).status, 'another event type is acknowledged').toBe(200);
    await expect.poll(async () => (await project.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
    await expect(client.describeFlow(`calendly-invitee.created-${eventId}-INVITEE02`)).rejects.toThrow();
    expect(calendly.reads, 'a redelivery reads nothing again').toEqual([eventId]);
    expect(JSON.stringify(recorder.logger.records)).not.toContain(sentinelSigningKey);
  } finally {
    recorder.stop.abort();
    await recorder.running;
  }
}, 120_000);

it('stores a booking while Dex is unreachable and starts its Flow after a restart', async () => {
  const eventId = `E${randomUUID().replaceAll('-', '').slice(0, 12).toUpperCase()}`;
  const bindings = [{ bindingName: InviteeCreatedTriggerBinding, configuration: {} }];
  const { project, objects } = await newTestProject(ConnectionName, bindings);
  const calendly = newCalendlyAPI();
  const offline = startRecorder(project, '127.0.0.1:9', calendly.send);
  const body = inviteeWebhookBody('invitee.created', 'INVITEE01', testEventTypeURI, eventId);
  try {
    expect((await deliverWebhook(await waitUntilReceiving(await offline.listening), body, sentinelSigningKey, new Date())).status, 'the 200 follows the inbox write').toBe(200);
    expect((await project.triggerInbox(inboxKey).pending()).map(event => event.id)).toEqual([`invitee.created:${eventId}:INVITEE01`]);
  } finally {
    offline.stop.abort();
    await offline.running.catch(() => undefined);
  }
  const { project: restarted } = await newTestProject(ConnectionName, bindings, objects);
  const recorder = startRecorder(restarted, dexAddress(), calendly.send);
  try {
    const { client } = await recorder.ready;
    const flowId = `calendly-invitee.created-${eventId}-INVITEE01`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    await expect.poll(async () => (await restarted.triggerInbox(inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
    expect(calendly.reads).toEqual([eventId]);
  } finally {
    recorder.stop.abort();
    await recorder.running;
  }
}, 120_000);

it('answers 503 to a delivery before the binding runs, and the retry starts the Flow', async () => {
  const eventId = `E${randomUUID().replaceAll('-', '').slice(0, 12).toUpperCase()}`;
  const calendly = newCalendlyAPI();
  const connectionRef = { provider: 'calendly', name: ConnectionName };
  const calendlyClient = new CalendlyClient({}, new StaticCredentialProvider([[connectionRef, { authMethodId: 'personal-access-token', accessToken: new SecretString('stand-in-token'), webhookSigningKey: new SecretString(sentinelSigningKey) }]]), { send: calendly.send });
  const connection = newConnection(calendlyClient, connectionRef);
  const flow = new InviteeRecorderFlow(connection);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-calendly-not-running-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress() });
  const client = new Client(registry, cache, { serverAddress: dexAddress() });
  const handler = inviteeEventReceivedWebhookHandler(connection);
  const server = await serveListener((request, response) => handler.serveHTTP(request, response));
  const stop = new AbortController();
  let running: Promise<void> | undefined;
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const body = inviteeWebhookBody('invitee.created', 'INVITEE01', testEventTypeURI, eventId);
    expect((await deliverWebhook(server.url, body, sentinelSigningKey, new Date())).status, 'no binding runs yet, so Calendly retries').toBe(503);
    const runner = newInviteeEventReceivedTrigger({ connection, connectionName: ConnectionName, bindingName: InviteeCreatedTriggerBinding, configuration: { eventTypeUri: testEventTypeURI }, target: newDexFlowTriggerTarget(client, flow, acceptBooking, resolveFlowId, mapToFlowInput) });
    running = runner.run(newTriggerContext(stop.signal));
    await expect.poll(() => handler.runningSourceCount()).toBe(1);
    expect((await deliverWebhook(server.url, body, sentinelSigningKey, new Date())).status, 'the retry is accepted').toBe(200);
    const flowId = `calendly-invitee.created-${eventId}-INVITEE01`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(calendly.reads).toEqual([eventId]);
  } finally {
    stop.abort();
    await running?.catch(() => undefined);
    await server.close();
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
