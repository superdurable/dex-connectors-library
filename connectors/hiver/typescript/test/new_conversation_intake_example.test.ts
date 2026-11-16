// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the new-conversation-intake example's Flow wiring, mappers, and binding UI.
import { Registry } from '@superdurable/dex';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import {
  ConnectionName, ConversationReceivedTriggerBinding, NewConversationIntakeFlow, acceptEveryConversation, mapToFlowInput, mapToGetConversationInput, resolveFlowId, validateReceivedConversation,
} from '../examples/new-conversation-intake/flow/workflow.js';
import type { ConversationReceivedEvent } from '../src/index.js';
import { HiverConversationsFake, newFakeConnection } from './hiver_conversations_fake.js';

const detectedAt = new Date('2036-02-23T09:30:00Z');
function delivery(conversationId = '573741003'): TriggerEvent<ConversationReceivedEvent> {
  return { id: `conversation-105902-${conversationId}`, occurredAt: detectedAt, payload: { inboxId: '105902', conversation: { id: conversationId, inboxId: '105902', status: 'open', tagIds: [] } } };
}

describe('new-conversation-intake example', () => {
  it('names one Flow per conversation and reads it back', () => {
    const event = delivery();
    expect(resolveFlowId(event)).toBe('hiver-new-conversation-conversation-105902-573741003');
    const input = mapToFlowInput(event);
    expect(input).toEqual({ triggerEventId: event.id, received: event.payload, detectedAt: '2036-02-23T09:30:00.000Z' });
    expect(mapToGetConversationInput(input)).toEqual({ inboxId: '105902', conversationId: '573741003' });
    expect(acceptEveryConversation()).toBe(true);
  });

  it('refuses an unusable delivery', () => {
    expect(validateReceivedConversation(mapToFlowInput(delivery()))).toBe('');
    expect(validateReceivedConversation({ ...mapToFlowInput(delivery()), triggerEventId: ' ' })).not.toBe('');
    expect(validateReceivedConversation(mapToFlowInput(delivery(' ')))).not.toBe('');
  });

  it('registers and declares its binding with the inbox picker', () => {
    const connection = newFakeConnection(new HiverConversationsFake('105902', 'support@acme.example.com'));
    const flow = new NewConversationIntakeFlow(connection);
    expect(new Registry([flow])).toBeDefined();
    expect(flow.getFlowType()).toBe('HiverNewConversationIntakeTypeScript');
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding!.connectionName, binding!.bindingName, binding!.definition.trigger.triggerName]).toEqual([ConnectionName, ConversationReceivedTriggerBinding, 'conversationReceived']);
    const [unit] = binding!.configurationUI!.units;
    expect([unit!.id, unit!.unitId, unit!.label, unit!.required]).toEqual(['sharedInbox', 'inboxPicker', 'Shared inbox', true]);
    expect(unit!.bindings).toEqual([{ port: 'inboxId', jsonPointer: '/inboxId' }]);
    for (const guidance of ['https://api2.hiverhq.com/v1/inboxes', '105902', 'not secret', 'cannot be blank']) expect(unit!.description).toContain(guidance);
    expect(() => new NewConversationIntakeFlow(newFakeConnection(new HiverConversationsFake('105902', 'x'), 'other'))).toThrow(ConnectionName);
  });
});
