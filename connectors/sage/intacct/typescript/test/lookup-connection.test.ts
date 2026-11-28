// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { Mock } from '../src/mock.js';
import { newConnectionWithOperations } from '../src/connection.js';
import { InvoiceFlow } from '../examples/invoice-inspection/flow.js';

it('binds an existing lookup-only script to the invoice lookup Flow', () => {
  const scripted = new Mock('sage-intacct').connection();
  const lookup = { getInvoice: () => scripted.operations.getInvoice() };
  const bound = newConnectionWithOperations(lookup, scripted.reference);
  expect(new InvoiceFlow(bound).getFlowType()).toBe('SageIntacctInvoiceInspectionTypeScript');
  expect(bound.operations.getInvoice()).toBe(lookup.getInvoice());
  expect(() => bound.operations.listInvoices()).toThrow('listInvoices');
});
