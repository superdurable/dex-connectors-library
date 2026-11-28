// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListInvoicesStep, type Connection, type ListInvoicesInput, type InvoiceList } from '@superdurable/connector-sage-intacct';

/** Static Dex Web connection for the invoice example. */
export const ConnectionName = 'sage-intacct';
const resultAttribute = new Attribute<InvoiceList>('sage-intacct-typescript-invoices', jsonCodec<InvoiceList>());

/** Read the first page of invoices visible to the authorized Sage Intacct user. */
export class InvoiceListingFlow implements Flow<ListInvoicesInput> {
  private readonly complete = new CompleteInvoice();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Sage Intacct connection name differs');
    this.read = newListInvoicesStep<ListInvoicesInput>({
      stepType: 'CallListInvoices',
      annotations: { groupId: 'provider', groupLabel: 'Sage Intacct', explanation: 'Read the first page of accounts receivable invoices.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'SageIntacctInvoiceListingTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListInvoicesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the invoice and complete the Flow."
class CompleteInvoice implements Step<QueryResult<InvoiceList>> {
  readonly inputCodec = jsonCodec<QueryResult<InvoiceList>>();
  getStepType(): string { return 'CompleteInvoice'; }
  execute(context: Context, result: QueryResult<InvoiceList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
