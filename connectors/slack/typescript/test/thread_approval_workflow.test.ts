// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerEvent } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { SlackClient } from '../src/client.js';
import { ChannelThreadCreatedTriggerConfigurationSchema, ThreadReplyCreatedTriggerConfigurationSchema, newConnection, type MessageEvent } from '../src/index.js';
import {
  ConnectionName, PostCompletionConfigurationUI, ReplyTriggerBinding, StartTriggerBinding, ThreadApprovalFlow, mapToFlowInput, mapToReceiveThreadReplyInput,
  newReplyTriggerFilter, newStartTriggerFilter, resolveFlowId,
} from '../examples/thread-approval/flow/workflow.js';

const credentials = { botToken: new SecretString('b'), userToken: new SecretString('u') };
const flow = new ThreadApprovalFlow(newConnection(new SlackClient({}, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }), { provider: 'slack', name: ConnectionName }));

/** Visible fields as id, unit, required flag, and the JSON pointer each writes. */
function visibleFields(ui: ConnectorConfigurationUI | undefined): string[] {
  return (ui?.units ?? []).map(unit => `${unit.id}:${unit.unitId}:${unit.required ? 'required' : 'optional'}:${unit.bindings.map(binding => `${binding.port}=${binding.jsonPointer}`).join(',')}`);
}

/** Build the configuration a binding's units write, setting each pointer to a sample of its port. */
function configurationFromUnits(ui: ConnectorConfigurationUI | undefined): Record<string, unknown> {
  const samples: Record<string, unknown> = { channelId: 'C1', memberIds: ['U2'], text: 'approved' };
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

const event = (id: string, payload: Partial<MessageEvent>): TriggerEvent<MessageEvent> => ({ id, occurredAt: new Date(0), payload: { teamId: 'T1', channelId: 'C1', timestamp: '1.0', threadTimestamp: '1.0', userId: 'U1', text: '', ...payload } });

describe('Slack thread approval example', () => {
  it('declares both static bindings and enumerates every visible configuration field', () => {
    const [start, reply] = flow.getConnectorTriggerBindings();
    expect(flow.getConnectorTriggerBindings()).toHaveLength(2);
    for (const binding of [start!, reply!]) expect(mustTriggerBindingDefinition(binding)).toEqual(binding);
    expect([start!.bindingName, start!.connectionName, start!.definition.trigger.triggerName]).toEqual([StartTriggerBinding, ConnectionName, 'channelThreadCreated']);
    expect([reply!.bindingName, reply!.connectionName, reply!.definition.trigger.triggerName]).toEqual([ReplyTriggerBinding, ConnectionName, 'threadReplyCreated']);
    expect(visibleFields(start!.configurationUI)).toEqual([
      'channel:channelPicker:required:channelId=/channelId',
      'message:textInput:optional:text=/threadTriggerMatcher/messageContains',
      'members:memberPicker:optional:memberIds=/threadTriggerMatcher/posterUserIds',
    ]);
    expect(visibleFields(reply!.configurationUI)).toEqual([
      'channel:channelPicker:required:channelId=/channelId',
      'message:textInput:optional:text=/threadReplyMatcher/messageContains',
      'approvers:memberPicker:required:memberIds=/threadReplyMatcher/posterUserIds',
    ]);
    expect(visibleFields(PostCompletionConfigurationUI)).toEqual(['completionText:textInput:required:text=/text']);
  });

  it('explains picker output and blank behavior and writes configurations the connector accepts', () => {
    const units = [...flow.getConnectorTriggerBindings().flatMap(binding => binding.configurationUI?.units ?? []), ...PostCompletionConfigurationUI.units];
    for (const unit of units) {
      expect(unit.description.length, unit.id).toBeGreaterThan(40);
      if (!unit.required) expect(unit.description, unit.id).toMatch(/leave (blank|empty)/);
      if (unit.unitId === 'channelPicker') expect(unit.description, unit.id).toMatch(/stable channel ID/);
    }
    expect(JSON.stringify(units)).not.toMatch(/token|secret/i);
    const [start, reply] = flow.getConnectorTriggerBindings();
    expect(ChannelThreadCreatedTriggerConfigurationSchema.parse(configurationFromUnits(start!.configurationUI))).toEqual({ channelId: 'C1', threadTriggerMatcher: { messageContains: 'approved', posterUserIds: ['U2'] } });
    expect(ThreadReplyCreatedTriggerConfigurationSchema.parse(configurationFromUnits(reply!.configurationUI))).toEqual({ channelId: 'C1', threadReplyMatcher: { messageContains: 'approved', posterUserIds: ['U2'] } });
    expect(ThreadReplyCreatedTriggerConfigurationSchema.safeParse({ channelId: 'C1', threadReplyMatcher: { messageContains: 'approved' } }).success).toBe(false);
  });

  it('resolves one Flow ID for a root and its reply and maps only safe identifiers', () => {
    const root = event('Ev-root', { userId: 'U1', text: 'Request Approval for this order' });
    const reply = event('Ev-reply', { timestamp: '2.0', userId: 'U2', text: 'APPROVE' });
    expect(resolveFlowId(root)).toBe('slack-thread-approval-T1-C1-1.0');
    expect(resolveFlowId(reply)).toBe(resolveFlowId(root));
    expect(mapToFlowInput(root)).toEqual({ eventId: 'Ev-root', teamId: 'T1', channelId: 'C1', threadTimestamp: '1.0' });
    expect(mapToReceiveThreadReplyInput(reply)).toEqual({ eventId: 'Ev-reply', userId: 'U2' });
  });

  it('filters channel, poster, text, and root or reply shape as Go does', () => {
    const startFilter = newStartTriggerFilter({ channelId: 'C1', threadTriggerMatcher: { messageContains: 'request approval', posterUserIds: ['U1'] } });
    const replyFilter = newReplyTriggerFilter({ channelId: 'C1', threadReplyMatcher: { messageContains: 'approve', posterUserIds: ['U2'] } });
    const root = event('Ev-root', { userId: 'U1', text: 'Request Approval for this order' });
    const reply = event('Ev-reply', { timestamp: '2.0', userId: 'U2', text: 'APPROVE' });
    expect([startFilter(root), startFilter(reply), replyFilter(root), replyFilter(reply)]).toEqual([true, false, false, true]);
    expect(replyFilter({ ...reply, payload: { ...reply.payload, channelId: 'C2' } })).toBe(false);
    expect(replyFilter({ ...reply, payload: { ...reply.payload, userId: 'U3' } })).toBe(false);
    expect(replyFilter({ ...reply, payload: { ...reply.payload, text: 'deny' } })).toBe(false);
    expect(startFilter({ ...root, id: '' })).toBe(false);
  });
});
