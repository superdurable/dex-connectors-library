// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerEvent } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import { SlackClient } from '../src/client.js';
import { ButtonClickedTriggerConfigurationSchema, ChannelThreadCreatedTriggerConfigurationSchema, newConnection, type ButtonClickEvent, type MessageEvent } from '../src/index.js';
import {
  ButtonApprovalFlow, ButtonTriggerBinding, ConnectionName, StartTriggerBinding, decideButtonClick, mapToFlowInput, mapToReceiveButtonDecisionInput, newButtonTriggerFilter,
  newStartTriggerFilter, resolveButtonFlowId, resolveFlowId, type ApprovalState,
} from '../examples/button-approval/flow/workflow.js';

const credentials = { botToken: new SecretString('b'), userToken: new SecretString('u') };
const flow = new ButtonApprovalFlow(newConnection(new SlackClient({}, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }), { provider: 'slack', name: ConnectionName }));

function visibleFields(ui: ConnectorConfigurationUI | undefined): string[] {
  return (ui?.units ?? []).map(unit => `${unit.id}:${unit.unitId}:${unit.required ? 'required' : 'optional'}:${unit.bindings.map(binding => `${binding.port}=${binding.jsonPointer}`).join(',')}`);
}

function configurationFromUnits(ui: ConnectorConfigurationUI | undefined): Record<string, unknown> {
  const samples: Record<string, unknown> = { channelId: 'C1', memberIds: ['U2'], text: 'request approval' };
  const configuration: Record<string, unknown> = {};
  for (const binding of (ui?.units ?? []).flatMap(unit => unit.bindings)) {
    const segments = binding.jsonPointer.split('/').slice(1);
    let target = configuration;
    for (const segment of segments.slice(0, -1)) target = (target[segment] ??= {}) as Record<string, unknown>;
    target[segments.at(-1)!] = samples[binding.port];
  }
  return configuration;
}

const root = (payload: Partial<MessageEvent> = {}): TriggerEvent<MessageEvent> => ({ id: 'Ev-root', occurredAt: new Date(0), payload: { teamId: 'T1', channelId: 'C1', timestamp: '1.0', threadTimestamp: '1.0', userId: 'U1', text: 'please request approval', ...payload } });
const click = (payload: Partial<ButtonClickEvent> = {}, id = 'T1:C1:2.0:approve:3.0'): TriggerEvent<ButtonClickEvent> => ({ id, occurredAt: new Date(0),
  payload: { teamId: 'T1', channelId: 'C1', messageTimestamp: '2.0', threadTimestamp: '1.0', userId: 'U2', actionId: 'approve', value: '1.0', actionTimestamp: '3.0', ...payload } });

describe('Slack button approval example', () => {
  it('declares the request and button bindings and enumerates every visible configuration field', () => {
    const [start, button] = flow.getConnectorTriggerBindings();
    expect(flow.getConnectorTriggerBindings()).toHaveLength(2);
    for (const binding of [start!, button!]) expect(mustTriggerBindingDefinition(binding)).toEqual(binding);
    expect([start!.bindingName, start!.connectionName, start!.definition.trigger.triggerName]).toEqual([StartTriggerBinding, ConnectionName, 'channelThreadCreated']);
    expect([button!.bindingName, button!.connectionName, button!.definition.trigger.triggerName]).toEqual([ButtonTriggerBinding, ConnectionName, 'buttonClicked']);
    expect(visibleFields(start!.configurationUI)).toEqual([
      'channel:channelPicker:required:channelId=/channelId',
      'message:textInput:optional:text=/threadTriggerMatcher/messageContains',
      'members:memberPicker:optional:memberIds=/threadTriggerMatcher/posterUserIds',
    ]);
    expect(visibleFields(button!.configurationUI)).toEqual(['channel:channelPicker:required:channelId=/channelId', 'approvers:memberPicker:optional:memberIds=/clickerUserIds']);
    const units = flow.getConnectorTriggerBindings().flatMap(binding => binding.configurationUI?.units ?? []);
    for (const unit of units) {
      expect(unit.description.length, unit.id).toBeGreaterThan(40);
      if (!unit.required) expect(unit.description, unit.id).toMatch(/leave (blank|empty)|blank matches|empty selection/);
      if (unit.unitId === 'channelPicker') expect(unit.description, unit.id).toMatch(/stable channel ID/);
    }
    expect(JSON.stringify(units)).not.toMatch(/token|secret/i);
    expect(ChannelThreadCreatedTriggerConfigurationSchema.parse(configurationFromUnits(start!.configurationUI))).toEqual({ channelId: 'C1', threadTriggerMatcher: { messageContains: 'request approval', posterUserIds: ['U2'] } });
    expect(ButtonClickedTriggerConfigurationSchema.parse(configurationFromUnits(button!.configurationUI))).toEqual({ channelId: 'C1', clickerUserIds: ['U2'] });
  });

  it('resolves the same Flow ID for a root and its button click and maps only safe identifiers', () => {
    expect(resolveFlowId(root())).toBe('slack-button-approval-T1-C1-1.0');
    expect(resolveButtonFlowId(click({ threadTimestamp: '' }))).toBe(resolveFlowId(root()));
    expect(mapToFlowInput(root())).toEqual({ eventId: 'Ev-root', teamId: 'T1', channelId: 'C1', threadTimestamp: '1.0', requesterUserId: 'U1' });
    expect(mapToReceiveButtonDecisionInput(click())).toEqual({ eventId: 'T1:C1:2.0:approve:3.0', actionId: 'approve', userId: 'U2', messageTimestamp: '2.0' });
  });

  it('accepts only the first click on the posted message', () => {
    const waiting: ApprovalState = { input: mapToFlowInput(root()), status: 'waitingForDecision', buttonMessageTimestamp: '2.0' };
    const reject = { eventId: 'click-reject', actionId: 'reject', userId: 'U2', messageTimestamp: '2.0' };
    expect(() => decideButtonClick({ ...waiting, status: 'postingButtons' }, reject)).toThrow(/still being posted/);
    expect(decideButtonClick(waiting, { ...reject, messageTimestamp: '9.0' }).result).toEqual({ accepted: false, duplicate: false, status: 'waitingForDecision' });
    const decided = decideButtonClick(waiting, reject);
    expect(decided).toEqual({ state: { ...waiting, status: 'postingDecision', decision: 'rejected', decisionEventId: 'click-reject', deciderUserId: 'U2' }, result: { accepted: true, duplicate: false, status: 'postingDecision' } });
    expect(decideButtonClick(decided.state, reject).result).toEqual({ accepted: false, duplicate: true, status: 'postingDecision' });
    expect(decideButtonClick(decided.state, { ...reject, eventId: 'later', actionId: 'approve' }).result).toEqual({ accepted: false, duplicate: false, status: 'postingDecision' });
  });

  it('filters Approve and Reject clicks from deciders and matching root messages as Go does', () => {
    const buttonFilter = newButtonTriggerFilter({ channelId: 'C1', clickerUserIds: ['U2'] });
    expect([click(), click({ actionId: 'reject' })].map(buttonFilter)).toEqual([true, true]);
    expect([click({ actionId: 'other' }), click({ userId: 'U3' }), click({ channelId: 'C2' }), click({ value: '' }), click({}, '')].map(buttonFilter)).toEqual([false, false, false, false, false]);
    const startFilter = newStartTriggerFilter({ channelId: 'C1', threadTriggerMatcher: { messageContains: 'REQUEST approval', posterUserIds: ['U1'] } });
    expect([root(), root({ threadTimestamp: '0.5' }), root({ userId: 'U3' }), root({ text: 'hello' })].map(startFilter)).toEqual([true, false, false, false]);
  });
});
