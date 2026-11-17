// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { LinearClient, IssueEventReceivedTriggerConfigurationSchema, defineIssueEventReceivedTriggerBinding, issueEventReceivedWebhookHandler, newConnection, newIssueEventReceivedTrigger, newProjectIssueEventReceivedEndpointRunner, type IssueEvent } from '../src/index.js';
import { deliver, fixedNow, issueBody, issueId, newTestProject, otherTeamId, serve, signingSecret, signature, teamId } from './trigger_support.js';

const reference = { provider: 'linear', name: 'linear-test' };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

async function fixture(configuration: { actions?: string[]; teamId?: string } = {}, secret = signingSecret, maxBodyBytes = 8192) {
  const client = new LinearClient({ webhookMaxBodyBytes: maxBodyBytes }, new StaticCredentialProvider([[reference, { authMethodId: 'personal-api-key', apiKey: new SecretString('lin_api_stand_in'), webhookSigningSecret: new SecretString(secret) }]]), { now: () => fixedNow });
  const connection = newConnection(client, reference);
  const endpoint = issueEventReceivedWebhookHandler(connection);
  const events: TriggerEvent<IssueEvent>[] = [];
  const runner = newIssueEventReceivedTrigger({ connection, connectionName: reference.name, bindingName: 'issues', configuration, target: { handleTrigger: async (_context, event) => { events.push(event); } } });
  const stop = new AbortController();
  const running = runner.run(newTriggerContext(stop.signal));
  const server = await serve((request, response) => endpoint.serveHTTP(request, response));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); await server.close(); });
  await expect.poll(() => endpoint.runningSourceCount()).toBe(1);
  return { url: server.url, events };
}

it('authenticates raw body, emits Go-stable ID, and removes sensitive fields', async () => {
  const running = await fixture();
  const body = issueBody();
  expect((await deliver(running.url, body)).status).toBe(200);
  await expect.poll(() => running.events.length).toBe(1);
  const event = running.events[0]!;
  expect(event.id).toBe(`update:${issueId}:1791115198123`);
  expect(event.payload).toMatchObject({ action: 'update', issue: { id: issueId, number: 7, team: { id: teamId } }, previousStateId: '22222222-2222-4222-8222-222222222222', updatedFields: ['description', 'stateId', 'updatedAt'] });
  expect(JSON.stringify(event)).not.toMatch(/SENTINEL|alice@example.com/);
  const redelivery = issueBody('update', teamId, fixedNow.getTime() - 1_000);
  expect((await deliver(running.url, redelivery)).status).toBe(200);
  await expect.poll(() => running.events.length).toBe(2);
  expect(running.events[1]!.id).toBe(event.id);
});

it('rejects forged, stale, unsigned, malformed, and oversized bodies', async () => {
  const running = await fixture();
  const body = issueBody();
  expect((await deliver(running.url, body, 'wrong-secret')).status).toBe(400);
  expect((await deliver(running.url, body, '')).status).toBe(400);
  expect((await deliver(running.url, body.replace('Fire panel', 'Forged'), signingSecret, signature(body))).status).toBe(400);
  expect((await deliver(running.url, issueBody('update', teamId, fixedNow.getTime() - 61_000))).status).toBe(400);
  expect((await deliver(running.url, issueBody('update', teamId, fixedNow.getTime() + 61_000))).status).toBe(400);
  expect((await deliver(running.url, '{invalid')).status).toBe(400);
  expect(running.events).toEqual([]);
  const small = await fixture({}, signingSecret, 64);
  expect((await deliver(small.url, body)).status).toBe(413);
  expect((await fetch(small.url)).status).toBe(405);
  const missing = await fixture({}, '');
  expect((await deliver(missing.url, body)).status).toBe(503);
});

it('acknowledges other entities and filters action and team', async () => {
  const running = await fixture({ actions: ['create'], teamId });
  for (const body of [issueBody('create', teamId, fixedNow.getTime(), undefined, 'Comment'), issueBody('update'), issueBody('create', otherTeamId)])
    expect((await deliver(running.url, body)).status).toBe(200);
  expect((await deliver(running.url, issueBody('create'))).status).toBe(200);
  await expect.poll(() => running.events.map(event => event.payload.action)).toEqual(['create']);
});

it('answers 503 before any binding runs, and accepts ID-only team and state fields', async () => {
  const client = new LinearClient({}, new StaticCredentialProvider([[reference, { authMethodId: 'personal-api-key', apiKey: new SecretString('lin_api_stand_in'), webhookSigningSecret: new SecretString(signingSecret) }]]), { now: () => fixedNow });
  const endpoint = issueEventReceivedWebhookHandler(newConnection(client, reference));
  const server = await serve((request, response) => endpoint.serveHTTP(request, response));
  cleanups.push(() => server.close());
  expect((await deliver(server.url, issueBody())).status).toBe(503);
  const running = await fixture();
  const envelope = JSON.parse(issueBody()) as { data: Record<string, unknown> };
  delete envelope.data.team;
  delete envelope.data.state;
  delete envelope.data.assignee;
  expect((await deliver(running.url, JSON.stringify(envelope))).status).toBe(200);
  await expect.poll(() => running.events.length).toBe(1);
  expect(running.events[0]!.payload.issue).toMatchObject({ team: { id: teamId, key: '', name: '' }, state: { id: '33333333-3333-4333-8333-333333333333', name: '', type: '' } });
});

it('validates the binding and configuration', () => {
  for (const configuration of [{ actions: ['open'] }, { actions: ['create', 'create'] }, { teamId: 'not-a-uuid' }, { unexpected: true }])
    expect(IssueEventReceivedTriggerConfigurationSchema.safeParse(configuration).success).toBe(false);
  expect(defineIssueEventReceivedTriggerBinding({ connectionName: reference.name, bindingName: 'issues', configurationUI: { units: [] } }).definition.trigger).toEqual({ connectorId: 'linear', triggerName: 'issueEventReceived' });
});

it('records before 200 and replays the inbox after a process restart', async () => {
  const { project, objects } = await newTestProject(reference.name, { actions: ['update'], teamId });
  const key = { connectorId: 'linear', connectionName: reference.name, triggerName: 'issueEventReceived', bindingName: 'issues' };
  async function start(target: (event: TriggerEvent<IssueEvent>) => Promise<void>, loaded = project) {
    const runner = newProjectIssueEventReceivedEndpointRunner(loaded, reference.name, [{ bindingName: 'issues', target: { handleTrigger: (_context, event) => target(event) } }], { now: () => fixedNow });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    const server = await serve((request, response) => runner.serveHTTP(request, response));
    await expect.poll(() => runner.runningSourceCount()).toBe(1);
    return { url: server.url, async close() { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); await server.close(); } };
  }
  const first = await start(async () => { throw new Error('Dex is offline'); });
  const body = issueBody();
  expect((await deliver(first.url, body)).status).toBe(200);
  expect((await project.triggerInbox(key).pending()).map(event => event.id)).toEqual([`update:${issueId}:1791115198123`]);
  await first.close();
  const restarted = (await newTestProject(reference.name, { actions: ['update'], teamId }, objects)).project;
  const events: TriggerEvent<IssueEvent>[] = [];
  const second = await start(async event => { events.push(event); }, restarted);
  cleanups.push(() => second.close());
  await expect.poll(() => events.length).toBe(1);
  expect(events[0]!.payload.issue.id).toBe(issueId);
  await expect.poll(async () => (await restarted.triggerInbox(key).pending()).length).toBe(0);
});
