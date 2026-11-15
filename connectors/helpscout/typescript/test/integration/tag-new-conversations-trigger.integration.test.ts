// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomInt } from 'node:crypto';
import type { Client } from '@superdurable/dex';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, TriggerInbox, type Configuration } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { TriggerDeliveryAbortedError, newTriggerContext } from '@superdurable/connectors-sdk';
import { newProjectConversationEventEndpointRunner } from '@superdurable/connector-helpscout';
import { expect, it } from 'vitest';
import { runTagNewConversationsApplication } from '../../examples/tag-new-conversations/app.js';
import { ConnectionName, NewConversationTriggerBinding, TriagedTag, type TaggingRecord } from '../../examples/tag-new-conversations/flow/workflow.js';
import { conversationJSON, deliver, helpScoutSignature, newRecordingLogger, sentinelWebhookSecret } from '../trigger_support.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const scope = { projectId: 'helpscout-trigger-integration', kind: 'live' as const };
const inboxKey = { connectorId: 'helpscout', connectionName: ConnectionName, triggerName: 'conversationEvent', bindingName: NewConversationTriggerBinding };

/** A Help Scout API stand-in that records each request and serves conversations of one inbox. */
function newHelpScoutAPI(mailboxId: number) {
  const requests: string[] = [];
  const tags = new Map<number, string[]>();
  const json = (status: number, body: unknown): ProviderResponse => ({ status, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) });
  const conversation = (id: number) => ({ ...JSON.parse(conversationJSON(id, 'active', mailboxId)) as Record<string, unknown>, tags: (tags.get(id) ?? []).map(tag => ({ tag })) });
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    requests.push(`${request.method} ${url.pathname}`);
    if (request.headers?.Authorization !== 'Bearer inert-access-token') return json(401, {});
    const match = /^\/v2\/conversations\/(\d+)(\/threads|\/tags)?$/.exec(url.pathname);
    if (!match) return json(404, {});
    const id = Number(match[1]);
    if (request.method === 'GET' && !match[2]) return json(200, conversation(id));
    if (request.method === 'GET' && match[2] === '/threads') return json(200, { _embedded: { threads: [{ id: id * 10, type: 'customer', status: 'active', state: 'published', body: 'I was charged twice', createdAt: '2026-09-30T11:58:00Z' }] }, page: { totalPages: 1 } });
    if (request.method === 'PUT' && match[2] === '/tags') {
      tags.set(id, (JSON.parse(Buffer.from(request.body ?? []).toString('utf8')) as { tags: string[] }).tags);
      return { status: 204, headers: {}, body: new Uint8Array() };
    }
    return json(404, {});
  };
  return { send, requests, tags };
}

/** A project holding the connection's stored credential and the new-conversations binding. */
async function newProject(mailboxId: number): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'helpscout', connectionName: ConnectionName }, 0, new CredentialMaterial({
    app_id: 'inert-app-id', app_secret: 'inert-app-secret', access_token: 'inert-access-token', webhook_secret: sentinelWebhookSecret,
  }, new Date(Date.now() + 2 * 24 * 3_600_000), 'apiKey'));
  const configuration: Configuration = {
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'helpscout', connectionName: ConnectionName, modulePath: 'helpscout', provider: 'helpscout', configuration: {} }],
    triggerBindings: [{ ...inboxKey, configuration: { mailboxId } }],
  } as Configuration;
  return { project: new LoadedProject(configuration, undefined, objects, undefined, undefined), objects };
}

/** Start the example application and resolve once its webhook listens and the Worker is registered. */
function startApplication(project: LoadedProject, send: ReturnType<typeof newHelpScoutAPI>['send']) {
  const logger = newRecordingLogger();
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; webhookURL: string; readinessURL: string }) => void;
  const ready = new Promise<{ client: Client; webhookURL: string; readinessURL: string }>(resolve => { resolveReady = resolve; });
  const running = runTagNewConversationsApplication(project, {
    serverAddress: dexAddress(), webhookHost: '127.0.0.1', webhookPort: 0, logger, helpScoutOptions: { send }, onReady: resolveReady,
  }, stop.signal);
  return { logger, stop, ready, running };
}

async function waitForFlowToExist(client: Client, flowId: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    try { await client.describeFlow(flowId); return; }
    catch (error) { if (Date.now() > deadline) throw error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

it('starts one tagging Flow per signed new conversation and refuses forged, filtered, and redelivered webhooks', async () => {
  const mailboxId = randomInt(100_000, 2_000_000_000);
  const conversationId = randomInt(100_000, 2_000_000_000);
  const { project, objects } = await newProject(mailboxId);
  const helpScout = newHelpScoutAPI(mailboxId);
  const application = startApplication(project, helpScout.send);
  const logger = application.logger;
  try {
    const { client, webhookURL, readinessURL } = await application.ready;
    await expect.poll(async () => (await fetch(readinessURL)).status, { timeout: 30_000 }).toBe(200);
    const body = conversationJSON(conversationId, 'active', mailboxId);

    expect((await deliver(webhookURL, 'convo.created', body.replace('jane@', 'eve@'), '', helpScoutSignature(sentinelWebhookSecret, body))).status).toBe(400);
    expect((await deliver(webhookURL, 'convo.created', body, 'another-secret')).status).toBe(400);
    expect((await deliver(webhookURL, 'convo.created', body, sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(webhookURL, 'convo.created', body, sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(webhookURL, 'convo.tags', body, sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(webhookURL, 'convo.created', conversationJSON(conversationId + 1, 'active', mailboxId + 1), sentinelWebhookSecret)).status).toBe(200);

    const createdId = `convo.created:${conversationId}:${createHash('sha256').update(body).digest().subarray(0, 16).toString('hex')}`;
    const flowId = `helpscout-${createdId.replaceAll(':', '-')}`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    const record = completed.singleOutput<TaggingRecord>();
    expect(record).toEqual({ request: { eventId: createdId, event: 'convo.created', conversationId, mailboxId }, stage: 'completed', status: 'active', newestThreadTypes: ['customer'], tags: [TriagedTag] });

    await expect.poll(async () => (await new TriggerInbox(objects, scope, inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
    await expect.poll(() => logger.records.filter(entry => entry.message === 'trigger event skipped: filtered').length, { timeout: 30_000 }).toBe(1);
    expect(logger.records.filter(entry => entry.message === 'trigger event delivered' && entry.attributes.duplicate === true)).toHaveLength(1);
    expect(helpScout.requests.filter(request => request.startsWith('PUT'))).toEqual([`PUT /v2/conversations/${conversationId}/tags`]);
    expect(helpScout.requests.filter(request => request.includes(String(conversationId + 1)))).toEqual([]);
    expect(JSON.stringify(logger.records)).not.toContain(sentinelWebhookSecret);
  } finally {
    application.stop.abort();
    await application.running;
  }
}, 120_000);

it('records a delivery while the Flow target is unavailable and starts its Flow after a restart', async () => {
  const mailboxId = randomInt(100_000, 2_000_000_000);
  const conversationId = randomInt(100_000, 2_000_000_000);
  const { project, objects } = await newProject(mailboxId);
  const helpScout = newHelpScoutAPI(mailboxId);
  const body = conversationJSON(conversationId, 'active', mailboxId);

  // The first process cannot reach Dex: its target keeps every event pending.
  const offline = newProjectConversationEventEndpointRunner(project, ConnectionName, [{ bindingName: NewConversationTriggerBinding,
    target: { handleTrigger: async () => { throw new Error('the Dex Server is unreachable'); } } }], { send: helpScout.send, logger: newRecordingLogger() });
  const server = createServer((request, response) => { void offline.serveHTTP(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('server address unavailable');
  const offlineURL = `http://127.0.0.1:${address.port}/webhooks/helpscout`;
  expect((await deliver(offlineURL, 'convo.created', body, sentinelWebhookSecret)).status, 'no binding runs yet, so Help Scout retries').toBe(503);
  const abort = new AbortController();
  const offlineRun = offline.run(newTriggerContext(abort.signal));
  await expect.poll(() => offline.runningSourceCount()).toBe(1);
  expect((await deliver(offlineURL, 'convo.created', body, sentinelWebhookSecret)).status).toBe(200);
  expect((await new TriggerInbox(objects, scope, inboxKey).pending()).map(event => event.id)).toHaveLength(1);
  abort.abort();
  await expect(offlineRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
  server.closeAllConnections();
  await new Promise<void>(resolve => server.close(() => resolve()));

  const application = startApplication(project, helpScout.send);
  try {
    const { client } = await application.ready;
    const eventId = `convo.created:${conversationId}:${createHash('sha256').update(body).digest().subarray(0, 16).toString('hex')}`;
    const flowId = `helpscout-${eventId.replaceAll(':', '-')}`;
    await waitForFlowToExist(client, flowId);
    const completed = await client.waitForFlow(flowId, 60_000);
    expect(completed.status, completed.errorMessage).toBe('completed');
    expect(completed.singleOutput<TaggingRecord>().tags).toEqual([TriagedTag]);
    await expect.poll(async () => (await new TriggerInbox(objects, scope, inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
    expect(application.logger.records.some(entry => entry.message === 'replaying pending trigger events')).toBe(true);
  } finally {
    application.stop.abort();
    await application.running;
  }
}, 120_000);
