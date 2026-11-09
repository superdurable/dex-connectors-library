// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetInvoiceStep, type Connection, type GetInvoiceInput, type InvoiceSummary } from '@superdurable/connector-sage-intacct';

/** Static Dex Web connection for the invoice example. */
export const ConnectionName = 'sage-intacct';
const resultAttribute = new Attribute<InvoiceSummary>('sage-intacct-typescript-invoice', jsonCodec<InvoiceSummary>());

/** Read one invoice visible to the authorized Sage Intacct user. */
export class InvoiceFlow implements Flow<GetInvoiceInput> {
  private readonly complete = new CompleteInvoice();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Sage Intacct connection name differs');
    this.read = newGetInvoiceStep<GetInvoiceInput>({
      stepType: 'CallGetInvoice',
      annotations: { groupId: 'provider', groupLabel: 'Sage Intacct', explanation: 'Read the selected accounts receivable invoice.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'SageIntacctInvoiceInspectionTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetInvoiceInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the invoice and complete the Flow."
class CompleteInvoice implements Step<QueryResult<InvoiceSummary>> {
  readonly inputCodec = jsonCodec<QueryResult<InvoiceSummary>>();
  getStepType(): string { return 'CompleteInvoice'; }
  execute(context: Context, result: QueryResult<InvoiceSummary>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
