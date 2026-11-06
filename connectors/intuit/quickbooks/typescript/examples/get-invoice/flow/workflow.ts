// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetInvoiceStep, type Connection, type GetInvoiceInput, type Invoice } from '@superdurable/connector-intuit-quickbooks';

/** Static Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'quickbooks-accounting';
/** Nonsecret invoice request retained for inspection. */
export const invoiceRequest = new Attribute<GetInvoiceInput>('quickbooks-invoice-request', jsonCodec<GetInvoiceInput>());
/** Provider invoice retained for inspection. */
export const invoiceResult = new Attribute<Invoice>('quickbooks-invoice-result', jsonCodec<Invoice>());

/** Read one QuickBooks invoice through a Dex connector Step. */
export class GetQuickBooksInvoiceFlow implements Flow<GetInvoiceInput> {
  private readonly prepare = new PrepareInvoiceLookup();
  private readonly complete = new CompleteInvoiceLookup();
  /** Bind the named QuickBooks connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('QuickBooks connection is required'); }
  /** Stable Flow type. */ getFlowType(): string { return 'GetQuickBooksInvoiceFlow'; }
  /** Register the application and provider Steps. */
  getSteps(): StepList<GetInvoiceInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetInvoiceStep({ stepType: 'GetQuickBooksInvoice', annotations: { groupId: 'quickbooks-invoice', groupLabel: 'QuickBooks Invoice', explanation: 'Read one invoice from the connected QuickBooks company.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [invoiceRequest, invoiceResult] }; }
}

/** Validate an invoice ID before the provider call. */
// dex:group group-id:quickbooks-invoice group-label:"QuickBooks Invoice"
// dex:explanation text:"Validate the invoice ID before reading the provider invoice."
export class PrepareInvoiceLookup implements Step<GetInvoiceInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareQuickBooksInvoiceLookup'; }
  /** Persist the safe ID and enter the connector Step. */
  execute(context: Context, input: GetInvoiceInput): StepDecision {
    if (!/^[0-9]{1,20}$/.test(input.invoiceId)) return forceFail('QuickBooks invoice ID must contain one to twenty digits');
    invoiceRequest.set(context, input);
    return goTo(new StepRef('GetQuickBooksInvoice'), input);
  }
}

/** Finish with the invoice returned by QuickBooks. */
// dex:group group-id:quickbooks-invoice group-label:"QuickBooks Invoice"
// dex:explanation text:"Persist the QuickBooks invoice and complete the Flow."
export class CompleteInvoiceLookup implements Step<QueryResult<Invoice>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteQuickBooksInvoiceLookup'; }
  /** Retain the nonsecret invoice and complete. */ execute(context: Context, result: QueryResult<Invoice>): StepDecision { invoiceResult.set(context, result.value); return gracefulComplete(result.value); }
}
