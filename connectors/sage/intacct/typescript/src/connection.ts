// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef } from '@superdurable/connectors-sdk';
import { Connection, type Operations } from './generated/connection.js';

export { Connection };
type InvoiceOperations = Pick<Operations, 'getInvoice'> & Partial<Pick<Operations, 'listInvoices'>>;

/** Bind a client or lookup-only provider to a named Sage Intacct connection. */
export function newConnection(client: InvoiceOperations, reference: ConnectionRef): Connection {
  if (!client) throw new TypeError('connector client is required');
  if (client.listInvoices) return new Connection(reference, client as Operations);
  return new Connection(reference, {
    getInvoice: () => client.getInvoice(),
    listInvoices() { throw new TypeError('Supply listInvoices to bind the invoice listing Step'); },
  });
}

/** Bind the supplied scripted queries. Lookup-only scripts can serve invoice lookup Steps. */
export function newConnectionWithOperations(operations: InvoiceOperations, reference: ConnectionRef): Connection {
  return newConnection(operations, reference);
}
