// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import type { InvoiceChangedEvent } from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { ConnectionName, InvoiceChangedFlow, InvoiceChangedTriggerBinding, acceptEveryInvoiceChange, mapToFlowInput, mapToGetInvoiceInput, resolveFlowId } from '../examples/invoice-changed/flow/workflow.js';

const event: TriggerEvent<InvoiceChangedEvent> = {
  id: 'qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852', occurredAt: new Date('2026-11-02T16:00:02Z'),
  payload: { realmId: '9341453050298464', environment: 'production', changeKind: 'updated', invoice: { invoiceId: '132', syncToken: '1', customerId: '58', lines: [], totalAmount: '25.50', balance: '0' } },
};

it('names one Flow per invoice version and keeps the event ID beside the change', () => {
  expect(resolveFlowId(event)).toBe('quickbooks-invoice-change-qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852');
  const input = mapToFlowInput(event);
  expect(input).toEqual({ triggerEventId: event.id, change: event.payload });
  expect(mapToGetInvoiceInput(input)).toEqual({ invoiceId: '132' });
  expect(acceptEveryInvoiceChange()).toBe(true);
});

it('declares the static binding and requires its connection', () => {
  const flow = new InvoiceChangedFlow(new GeneratedMock(ConnectionName).connection());
  expect(flow.getFlowType()).toBe('QuickBooksInvoiceChange');
  const [binding] = flow.getConnectorTriggerBindings();
  expect([binding?.connectionName, binding?.bindingName]).toEqual([ConnectionName, InvoiceChangedTriggerBinding]);
  expect(() => new InvoiceChangedFlow(new GeneratedMock('other').connection())).toThrow();
});
