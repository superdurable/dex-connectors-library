// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { WebhookClient, RequestReceivedTriggerConfigurationSchema, defineRequestReceivedTriggerBinding, newConnection, newProjectRequestReceivedEndpointRunner, newRequestReceivedTrigger, requestReceivedWebhookHandler, type Config, type WebhookRequestEvent } from '../src/index.js';
import { fixedNow, hmac, newTestProject, post, serve, signingSecret, standardHeaders, submissionBody } from './trigger_support.js';

const reference = { provider: 'webhook', name: 'receiver-test' };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { while (cleanups.length) await cleanups.pop()!(); });

async function fixture(config: Config = {}, binding: { matchPointer?: string; matchValues?: string[] } = {}, secret = signingSecret) {
  const client = new WebhookClient(config, new StaticCredentialProvider([[reference, { signingSecret: new SecretString(secret) }]]), { now: () => fixedNow });
  const connection = newConnection(client, reference);
  const endpoint = requestReceivedWebhookHandler(connection);
  const events: TriggerEvent<WebhookRequestEvent>[] = [];
  const trigger = newRequestReceivedTrigger({ connection, connectionName: reference.name, bindingName: 'submissions', configuration: binding,
    target: { handleTrigger: async (_context, event) => { events.push(event); } } });
  const stop = new AbortController(); const running = trigger.run(newTriggerContext(stop.signal));
  const server = await serve((request, response) => endpoint.serveHTTP(request, response));
  cleanups.push(async () => { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); await server.close(); });
  await expect.poll(() => endpoint.runningSourceCount()).toBe(1);
  return { url: server.url, events };
}

it('accepts Go-tested HMAC hex, prefixed hex, and prefixed base64 shapes', async () => {
  for (const [config, header, value] of [
    [{ eventIdPointer: '/event_id' }, 'X-Signature-256', hmac(submissionBody)],
    [{ signatureHeader: 'X-Hub-Signature-256', signaturePrefix: 'sha256=', eventIdPointer: '/event_id' }, 'X-Hub-Signature-256', `sha256=${hmac(submissionBody)}`],
    [{ signatureHeader: 'Typeform-Signature', signaturePrefix: 'sha256=', signatureEncoding: 'base64', eventIdPointer: '/event_id' }, 'Typeform-Signature', `sha256=${hmac(submissionBody, signingSecret, 'base64')}`],
  ] as const) {
    const running = await fixture(config);
    expect((await post(running.url, submissionBody, { [header]: value })).status).toBe(200);
    await expect.poll(() => running.events.length).toBe(1);
    expect(running.events[0]!.id).toBe('evt_1');
    expect(running.events[0]!.payload.jsonBody).toMatchObject({ event_type: 'form_response' });
  }
});

it('verifies Standard Webhooks including rotated signatures and freshness', async () => {
  const secret = `whsec_${Buffer.from('stand-in-standard-key').toString('base64')}`;
  const running = await fixture({ verification: 'standardWebhooks', timestampToleranceSeconds: 60 }, {}, secret);
  const headers = standardHeaders(submissionBody, 'msg_1', fixedNow, secret);
  expect((await post(running.url, submissionBody, headers)).status).toBe(200);
  await expect.poll(() => running.events.length).toBe(1);
  expect(running.events[0]!.id).toBe('msg_1');
  expect((await post(running.url, submissionBody, { ...headers, 'webhook-signature': `v1,${Buffer.from('old signature').toString('base64')} v2,ZmFrZQ== ${headers['webhook-signature']}` })).status).toBe(200);
  for (const bad of [standardHeaders(submissionBody, 'old', new Date(fixedNow.getTime() - 61_000), secret),
    standardHeaders(submissionBody, 'future', new Date(fixedNow.getTime() + 61_000), secret),
    { ...headers, 'webhook-id': 'different' }, { ...headers, 'webhook-signature': 'v1,invalid' }])
    expect((await post(running.url, submissionBody, bad)).status).toBe(400);
  expect((await post(running.url, submissionBody, headers, 'text/plain')).status).toBe(400);
  const unavailable = await fixture({ verification: 'standardWebhooks' }, {}, 'whsec_invalid*');
  expect((await post(unavailable.url, submissionBody, headers)).status).toBe(503);
});

it('compares shared tokens, derives header/pointer/digest IDs, and decodes forms', async () => {
  const running = await fixture({ verification: 'sharedToken', tokenHeader: 'X-Form-Token', eventIdHeader: 'X-Delivery', eventIdPointer: '/submission/id', forwardedHeaders: ['User-Agent'] });
  const body = '{"submission":{"id":42},"event_type":"form_response"}';
  expect((await post(running.url, body, { 'X-Form-Token': signingSecret, 'X-Delivery': 'delivery-1', 'User-Agent': 'sender/1', Authorization: `Bearer ${signingSecret}` })).status).toBe(200);
  expect((await post(running.url, body, { 'X-Form-Token': signingSecret })).status).toBe(200);
  await expect.poll(() => running.events.length).toBe(2);
  expect(running.events.map(event => event.id)).toEqual(['delivery-1', '42']);
  expect(running.events[0]!.payload.headers).toEqual({ 'User-Agent': 'sender/1' });
  expect(JSON.stringify(running.events)).not.toContain(signingSecret);
  expect((await post(running.url, body, { 'X-Form-Token': 'wrong' })).status).toBe(400);
  const form = await fixture({ eventIdPointer: '/submission_id/0' });
  const formBody = 'submission_id=sub-7&email=ada%40example.com';
  expect((await post(form.url, formBody, { 'X-Signature-256': hmac(formBody) }, 'application/x-www-form-urlencoded')).status).toBe(200);
  await expect.poll(() => form.events.length).toBe(1);
  expect(form.events[0]!.id).toBe('sub-7');
  expect(form.events[0]!.payload.formBody).toEqual({ submission_id: ['sub-7'], email: ['ada@example.com'] });
});

it('filters by JSON pointer, rejects unsafe forwarding, and validates configuration', async () => {
  const running = await fixture({ eventIdPointer: '/event_id' }, { matchPointer: '/event_type', matchValues: ['form_response', '42', 'true'] });
  for (const [body, expected] of [
    ['{"event_id":"skip","event_type":"other"}', 0], ['{"event_id":"one","event_type":"form_response"}', 1],
    ['{"event_id":"two","event_type":42}', 2], ['{"event_id":"three","event_type":true}', 3],
  ] as const) { expect((await post(running.url, body, { 'X-Signature-256': hmac(body) })).status).toBe(200); await expect.poll(() => running.events.length).toBe(expected); }
  for (const forwardedHeaders of [['Authorization'], ['Cookie'], ['Webhook-Signature'], ['X-Signature-256'], ['X-Webhook-Token'], ['X Bad']])
    expect(() => new WebhookClient({ forwardedHeaders }, new StaticCredentialProvider([]))).toThrow();
  for (const configuration of [{ matchPointer: '/type' }, { matchValues: ['x'] }, { matchPointer: 'type', matchValues: ['x'] }])
    expect(RequestReceivedTriggerConfigurationSchema.safeParse(configuration).success).toBe(false);
  expect(defineRequestReceivedTriggerBinding({ connectionName: reference.name, bindingName: 'submissions', configurationUI: { units: [] } }).definition.trigger).toEqual({ connectorId: 'webhook', triggerName: 'requestReceived' });
});

it('rejects invalid request IDs and bodies, and enforces the configured body limit', async () => {
  const pointer = await fixture({ eventIdPointer: '/event_id', maxBodyBytes: 100 });
  for (const body of ['{"event_type":"form_response"}', '{"event_id":"contains space"}', '{"event_id":true}', '{'])
    expect((await post(pointer.url, body, { 'X-Signature-256': hmac(body) })).status).toBe(400);
  expect((await post(pointer.url, 'x'.repeat(101), { 'X-Signature-256': hmac('x'.repeat(101)) })).status).toBe(413);
  const form = await fixture();
  for (const body of ['field=%zz', 'field=one;field=two'])
    expect((await post(form.url, body, { 'X-Signature-256': hmac(body) }, 'application/x-www-form-urlencoded')).status).toBe(400);
});

it('records before 200 and replays the project inbox after restart', async () => {
  const { project, objects } = await newTestProject(reference.name, { eventIdPointer: '/event_id' }, {});
  const key = { connectorId: 'webhook', connectionName: reference.name, triggerName: 'requestReceived', bindingName: 'submissions' };
  async function start(loaded: typeof project, handle: (event: TriggerEvent<WebhookRequestEvent>) => Promise<void>) {
    const runner = newProjectRequestReceivedEndpointRunner(loaded, reference.name, [{ bindingName: 'submissions', target: { handleTrigger: (_context, event) => handle(event) } }], { now: () => fixedNow });
    const stop = new AbortController(); const running = runner.run(newTriggerContext(stop.signal));
    const server = await serve((request, response) => runner.serveHTTP(request, response));
    await expect.poll(() => runner.runningSourceCount()).toBe(1);
    return { url: server.url, async close() { stop.abort(); await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); await server.close(); } };
  }
  const first = await start(project, async () => { throw new Error('Dex unavailable'); });
  expect((await post(first.url, submissionBody, { 'X-Signature-256': hmac(submissionBody) })).status).toBe(200);
  expect((await project.triggerInbox(key).pending()).map(event => event.id)).toEqual(['evt_1']);
  await first.close();
  const restarted = (await newTestProject(reference.name, { eventIdPointer: '/event_id' }, {}, objects)).project;
  const events: TriggerEvent<WebhookRequestEvent>[] = [];
  const second = await start(restarted, async event => { events.push(event); });
  cleanups.push(() => second.close());
  await expect.poll(() => events.length).toBe(1);
  expect(events[0]!.payload.jsonBody).toMatchObject({ event_id: 'evt_1' });
  await expect.poll(async () => (await restarted.triggerInbox(key).pending()).length).toBe(0);
});
