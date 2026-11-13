// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer, type Server } from 'node:http';
import { afterEach, describe, expect, it } from 'vitest';
import { StaticCredentialProvider, newTrigger, newTriggerContext, type TriggerEvent, type TriggerTarget } from '../src/index.js';
import { CredentialBrokerUnavailableError, InMemoryObjectStore, RelaySignatureInvalidError, TriggerInbox, newDurableTriggerTarget } from '../src/projectconfig/index.js';
import { WebhookEndpoint, WebhookEndpointRunner, WebhookVerificationUnavailableError, newRelayedRequestVerification } from '../src/webhooktrigger/index.js';
import { newRecordingLogger } from './trigger_test_support.js';

const connection = { provider: 'sample', name: 'workspace' };
const servers: Server[] = [];
afterEach(async () => { await Promise.all(servers.splice(0).map(server => new Promise(resolve => server.close(resolve)))); });

async function serve(endpoint: { serveHTTP: WebhookEndpoint<unknown, unknown>['serveHTTP'] }): Promise<string> {
  const server = createServer((request, response) => { void endpoint.serveHTTP(request, response); });
  servers.push(server);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('server address unavailable');
  return `http://127.0.0.1:${address.port}/hooks`;
}

function newEndpoint(verify: (body: string) => Promise<void> = async () => undefined) {
  return new WebhookEndpoint<{ secret: string }, { text: string }>({
    connectorId: 'sample', triggerName: 'itemCreated', connection, maxBodyBytes: 64, logger: newRecordingLogger(),
    credentials: new StaticCredentialProvider([[connection, { secret: 's' }]]),
    verifyRequest: async request => verify(Buffer.from(request.body).toString()),
    decodeEvent: request => {
      const body = JSON.parse(Buffer.from(request.body).toString()) as { id?: string; text: string };
      if (body.text === 'ignored') return undefined;
      if (!body.id) throw new Error('missing ID');
      return { id: body.id, occurredAt: request.receivedAt, payload: { text: body.text } };
    },
  });
}

describe('webhook endpoint', () => {
  it('answers 503 without a running source, then records before 200 and delivers in arrival order', async () => {
    const endpoint = newEndpoint();
    const url = await serve(endpoint);
    const post = (body: string, method = 'POST') => fetch(url, { method, body: method === 'GET' ? null : body }).then(response => response.status);
    expect(await post('{"id":"Ev0","text":"early"}')).toBe(503);
    const prepared: string[] = [];
    const delivered: string[] = [];
    let release!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    const target: TriggerTarget<{ text: string }> = {
      prepareTrigger: async (_context, event) => { prepared.push(event.id); },
      handleTrigger: async (_context, event: TriggerEvent<{ text: string }>) => { await gate; delivered.push(event.id); },
    };
    const controller = new AbortController();
    const source = endpoint.newSource(event => event.payload.text !== 'other');
    const running = source.run(newTriggerContext(controller.signal), target).catch((error: unknown) => error);
    expect(endpoint.runningSourceCount()).toBe(1);
    expect(await post('{"id":"Ev1","text":"first"}')).toBe(200);
    expect(await post('{"id":"Ev2","text":"second"}')).toBe(200);
    expect(await post('{"id":"Ev3","text":"other"}')).toBe(200);
    expect(await post('{"text":"ignored"}')).toBe(200);
    expect(prepared).toEqual(['Ev1', 'Ev2']);
    release();
    await expect.poll(() => delivered).toEqual(['Ev1', 'Ev2']);
    expect(await post('', 'GET')).toBe(405);
    expect(await post('x'.repeat(65))).toBe(413);
    expect(await post('{"text":"no id"}')).toBe(400);
    controller.abort();
    expect(await running).toMatchObject({ name: 'TriggerDeliveryAbortedError' });
    expect(endpoint.runningSourceCount()).toBe(0);
  });

  it('delivers in arrival order when an earlier request records more slowly', async () => {
    const endpoint = newEndpoint();
    const url = await serve(endpoint);
    const delivered: string[] = [];
    let isRootRecordingStarted = false;
    let releaseRoot!: () => void;
    const rootRecorded = new Promise<void>(resolve => { releaseRoot = resolve; });
    const controller = new AbortController();
    const recordings: string[] = [];
    void endpoint.newSource().run(newTriggerContext(controller.signal), {
      prepareTrigger: async (_context, event) => {
        recordings.push(`start:${event.id}`);
        if (event.id === 'EvRoot') { isRootRecordingStarted = true; await rootRecorded; }
        recordings.push(`end:${event.id}`);
      },
      handleTrigger: async (_context, event) => { delivered.push(event.id); },
    }).catch(() => undefined);
    const root = fetch(url, { method: 'POST', body: '{"id":"EvRoot","text":"root"}' });
    await expect.poll(() => isRootRecordingStarted).toBe(true);
    let isReplyAnswered = false;
    const reply = fetch(url, { method: 'POST', body: '{"id":"EvReply","text":"reply"}' }).then(response => { isReplyAnswered = true; return response.status; });
    await new Promise(resolve => setTimeout(resolve, 50));
    expect(delivered).toEqual([]);
    expect(isReplyAnswered).toBe(false);
    releaseRoot();
    expect([(await root).status, await reply]).toEqual([200, 200]);
    await expect.poll(() => delivered).toEqual(['EvRoot', 'EvReply']);
    // The inboxes record in arrival order.
    expect(recordings).toEqual(['start:EvRoot', 'end:EvRoot', 'start:EvReply', 'end:EvReply']);
    controller.abort();
  });

  it('records a durable inbox in arrival order and never lets an unverified request hold a slot', async () => {
    let releaseVerification!: () => void;
    const verificationReleased = new Promise<void>(resolve => { releaseVerification = resolve; });
    const endpoint = newEndpoint(async body => { if (body.includes('unverified')) { await verificationReleased; throw new Error('bad signature'); } });
    const url = await serve(endpoint);
    const key = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'itemCreated', bindingName: 'start' };
    const inbox = new TriggerInbox(new InMemoryObjectStore(), { projectId: 'p', kind: 'live' }, key);
    const controller = new AbortController();
    // The application is unavailable, so every recorded event stays pending while delivery retries.
    void endpoint.newSource().run(newTriggerContext(controller.signal), newDurableTriggerTarget(inbox, key, { handleTrigger: async () => { throw new Error('Dex unavailable'); } }, { logger: newRecordingLogger() })).catch(() => undefined);
    const unverified = fetch(url, { method: 'POST', body: '{"id":"EvForged","text":"unverified"}' }).then(response => response.status);
    const statuses: number[] = [];
    for (const id of ['EvA', 'EvB', 'EvC']) statuses.push((await fetch(url, { method: 'POST', body: `{"id":"${id}","text":"valid"}` })).status);
    expect(statuses).toEqual([200, 200, 200]);
    expect((await inbox.pending()).map(event => event.id)).toEqual(['EvA', 'EvB', 'EvC']);
    releaseVerification();
    expect(await unverified).toBe(400);
    controller.abort();
  });

  it('queues an event whose write finished after its slot expired, and times waiting slots from reservation', async () => {
    const endpoint = new WebhookEndpoint<{ secret: string }, { text: string }>({
      connectorId: 'sample', triggerName: 'itemCreated', connection, maxBodyBytes: 1024, arrivalTimeoutMs: 80, logger: newRecordingLogger(),
      credentials: new StaticCredentialProvider([[connection, { secret: 's' }]]),
      verifyRequest: async () => undefined,
      decodeEvent: request => { const body = JSON.parse(Buffer.from(request.body).toString()) as { id: string; text: string }; return { id: body.id, occurredAt: request.receivedAt, payload: { text: body.text } }; },
    });
    const url = await serve(endpoint);
    let releaseWrite!: () => void;
    const writeReleased = new Promise<void>(resolve => { releaseWrite = resolve; });
    const recorded: string[] = [];
    const delivered: string[] = [];
    const controller = new AbortController();
    void endpoint.newSource().run(newTriggerContext(controller.signal), {
      // These writes are already in flight when their slots expire, so they ignore the abort and then succeed.
      prepareTrigger: async (_context, event) => { if (event.id.startsWith('EvSlow')) await writeReleased; recorded.push(event.id); },
      handleTrigger: async (_context, event) => { delivered.push(event.id); },
    }).catch(() => undefined);
    const post = (id: string) => { const started = Date.now(); return fetch(url, { method: 'POST', body: `{"id":"${id}","text":"x"}` }).then(response => ({ status: response.status, elapsedMs: Date.now() - started })); };
    const first = post('EvSlowFirst');
    await new Promise(resolve => setTimeout(resolve, 10));
    const second = post('EvSlowSecond');
    const [firstResult, secondResult] = await Promise.all([first, second]);
    expect([firstResult.status, secondResult.status]).toEqual([503, 503]);
    // The second slot waited behind the first and expired 80 ms after its own reservation, not 80 ms after
    // reaching the head of the queue, which would take about 160 ms.
    expect(secondResult.elapsedMs).toBeLessThan(140);
    expect((await post('EvAfter')).status).toBe(200);
    releaseWrite();
    await expect.poll(() => delivered).toEqual(['EvAfter', 'EvSlowFirst', 'EvSlowSecond']);
    expect(recorded).toEqual(['EvAfter', 'EvSlowFirst', 'EvSlowSecond']);
    controller.abort();
  });

  it('abandons a slot that records too slowly with 503, refuses its late write, and serves later events', async () => {
    const endpoint = new WebhookEndpoint<{ secret: string }, { text: string }>({
      connectorId: 'sample', triggerName: 'itemCreated', connection, maxBodyBytes: 1024, arrivalTimeoutMs: 50,
      credentials: new StaticCredentialProvider([[connection, { secret: 's' }]]),
      verifyRequest: async () => undefined,
      decodeEvent: request => { const body = JSON.parse(Buffer.from(request.body).toString()) as { id: string; text: string }; return { id: body.id, occurredAt: request.receivedAt, payload: { text: body.text } }; },
    });
    const url = await serve(endpoint);
    let releaseStall!: () => void;
    const stallReleased = new Promise<void>(resolve => { releaseStall = resolve; });
    const recorded: string[] = [];
    const delivered: string[] = [];
    const controller = new AbortController();
    void endpoint.newSource().run(newTriggerContext(controller.signal), {
      prepareTrigger: async (context, event) => { if (event.id === 'EvStall') await stallReleased; if (!context.signal.aborted) recorded.push(event.id); },
      handleTrigger: async (_context, event) => { delivered.push(event.id); },
    }).catch(() => undefined);
    const stalled = fetch(url, { method: 'POST', body: '{"id":"EvStall","text":"x"}' }).then(response => response.status);
    await new Promise(resolve => setTimeout(resolve, 5));
    const next = fetch(url, { method: 'POST', body: '{"id":"EvNext","text":"x"}' }).then(response => response.status);
    expect(await stalled).toBe(503);
    expect(await next).toBe(200);
    releaseStall();
    await expect.poll(() => delivered).toEqual(['EvNext']);
    expect(recorded).toEqual(['EvNext']);
    controller.abort();
  });

  it('answers 400 for a refused signature and 503 for unavailable verification or recording', async () => {
    const answers = [new Error('bad signature'), new WebhookVerificationUnavailableError()];
    const endpoint = newEndpoint(async () => { const answer = answers.shift(); if (answer) throw answer; });
    const url = await serve(endpoint);
    const controller = new AbortController();
    void endpoint.newSource().run(newTriggerContext(controller.signal), { prepareTrigger: async () => { throw new Error('inbox unavailable'); }, handleTrigger: async () => undefined }).catch(() => undefined);
    const post = () => fetch(url, { method: 'POST', body: '{"id":"Ev1","text":"x"}' }).then(response => response.status);
    expect(await post()).toBe(400);
    expect(await post()).toBe(503);
    expect(await post()).toBe(503);
    controller.abort();
  });

  it('maps relayed verification failures and runs endpoint runners until one fails', async () => {
    const outcomes: unknown[] = [new RelaySignatureInvalidError(), new CredentialBrokerUnavailableError(), undefined];
    const verify = newRelayedRequestVerification({ verifyRelayedRequest: async () => { const outcome = outcomes.shift(); if (outcome) throw outcome; } });
    const request = { url: '/', headers: {}, body: Buffer.from(''), receivedAt: new Date() };
    await expect(verify(request, undefined, new AbortController().signal)).rejects.toBeInstanceOf(RelaySignatureInvalidError);
    await expect(verify(request, undefined, new AbortController().signal)).rejects.toBeInstanceOf(WebhookVerificationUnavailableError);
    await verify(request, undefined, new AbortController().signal);

    const definition = { trigger: { connectorId: 'sample', triggerName: 'itemCreated' }, description: 'Receive an item.' };
    const logger = newRecordingLogger();
    const failing = newTrigger({ definition, logger, binding: { connection, trigger: definition.trigger, name: 'a' }, target: { handleTrigger: async () => undefined }, source: { run: async () => { throw new Error('source failed'); } } });
    let isStopped = false;
    const waiting = newTrigger({ definition, logger, binding: { connection, trigger: definition.trigger, name: 'b' }, target: { handleTrigger: async () => undefined }, source: { run: context => new Promise(resolve => context.signal.addEventListener('abort', () => { isStopped = true; resolve(); })) } });
    await expect(new WebhookEndpointRunner(newEndpoint(), [failing, waiting]).run(newTriggerContext())).rejects.toThrow('source failed');
    expect(isStopped).toBe(true);
  });
});
