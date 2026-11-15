// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import { describe, expect, it } from 'vitest';
import { GeneratedMock } from '../src/mock.js';
import type { MessageReceivedEvent } from '../src/index.js';
import {
  ConnectionName, FlowType, InboundMessageFlow, MessageReceivedTriggerBinding, acceptEveryInboundMessage, invalidInputReason, isOptOutText,
  mapToFlowInput, mapToGetMessageInput, resolveFlowId,
} from '../examples/message-received/flow/workflow.js';

const sid = 'SM' + '0123456789abcdef0123456789abcdef';
const zero = '0001-01-01T00:00:00Z';
const received: MessageReceivedEvent = {
  message: { sid, accountSid: 'AC' + '0'.repeat(32), to: '+14155550100', from: '+14155550123', direction: 'inbound', status: 'received', createdAt: zero, sentAt: zero, updatedAt: zero },
  body: 'hello',
};
const triggerEvent = { id: `twilio-message-${sid}`, occurredAt: new Date('2026-11-15T12:00:00Z'), payload: received };

describe('message-received example', () => {
  it('names one Flow per message and reads it back', () => {
    expect(resolveFlowId(triggerEvent)).toBe(`twilio-inbound-message-twilio-message-${sid}`);
    expect(acceptEveryInboundMessage()).toBe(true);
    expect(mapToFlowInput(triggerEvent)).toEqual({ triggerEventId: `twilio-message-${sid}`, received });
    expect(mapToGetMessageInput(mapToFlowInput(triggerEvent))).toEqual({ messageSid: sid });
  });

  it('matches opt-out keywords on the whole trimmed text', () => {
    for (const keyword of ['STOP', ' stop ', 'Unsubscribe', 'optout']) expect(isOptOutText(keyword), keyword).toBe(true);
    for (const text of ['please stop', 'hello', '']) expect(isOptOutText(text), text).toBe(false);
  });

  it('refuses incomplete deliveries', () => {
    expect(invalidInputReason({ triggerEventId: 't', received })).toBe('');
    expect(invalidInputReason({ triggerEventId: ' ', received })).toBe('triggerEventId is required');
    expect(invalidInputReason({ triggerEventId: 't', received: { message: { ...received.message, to: '' } } })).toContain('received.message.to');
    expect(invalidInputReason({ triggerEventId: 't', received: { message: { ...received.message, direction: 'outbound-api' } } })).toContain('inbound');
  });

  it('declares the static binding with the receiving address unit', () => {
    const flow = new InboundMessageFlow(new GeneratedMock(ConnectionName).connection());
    expect(() => new Registry([flow])).not.toThrow();
    expect(flow.getFlowType()).toBe(FlowType);
    const [binding] = flow.getConnectorTriggerBindings();
    expect([binding!.definition.trigger.triggerName, binding!.connectionName, binding!.bindingName]).toEqual(['messageReceived', ConnectionName, MessageReceivedTriggerBinding]);
    const [unit] = binding!.configurationUI!.units;
    expect([unit!.id, unit!.unitId, unit!.label, unit!.required]).toEqual(['receivingNumber', 'receivingAddressInput', 'Receiving number', false]);
    expect(unit!.bindings).toEqual([{ port: 'to', jsonPointer: '/to' }]);
    expect(unit!.description).toContain('https://console.twilio.com/us1/develop/phone-numbers/manage/incoming');
    expect(unit!.description).toMatch(/blank.*baseline/s);
  });
});
