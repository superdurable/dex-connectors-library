// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import type { TicketChangedEvent } from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, mapToGetTicketInput, resolveFlowId } from '../examples/ticket-changed/flow/workflow.js';

const event: TriggerEvent<TicketChangedEvent> = {
  id: 'zendesk-ticket-897cba421f2a136d36a4375ed6386bdb', occurredAt: new Date('2026-03-02T08:58:00Z'),
  payload: { changeKind: 'created', ticket: { id: 42, agentUrl: 'https://acme.zendesk.com/agent/tickets/42', status: 'new', createdAt: '2026-03-02T08:58:00Z', updatedAt: '2026-03-02T08:58:00Z' } },
};

it('names one Flow per ticket version and reads the newest comment', () => {
  expect(resolveFlowId(event)).toBe(`zendesk-ticket-change-${event.id}`);
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change: event.payload });
  expect(mapToGetTicketInput(input)).toEqual({ ticketId: 42, latestCommentLimit: 1 });
  expect(acceptEveryTicketChange()).toBe(true);
});

it('declares the static binding without a configuration unit and requires its connection', () => {
  const flow = new TicketChangedFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('ZendeskTicketChangeTypeScript');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName, binding?.configurationUI]).toEqual([ConnectionName, TicketChangedTriggerBinding, undefined]);
  expect(() => new TicketChangedFlow(new GeneratedMock('other').connection())).toThrow();
});
