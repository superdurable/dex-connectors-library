// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerTarget } from '@superdurable/connectors-sdk';
import { afterEach, describe, expect, it } from 'vitest';
import {
  WebhookEventConversationCreated, WebhookEventConversationStatusUpdated, WebhookEventConversationTagsUpdated, WebhookEventCustomerReplyCreated, WebhookEventNoteCreated,
  ConversationEventTriggerConfigurationSchema, conversationEventWebhookHandler, conversationWebhookEvents, newConversationEventTrigger, validateConversationEventTriggerConfiguration,
  type Config, type ConversationEvent, type ConversationEventTriggerConfiguration,
} from '../src/index.js';
import { LoopbackServers, RecordingTarget, conversationJSON, deliver, eventually, fixedNow, helpScoutSignature, newTestConnection, sentinelWebhookSecret, testConnection } from './trigger_support.js';

const servers = new LoopbackServers();
const stops: Array<() => Promise<void>> = [];
afterEach(async () => { for (const stop of stops.splice(0)) await stop(); await servers.closeAll(); });

/** Serve one connection's endpoint while a conversations binding runs until the test ends. */
async function startWebhookFixture(webhookSecret: string, configuration: ConversationEventTriggerConfiguration = {}, config: Config = {}): Promise<{ url: string; target: RecordingTarget }> {
  const connection = newTestConnection(webhookSecret, config);
  const handler = conversationEventWebhookHandler(connection);
  const target = new RecordingTarget();
  const runner = newConversationEventTrigger({ connection, connectionName: testConnection.name, bindingName: 'conversations', configuration, target });
  const abort = new AbortController();
  const result = runner.run(newTriggerContext(abort.signal));
  stops.push(async () => { abort.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); });
  await eventually(() => handler.runningSourceCount() === 1, 'the binding did not start receiving');
  return { url: await servers.serve((request, response) => handler.serveHTTP(request, response)), target };
}

/** Deliveries are asynchronous after the 200; a following accepted event proves none came before it. */
async function requireNoEventBefore(fixture: { url: string; target: RecordingTarget }, expectedCount: number): Promise<void> {
  await new Promise(resolve => setTimeout(resolve, 50));
  expect(fixture.target.events).toHaveLength(expectedCount);
}

describe('conversationEvent webhook', () => {
  it("verifies Help Scout's documented example signature and acknowledges a customer event", async () => {
    const documentedBody = '{"ticket":{"id":"1","number":"2"},"customer":{"id":"1","fname":"Jackie","lname":"Chan","email":"jackie.chan@somewhere.com","emails":["jackie.chan@somewhere.com"]}}';
    expect(helpScoutSignature('your secret key', documentedBody)).toBe('I1KlvGppYqvFTJgJ9jezdQMDiyI=');
    const fixture = await startWebhookFixture('your secret key');
    expect((await deliver(fixture.url, 'customer.created', documentedBody, '', 'I1KlvGppYqvFTJgJ9jezdQMDiyI=')).status).toBe(200);
    await requireNoEventBefore(fixture, 0);
  });

  it('delivers a verified conversation with a stable ID', async () => {
    const fixture = await startWebhookFixture(sentinelWebhookSecret);
    const body = conversationJSON(501, 'active', 123, 'billing');
    const answer = await deliver(fixture.url, WebhookEventConversationCreated, body, sentinelWebhookSecret);
    expect(answer.status).toBe(200);
    expect(answer.text).not.toContain(sentinelWebhookSecret);
    const event = await fixture.target.eventAt(0);
    expect(event.id).toMatch(/^convo\.created:501:[0-9a-f]{32}$/);
    expect(event.occurredAt).toEqual(fixedNow);
    expect(event.payload.event).toBe(WebhookEventConversationCreated);
    expect(event.payload.conversation.id).toBe(501);
    expect(event.payload.conversation.mailboxId).toBe(123);
    expect(event.payload.conversation.primaryCustomer?.email).toBe('jane@acme.example.com');
    expect(event.payload.conversation.tags).toEqual(['billing']);
    expect(JSON.stringify(event)).not.toContain(sentinelWebhookSecret);

    expect((await deliver(fixture.url, WebhookEventConversationCreated, body, sentinelWebhookSecret)).status).toBe(200);
    expect((await fixture.target.eventAt(1)).id).toBe(event.id);
    expect((await deliver(fixture.url, WebhookEventConversationTagsUpdated, body, sentinelWebhookSecret)).status).toBe(200);
    expect((await fixture.target.eventAt(2)).id).not.toBe(event.id);
    const changed = conversationJSON(501, 'pending', 123, 'billing');
    expect((await deliver(fixture.url, WebhookEventConversationStatusUpdated, changed, sentinelWebhookSecret)).status).toBe(200);
    expect((await fixture.target.eventAt(3)).payload.conversation.status).toBe('pending');
  });

  it('rejects forged and malformed deliveries with 400', async () => {
    const fixture = await startWebhookFixture(sentinelWebhookSecret);
    const body = conversationJSON(501, 'active', 123);
    const notification = '{"url":"https://api.helpscout.net/v2/conversations/501"}';
    const cases: Array<{ name: string; event: string; signature: string; body: string }> = [
      { name: 'wrong secret', event: WebhookEventConversationCreated, signature: helpScoutSignature('another-secret', body), body },
      { name: 'tampered body', event: WebhookEventConversationCreated, signature: helpScoutSignature(sentinelWebhookSecret, body), body: body.replace('jane@', 'eve@') },
      { name: 'missing signature', event: WebhookEventConversationCreated, signature: '', body },
      { name: 'hex signature', event: WebhookEventConversationCreated, signature: 'ab'.repeat(20), body },
      { name: 'unpadded signature', event: WebhookEventConversationCreated, signature: helpScoutSignature(sentinelWebhookSecret, body).replace(/=+$/, ''), body },
      { name: 'missing event', event: '', signature: helpScoutSignature(sentinelWebhookSecret, body), body },
      { name: 'notification payload', event: WebhookEventConversationCreated, signature: helpScoutSignature(sentinelWebhookSecret, notification), body: notification },
    ];
    for (const test of cases) {
      const answer = await deliver(fixture.url, test.event, test.body, '', test.signature);
      expect(answer.status, test.name).toBe(400);
      expect(answer.text, test.name).not.toContain('X-HelpScout');
    }
    await requireNoEventBefore(fixture, 0);
  });

  it('answers 503 without a secret, acknowledges other events, and bounds the body and method', async () => {
    const body = conversationJSON(501, 'active', 123);
    const unsigned = await startWebhookFixture('');
    expect((await deliver(unsigned.url, WebhookEventConversationCreated, body, sentinelWebhookSecret)).status).toBe(503);

    const fixture = await startWebhookFixture(sentinelWebhookSecret);
    expect((await deliver(fixture.url, 'convo.deleted', '{"id":501}', sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(fixture.url, 'satisfaction.ratings', '{"id":1}', sentinelWebhookSecret)).status).toBe(200);
    await requireNoEventBefore(fixture, 0);

    const small = await startWebhookFixture(sentinelWebhookSecret, {}, { webhookMaxBodyBytes: 64 });
    expect((await deliver(small.url, WebhookEventConversationCreated, body, sentinelWebhookSecret)).status).toBe(413);
    expect((await fetch(small.url)).status).toBe(405);
  });

  it('filters by event and inbox while still acknowledging', async () => {
    const fixture = await startWebhookFixture(sentinelWebhookSecret, { events: [WebhookEventConversationCreated, WebhookEventCustomerReplyCreated], mailboxId: 123 });
    expect((await deliver(fixture.url, WebhookEventNoteCreated, conversationJSON(501, 'active', 123), sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(fixture.url, WebhookEventConversationCreated, conversationJSON(502, 'active', 999), sentinelWebhookSecret)).status).toBe(200);
    expect((await deliver(fixture.url, WebhookEventCustomerReplyCreated, conversationJSON(503, 'active', 123), sentinelWebhookSecret)).status).toBe(200);
    expect((await fixture.target.eventAt(0)).payload.conversation.id).toBe(503);
    await requireNoEventBefore(fixture, 1);
  });

  it('answers 503 while no binding runs, so Help Scout retries', async () => {
    const connection = newTestConnection(sentinelWebhookSecret);
    const handler = conversationEventWebhookHandler(connection);
    const url = await servers.serve((request, response) => handler.serveHTTP(request, response));
    expect((await deliver(url, WebhookEventConversationCreated, conversationJSON(501, 'active', 123), sentinelWebhookSecret)).status).toBe(503);
  });
});

describe('conversationEvent configuration', () => {
  it('validates events and the inbox like the Go connector', () => {
    for (const configuration of [{ events: ['customer.created'] }, { events: ['convo.created', 'convo.created'] }, { mailboxId: -1 }]) {
      expect(() => validateConversationEventTriggerConfiguration(configuration)).toThrow();
    }
    expect(() => validateConversationEventTriggerConfiguration({})).not.toThrow();
    expect(ConversationEventTriggerConfigurationSchema.parse({ events: null, mailboxId: null }), 'Go reads null as the zero value').toEqual({ events: null, mailboxId: null });
    expect(() => validateConversationEventTriggerConfiguration({ events: conversationWebhookEvents(), mailboxId: 123 })).not.toThrow();
    expect(conversationWebhookEvents()).toHaveLength(10);
  });

  it('rejects an invalid binding and a mismatched connection name at construction', () => {
    const connection = newTestConnection(sentinelWebhookSecret);
    const target: TriggerTarget<ConversationEvent> = { handleTrigger: async () => undefined };
    expect(() => newConversationEventTrigger({ connection, connectionName: testConnection.name, bindingName: 'conversations', configuration: { mailboxId: -1 }, target })).toThrow(/mailboxId/);
    expect(() => newConversationEventTrigger({ connection, connectionName: 'another', bindingName: 'conversations', target })).toThrow(/must equal/);
  });
});
