// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, newDexFlowTriggerTarget, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import {
  ConversationEventTriggerConfigurationSchema, WebhookEventConversationCreated, WebhookEventConversationTagsUpdated, conversationEventWebhookHandler, newConversationEventTrigger,
  validateConversationEventTriggerConfiguration, type ConversationEvent,
} from '../src/index.js';
import {
  ConnectionName, FlowIDPrefix, NewConversationTriggerBinding, TagNewConversationFlow, acceptNewConversation, mapToFlowInput, resolveFlowId,
} from '../examples/tag-new-conversations/flow/workflow.js';
import { UIMailboxPickerPortMailboxID, UIUnitMailboxPicker } from '../src/index.js';
import { LoopbackServers, conversationJSON, deliver, eventually, helpScoutSignature, newTestConnection, sentinelWebhookSecret, testConnection } from './trigger_support.js';

const event = (payload: Partial<ConversationEvent['conversation']>, name = WebhookEventConversationCreated): TriggerEvent<ConversationEvent> => ({
  id: `${name}:501:${'0f'.repeat(16)}`, occurredAt: new Date(0),
  payload: { event: name, conversation: { id: 501, number: 12, subject: 'Double charge', status: 'active', mailboxId: 123, threadCount: 1, tags: [], ...payload } },
});

describe('Help Scout tag-new-conversations example', () => {
  const flow = new TagNewConversationFlow(newTestConnection(sentinelWebhookSecret, {}, {}));

  it('uses the static connection name the tests bind', () => {
    expect(ConnectionName).toBe(testConnection.name);
  });

  it('declares the new-conversations binding and its one visible inbox field', () => {
    const bindings = flow.getConnectorTriggerBindings();
    expect(bindings).toHaveLength(1);
    const binding = bindings[0]!;
    expect(mustTriggerBindingDefinition(binding)).toEqual(binding);
    expect([binding.connectionName, binding.bindingName, binding.definition.trigger.triggerName]).toEqual([ConnectionName, NewConversationTriggerBinding, 'conversationEvent']);
    const units = binding.configurationUI?.units ?? [];
    expect(units.map(unit => `${unit.id}:${unit.unitId}:${unit.required ? 'required' : 'optional'}:${unit.bindings.map(item => `${item.port}=${item.jsonPointer}`).join(',')}`))
      .toEqual([`inbox:${UIUnitMailboxPicker}:optional:${UIMailboxPickerPortMailboxID}=/mailboxId`]);
    expect(units[0]!.description).toMatch(/stores the inbox ID/);
    expect(units[0]!.description).toMatch(/Leave it empty/);
    expect(JSON.stringify(units)).not.toMatch(/token|secret/i);
    const written = ConversationEventTriggerConfigurationSchema.parse({ mailboxId: 123 });
    expect(() => validateConversationEventTriggerConfiguration(written)).not.toThrow();
  });

  it('admits only a convo.created event for an active conversation', () => {
    expect(acceptNewConversation(event({}))).toBe(true);
    expect(acceptNewConversation(event({ status: 'pending' }))).toBe(false);
    expect(acceptNewConversation(event({}, WebhookEventConversationTagsUpdated))).toBe(false);
  });

  it('maps one Flow ID per event ID and only identifiers into the Flow', () => {
    const created = event({});
    expect(resolveFlowId(created)).toBe(`${FlowIDPrefix}convo.created-501-${'0f'.repeat(16)}`);
    expect(mapToFlowInput(created)).toEqual({ eventId: created.id, event: 'convo.created', conversationId: 501, mailboxId: 123 });
  });
});

describe('Help Scout tag-new-conversations Trigger target without Dex', () => {
  const servers = new LoopbackServers();
  let cleanup: Array<() => Promise<void>> = [];
  afterEach(async () => { for (const step of cleanup) await step(); cleanup = []; await servers.closeAll(); });

  it('acknowledges a signed delivery while Dex is unreachable and rejects a tampered one', async () => {
    const connection = newTestConnection(sentinelWebhookSecret);
    const flow = new TagNewConversationFlow(connection);
    const directory = await mkdtemp(join(tmpdir(), 'dex-helpscout-unreachable-'));
    const cache = openBlobCache({ directory, maxBytes: 1024 * 1024 });
    // Port 9 (discard) refuses connections, standing in for an unreachable Dex Server.
    const client = new Client(new Registry([flow]), cache, { serverAddress: '127.0.0.1:9' });
    const starts: string[] = [];
    const dexTarget = newDexFlowTriggerTarget(client, flow, acceptNewConversation, resolveFlowId, mapToFlowInput);
    const target = { handleTrigger: async (context: Parameters<typeof dexTarget.handleTrigger>[0], delivered: TriggerEvent<ConversationEvent>) => { starts.push(delivered.id); await dexTarget.handleTrigger(context, delivered); } };
    const handler = conversationEventWebhookHandler(connection);
    const abort = new AbortController();
    const running = newConversationEventTrigger({ connection, connectionName: ConnectionName, bindingName: NewConversationTriggerBinding, configuration: { mailboxId: 123 }, target })
      .run(newTriggerContext(abort.signal)).catch(() => undefined);
    cleanup.push(async () => { abort.abort(); await running; await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
    await eventually(() => handler.runningSourceCount() === 1, 'the binding did not start receiving');
    const url = await servers.serve((request, response) => handler.serveHTTP(request, response));

    const body = conversationJSON(501, 'active', 123);
    expect((await deliver(url, WebhookEventConversationCreated, body, sentinelWebhookSecret)).status).toBe(200);
    await eventually(() => starts.length >= 1, 'the signed delivery was not routed toward Dex');
    expect((await deliver(url, WebhookEventConversationCreated, body.replace('jane@', 'eve@'), '', helpScoutSignature(sentinelWebhookSecret, body))).status).toBe(400);
    expect((await deliver(url, WebhookEventConversationCreated, conversationJSON(502, 'active', 999), sentinelWebhookSecret)).status).toBe(200);
    expect(new Set(starts).size).toBe(1);
  });
});
