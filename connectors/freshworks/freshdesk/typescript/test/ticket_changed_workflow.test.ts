// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import type { TicketChangedEvent } from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, mapToGetTicketInput, resolveFlowId } from '../examples/ticket-changed/flow/workflow.js';

const event: TriggerEvent<TicketChangedEvent> = {
  id: 'freshdesk-ticket-42-1772442005', occurredAt: new Date('2026-03-02T09:00:05Z'),
  payload: { changeKind: 'created', ticket: { id: 42, status: 2, priority: 1, createdAt: '2026-03-02T09:00:05Z', updatedAt: '2026-03-02T09:00:05Z' } },
};

it('names one Flow per ticket version and keeps the event ID beside the change', () => {
  expect(resolveFlowId(event)).toBe('freshdesk-ticket-change-freshdesk-ticket-42-1772442005');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change: event.payload });
  expect(mapToGetTicketInput(input)).toEqual({ ticketId: 42 });
  expect(acceptEveryTicketChange()).toBe(true);
});

it('declares the static binding and requires its connection', () => {
  const flow = new TicketChangedFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('FreshdeskTicketChange');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName]).toEqual([ConnectionName, TicketChangedTriggerBinding]);
  expect(() => new TicketChangedFlow(new GeneratedMock('other').connection())).toThrow();
});
