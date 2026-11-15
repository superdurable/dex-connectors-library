// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Registry } from '@superdurable/dex';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { GeneratedMock } from '../src/mock.js';
import type { TicketChangedEvent } from '../src/index.js';
import {
  ConnectionName, TicketChangeFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, mapToGetTicketInput, resolveFlowId,
} from '../examples/ticket-changed/flow/workflow.js';

const change: TriggerEvent<TicketChangedEvent> = {
  id: 'zdesk-0123456789abcdef0123456789abcdef', occurredAt: new Date('2026-03-02T09:00:05Z'),
  payload: {
    orgId: '2389290', departmentId: '1892000000006907', changeKind: 'updated',
    ticket: { id: '1892000000099001', subject: 'Billing question', status: 'Open', createdAt: '2026-03-02T09:00:05Z', modifiedAt: '2026-03-02T09:00:05Z' },
  },
};

it('maps each ticket version to one Flow that reads the ticket back', () => {
  expect(acceptEveryTicketChange()).toBe(true);
  expect(resolveFlowId(change)).toBe('zoho-desk-ticket-change-zdesk-0123456789abcdef0123456789abcdef');
  const input = mapToFlowInput(change);
  expect(input).toEqual({ triggerEventId: change.id, change: change.payload });
  expect(mapToGetTicketInput(input)).toEqual({ ticketId: '1892000000099001' });
});

it('declares the desk-ticket-changes binding with one guided optional department picker', () => {
  const flow = new TicketChangeFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('ZohoDeskTicketChangeTypeScript');
  const [binding, ...others] = flow.getConnectorTriggerBindings();
  expect(others).toEqual([]);
  expect(binding).toMatchObject({ connectionName: ConnectionName, bindingName: TicketChangedTriggerBinding, definition: { trigger: { connectorId: 'zoho-desk', triggerName: 'ticketChanged' } } });
  expect(binding!.configurationUI!.units).toEqual([expect.objectContaining({
    id: 'watchedDepartment', unitId: 'departmentPicker', label: 'Watched department', required: false,
    bindings: [{ port: 'departmentId', jsonPointer: '/departmentId' }, { port: 'departmentName', jsonPointer: '/departmentName' }],
  })]);
  for (const guidance of ["connection's organization", 'department ID and name', 'blank', 'every department', 'baseline', 'Restart the Worker']) {
    expect(binding!.configurationUI!.units[0]!.description).toContain(guidance);
  }
});

it('registers with its static connection and refuses another', () => {
  expect(() => new Registry([new TicketChangeFlow(new GeneratedMock(ConnectionName).connection())])).not.toThrow();
  expect(() => new TicketChangeFlow(new GeneratedMock('other').connection())).toThrow('zoho-desk-ticket-watcher');
});
