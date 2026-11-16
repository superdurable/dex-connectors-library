// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Deterministic checks of the new-conversation example's Flow wiring and mappers.
import { SecretString, StaticCredentialProvider, type TriggerEvent } from '@superdurable/connectors-sdk';
import { Registry } from '@superdurable/dex';
import { expect, it } from 'vitest';
import {
  ConnectionName, ConversationCreatedTriggerBinding, NewConversationFlow, acceptCustomerConversation, mapToFlowInput, mapToGetConversationInput, newIntake, resolveFlowId, validateNewConversation,
} from '../examples/new-conversation/flow/workflow.js';
import { ReamazeClient, newConnection, type ConversationCreatedEvent } from '../src/index.js';

function created(status = 0, slug = 'refund-request'): TriggerEvent<ConversationCreatedEvent> {
  const payload: ConversationCreatedEvent = { brand: 'acme', channel: 'support', conversation: { id: slug, subject: 'Refund', status, tags: [], channel: { slug: 'support' }, createdAt: '2026-03-02T09:00:00Z' } };
  return { id: 'reamaze-conversation-0123', occurredAt: new Date('2026-03-02T09:00:00Z'), payload };
}

it('names one Flow per conversation and reads it back', () => {
  const event = created();
  expect(resolveFlowId(event)).toBe('reamaze-new-conversation-reamaze-conversation-0123');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: 'reamaze-conversation-0123', created: event.payload });
  expect(mapToGetConversationInput(input)).toEqual({ conversationId: 'refund-request' });
});

it('skips spam and keeps archived conversations', () => {
  expect(acceptCustomerConversation(created(0))).toBe(true);
  expect(acceptCustomerConversation(created(4)), 'archived conversations still start a Flow').toBe(true);
  expect(acceptCustomerConversation(created(3))).toBe(false);
  expect(acceptCustomerConversation(created(9))).toBe(false);
});

it('refuses an unusable delivery and summarizes the read-back', () => {
  expect(validateNewConversation(mapToFlowInput(created()))).toBe('');
  expect(validateNewConversation({ triggerEventId: ' ', created: created().payload })).toMatch(/triggerEventId/);
  expect(validateNewConversation(mapToFlowInput(created(0, ' ')))).toMatch(/conversation\.id/);
  const conversation = { id: 'refund-request', subject: 'Refund', status: 1, tags: [], channel: { slug: 'billing' }, requester: { email: 'jane@example.com' }, createdAt: '2026-03-02T09:00:00Z' };
  const message = { body: 'a', visibility: 0, isInternalNote: false, origin: 0, createdAt: '2026-03-02T09:00:00Z' };
  expect(newIntake({ conversation, messages: [message, message] })).toEqual({ conversationId: 'refund-request', channel: 'billing', subject: 'Refund', requesterEmail: 'jane@example.com', status: 1, messageCount: 2 });
  expect(newIntake({ conversation: { ...conversation, requester: undefined } as never, messages: [] })).not.toHaveProperty('requesterEmail');
});

it('registers and declares its Trigger binding with the channel entry', () => {
  const reference = { provider: 'reamaze', name: ConnectionName };
  const client = new ReamazeClient({ brand: 'acme' }, new StaticCredentialProvider([[reference, { email: 'a@example.com', apiToken: new SecretString('x') }]]));
  const flow = new NewConversationFlow(newConnection(client, reference));
  new Registry([flow]);
  expect(flow.getFlowType()).toBe('ReamazeNewConversationTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName, binding?.definition.trigger.triggerName]).toEqual([ConnectionName, ConversationCreatedTriggerBinding, 'conversationCreated']);
  expect(binding?.configurationUI?.units.map(unit => unit.unitId)).toEqual(['channelSlugEntry']);
  expect(() => new NewConversationFlow(newConnection(client, { provider: 'reamaze', name: 'other' }))).toThrow(ConnectionName);
});
