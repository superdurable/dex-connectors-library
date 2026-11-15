// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerEvent } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { GmailClient, GmailConnection, MessageReceivedTriggerConfigurationSchema, ReplyReceivedTriggerConfigurationSchema, type MessageEvent } from '../src/index.js';
import {
  ConnectionName, GmailThreadReplyFlow, ReplyMessageConfigurationSchema, ReplyMessageConfigurationUI, ReplyTriggerBinding, StartTriggerBinding, loadReplyMessageConfiguration, mapToFlowInput, mapToReceiveEmailReplyInput,
  newReplyTriggerFilter, newStartTriggerFilter, resolveFlowId,
} from '../examples/thread-reply/flow/workflow.js';

const reference = { provider: 'google', name: ConnectionName };
const flow = new GmailThreadReplyFlow(new GmailConnection(new GmailClient({}, new StaticCredentialProvider([[reference, { accessToken: new SecretString('t'), primaryEmail: 'owner@example.com' }]])), reference));

/** Visible fields as id, unit, required flag, and the JSON pointer each writes. */
function visibleFields(ui: ConnectorConfigurationUI | undefined): string[] {
  return (ui?.units ?? []).map(unit => `${unit.id}:${unit.unitId}:${unit.required ? 'required' : 'optional'}:${unit.bindings.map(binding => `${binding.port}=${binding.jsonPointer}`).join(',')}`);
}

/** Build the configuration a binding's units write, setting each pointer to a sample of its port. */
function configurationFromUnits(ui: ConnectorConfigurationUI | undefined): Record<string, unknown> {
  const samples: Record<string, unknown> = { query: 'label:inbox is:unread', emails: ['ada@example.com'], text: 'order' };
  const configuration: Record<string, unknown> = {};
  for (const unit of ui?.units ?? []) {
    for (const binding of unit.bindings) {
      const segments = binding.jsonPointer.split('/').slice(1);
      let target = configuration;
      for (const segment of segments.slice(0, -1)) target = (target[segment] ??= {}) as Record<string, unknown>;
      target[segments.at(-1)!] = samples[binding.port];
    }
  }
  return configuration;
}

const event = (id: string, payload: Partial<MessageEvent>): TriggerEvent<MessageEvent> => ({ id, occurredAt: new Date(0), payload: {
  primaryEmail: 'owner@example.com', messageId: id, threadId: 'thread-1', from: 'Ada <ada@example.com>', subject: 'Order 1042', snippet: 'Has it shipped?', receivedAt: '2026-01-02T00:00:00Z', isReply: false, ...payload,
} });

describe('Gmail thread reply example', () => {
  it('declares both static bindings and enumerates every visible configuration field', () => {
    const [start, reply] = flow.getConnectorTriggerBindings();
    expect(flow.getConnectorTriggerBindings()).toHaveLength(2);
    for (const binding of [start!, reply!]) expect(mustTriggerBindingDefinition(binding)).toEqual(binding);
    expect([start!.bindingName, start!.connectionName, start!.definition.trigger.triggerName]).toEqual([StartTriggerBinding, ConnectionName, 'messageReceived']);
    expect([reply!.bindingName, reply!.connectionName, reply!.definition.trigger.triggerName]).toEqual([ReplyTriggerBinding, ConnectionName, 'replyReceived']);
    expect(visibleFields(start!.configurationUI)).toEqual([
      'query:searchQueryInput:optional:query=/searchQuery',
      'message:textInput:optional:text=/messageMatcher/messageContains',
      'senders:emailListInput:optional:emails=/messageMatcher/senderEmails',
    ]);
    expect(visibleFields(reply!.configurationUI)).toEqual([
      'query:searchQueryInput:optional:query=/searchQuery',
      'message:textInput:optional:text=/replyMatcher/messageContains',
      'senders:emailListInput:optional:emails=/replyMatcher/senderEmails',
    ]);
    expect(visibleFields(ReplyMessageConfigurationUI)).toEqual(['replyBody:textInput:required:text=/textBody']);
  });

  it('explains blank behavior and writes configurations the connector accepts', () => {
    const units = [...flow.getConnectorTriggerBindings().flatMap(binding => binding.configurationUI?.units ?? []), ...ReplyMessageConfigurationUI.units];
    for (const unit of units) {
      expect(unit.description.length, unit.id).toBeGreaterThan(40);
      if (!unit.required) expect(unit.description, unit.id).toMatch(/leave blank/);
    }
    expect(JSON.stringify(units)).not.toMatch(/token|secret/i);
    const [start, reply] = flow.getConnectorTriggerBindings();
    expect(MessageReceivedTriggerConfigurationSchema.parse(configurationFromUnits(start!.configurationUI))).toEqual({ searchQuery: 'label:inbox is:unread', messageMatcher: { messageContains: 'order', senderEmails: ['ada@example.com'] } });
    expect(ReplyReceivedTriggerConfigurationSchema.parse(configurationFromUnits(reply!.configurationUI))).toEqual({ searchQuery: 'label:inbox is:unread', replyMatcher: { messageContains: 'order', senderEmails: ['ada@example.com'] } });
  });

  it('loads the completion reply that the reply unit writes and refuses a missing or invalid one', () => {
    const saved = (configuration: Record<string, unknown>, stepType = 'ReplyToReceivedEmail') => ({ operationConfigurations: [{ connectorId: 'gmail', connectionName: ConnectionName, operationId: 'replyToMessage', flowType: flow.getFlowType(), stepType, configuration }] }) as never;
    const written = configurationFromUnits(ReplyMessageConfigurationUI);
    expect(ReplyMessageConfigurationSchema.parse(written)).toEqual({ textBody: 'order' });
    expect(loadReplyMessageConfiguration(saved({ textBody: 'Resolved.' }))).toEqual({ textBody: 'Resolved.' });
    expect(() => loadReplyMessageConfiguration(saved({ textBody: 'Resolved.' }, 'OtherStep'))).toThrow('load the saved reply message: no configuration for GmailThreadReplyFlow Step ReplyToReceivedEmail');
    expect(() => loadReplyMessageConfiguration(saved({ textBody: 'Resolved.', htmlBody: '<p>x</p>' }))).toThrow('load the saved reply message');
  });

  it('resolves one Flow ID for a root and its reply and maps only safe identifiers', () => {
    const root = event('m-root', {});
    const reply = event('m-reply', { isReply: true });
    expect(resolveFlowId(root)).toBe('gmail-thread-reply-gmail-inbox-thread-1');
    expect(resolveFlowId(reply)).toBe(resolveFlowId(root));
    expect(mapToFlowInput(root)).toEqual({ eventId: 'm-root', primaryEmail: 'owner@example.com', messageId: 'm-root', threadId: 'thread-1' });
    expect(mapToReceiveEmailReplyInput(reply)).toEqual({ eventId: 'm-reply', messageId: 'm-reply' });
  });

  it('filters root or reply shape, sender, and text as the Go example does', () => {
    const startFilter = newStartTriggerFilter({ messageMatcher: { messageContains: 'SHIPPED', senderEmails: ['ada@example.com'] } });
    const replyFilter = newReplyTriggerFilter({ replyMatcher: {} });
    const root = event('m-root', {});
    const reply = event('m-reply', { isReply: true });
    expect([startFilter(root), startFilter(reply), replyFilter(root), replyFilter(reply)]).toEqual([true, false, false, true]);
    expect(startFilter({ ...root, payload: { ...root.payload, from: 'eve@example.com' } })).toBe(false);
    expect(startFilter({ ...root, payload: { ...root.payload, snippet: 'nothing' } })).toBe(false);
    expect(replyFilter({ ...reply, id: '' })).toBe(false);
    expect(replyFilter({ ...reply, payload: { ...reply.payload, primaryEmail: '' } })).toBe(false);
  });
});
