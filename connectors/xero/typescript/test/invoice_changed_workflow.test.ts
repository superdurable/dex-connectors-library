// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import type { InvoiceChangedEvent } from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { ConnectionName, InvoiceChangedFlow, InvoiceChangedTriggerBinding, acceptEveryInvoiceChange, mapToFlowInput, mapToGetInvoiceInput, resolveFlowId } from '../examples/invoice-changed/flow/workflow.js';

const event: TriggerEvent<InvoiceChangedEvent> = {
  id: 'xero-invoice-2320ceb3271e677af3a09fb91ca96e10', occurredAt: new Date('2026-10-01T09:00:02.5Z'),
  payload: { invoice: { invoiceId: '8a1d0e2f-0000-4000-8000-00000000000b', type: 'ACCREC', status: 'DRAFT', contact: { contactId: '' }, total: '20.00', updatedAt: '2026-10-01T09:00:02.5Z' } },
};

it('names one Flow per change and keeps the event ID beside the change', () => {
  expect(resolveFlowId(event)).toBe('xero-invoice-change-xero-invoice-2320ceb3271e677af3a09fb91ca96e10');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change: event.payload });
  expect(mapToGetInvoiceInput(input)).toEqual({ invoiceId: '8a1d0e2f-0000-4000-8000-00000000000b' });
  expect(acceptEveryInvoiceChange()).toBe(true);
});

it('declares the static binding with its invoice filter unit and requires its connection', () => {
  const flow = new InvoiceChangedFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('XeroInvoiceChange');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName]).toEqual([ConnectionName, InvoiceChangedTriggerBinding]);
  const [unit] = binding?.configurationUI?.units ?? [];
  expect(unit?.unitId).toBe('invoiceChangeFilter');
  expect(unit?.bindings).toEqual([{ port: 'type', jsonPointer: '/type' }, { port: 'statuses', jsonPointer: '/statuses' }]);
  expect(() => new InvoiceChangedFlow(new GeneratedMock('other').connection())).toThrow();
});
