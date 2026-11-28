// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListInvoicesStep, type Connection, type ListInvoicesInput, type InvoicePage } from '@superdurable/connector-freshbooks';

import { ConnectionName } from './flow.js';
const resultAttribute = new Attribute<InvoicePage>('freshbooks-typescript-invoice-page', jsonCodec<InvoicePage>());

/** Read one page of FreshBooks invoices. */
export class InvoicePageFlow implements Flow<ListInvoicesInput> {
  private readonly complete = new CompleteInvoicePage();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Invoice connection name differs');
    this.read = newListInvoicesStep<ListInvoicesInput>({
      stepType: 'CallListInvoices',
      annotations: { groupId: 'provider', groupLabel: 'FreshBooks', explanation: 'Read one page of FreshBooks invoices.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteInvoicePage'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FreshBooksInvoiceListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListInvoicesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the invoice and complete the Flow."
class CompleteInvoicePage implements Step<QueryResult<InvoicePage>> {
  readonly inputCodec = jsonCodec<QueryResult<InvoicePage>>();
  getStepType(): string { return 'CompleteInvoicePage'; }
  execute(context: Context, result: QueryResult<InvoicePage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
