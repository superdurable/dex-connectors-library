// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetInvoiceStep, type Connection, type GetInvoiceInput, type Invoice } from '@superdurable/connector-freshbooks';

/** Static Dex Web connection for the Invoice example. */
export const ConnectionName = 'freshbooks';
const resultAttribute = new Attribute<Invoice>('freshbooks-typescript-invoice', jsonCodec<Invoice>());

/** Read the requested FreshBooks invoice. */
export class InvoiceFlow implements Flow<GetInvoiceInput> {
  private readonly complete = new CompleteInvoice();
  private readonly read;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Invoice connection name differs');
    this.read = newGetInvoiceStep<GetInvoiceInput>({
      stepType: 'CallGetInvoice',
      annotations: { groupId: 'provider', groupLabel: 'FreshBooks', explanation: 'Read the requested FreshBooks invoice.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteInvoice'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FreshBooksInvoiceLookupTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetInvoiceInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the invoice and complete the Flow."
class CompleteInvoice implements Step<QueryResult<Invoice>> {
  readonly inputCodec = jsonCodec<QueryResult<Invoice>>();
  getStepType(): string { return 'CompleteInvoice'; }
  execute(context: Context, result: QueryResult<Invoice>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
