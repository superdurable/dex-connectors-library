// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import type { TicketChangedEvent } from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import {
  ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, mapToGetTicketInput, resolveFlowId, validateTicketChange,
} from '../examples/ticket-changed/flow/workflow.js';

const event: TriggerEvent<TicketChangedEvent> = {
  id: 'gorgias-event-9003', occurredAt: new Date('2026-11-02T09:00:00Z'),
  payload: { eventId: 9003, eventType: 'ticket-closed', ticketId: 7002, occurredAt: '2026-11-02T09:00:00Z', userId: 12 },
};

it('names one Flow per Gorgias event and reads the ticket it names', () => {
  expect(resolveFlowId(event)).toBe('gorgias-ticket-change-gorgias-event-9003');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, event: event.payload });
  expect(mapToGetTicketInput(input)).toEqual({ ticketId: 7002 });
  expect(acceptEveryTicketChange()).toBe(true);
});

it('refuses an incomplete event', () => {
  const input = mapToFlowInput(event);
  expect(validateTicketChange(input)).toBe('');
  expect(validateTicketChange({ ...input, triggerEventId: ' ' })).toMatch(/triggerEventId/);
  expect(validateTicketChange({ ...input, event: { ...input.event, eventId: 0 } })).toMatch(/eventId/);
  expect(validateTicketChange({ ...input, event: { ...input.event, ticketId: 0 } })).toMatch(/ticketId/);
  expect(validateTicketChange({ ...input, event: { ...input.event, eventType: 'ticket-message-created' } })).toMatch(/eventType/);
});

it('declares the static binding without picker units and requires its connection', () => {
  const flow = new TicketChangedFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('GorgiasTicketChangeTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName, binding?.configurationUI]).toEqual([ConnectionName, TicketChangedTriggerBinding, undefined]);
  expect(binding?.definition.trigger.triggerName).toBe('ticketChanged');
  expect(() => new TicketChangedFlow(new GeneratedMock('other').connection())).toThrow();
});
