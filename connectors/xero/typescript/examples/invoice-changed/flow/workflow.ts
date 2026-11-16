// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineInvoiceChangedTriggerBinding, newGetInvoiceStep, type Connection, type GetInvoiceInput, type Invoice, type InvoiceChangedEvent } from '@superdurable/connector-xero';

/** Static Dex Web connection for Xero. */
export const ConnectionName = 'xero-invoice-watcher';
/** The stable invoiceChanged binding that starts this Flow. */
export const InvoiceChangedTriggerBinding = 'invoice-changes';
const readChangedInvoiceStepType = 'ReadChangedInvoice';

/** One invoiceChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface InvoiceChange { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered change. */ change: InvoiceChangedEvent }

/** The Flow result: the changed invoice read back from Xero. */
export interface InvoiceChangeOutcome {
  /** The changed invoice. */ invoiceId: string;
  /** The invoice status in the delivered change. */ deliveredStatus: string;
  /** The invoice read back from Xero, which a later change may already have updated. */ current: Invoice;
}

/** The delivered change. */ export const invoiceChangeAttribute = new Attribute<InvoiceChange>('xero-invoice-change', jsonCodec<InvoiceChange>());
/** The recorded outcome. */ export const invoiceChangeOutcomeAttribute = new Attribute<InvoiceChangeOutcome>('xero-invoice-change-outcome', jsonCodec<InvoiceChangeOutcome>());

/** Records one Xero invoice change. */
export class InvoiceChangedFlow implements Flow<InvoiceChange> {
  private readonly record = new RecordInvoiceChange();
  private readonly complete = new CompleteInvoiceChange();
  /** Bind the Xero connection at registration time. */ constructor(private readonly connection: Connection) {
    if (connection?.reference.name !== ConnectionName) throw new TypeError('invoice change Flow requires the xero-invoice-watcher connection');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'XeroInvoiceChange'; }
  /** Register the record, read, and complete Steps. */
  getSteps(): StepList<InvoiceChange> {
    return StepList.startStep(this.record).otherSteps(
      newGetInvoiceStep<InvoiceChange>({
        stepType: readChangedInvoiceStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed invoice back, so the record holds its current state rather than the polled copy.' },
        mapToOperationInput: mapToGetInvoiceInput,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [invoiceChangeAttribute, invoiceChangeOutcomeAttribute] }; }
  /** Declare the invoiceChanged binding and its invoice filter for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineInvoiceChangedTriggerBinding({ connectionName: ConnectionName, bindingName: InvoiceChangedTriggerBinding, configurationUI: { units: [{
      id: 'invoiceFilter', unitId: 'invoiceChangeFilter', label: 'Invoices that start this Flow', required: false,
      description: 'Choose Sales invoices (ACCREC), Bills (ACCPAY), or both, and optionally the statuses an invoice must have after its change, such as PAID to react only to payments in full; Xero\'s own values are saved. Leave it unsaved to start a Flow for every invoice and bill created or changed in the organisation. The first start records existing invoices as a baseline and starts no Flow for them. Restart the Worker after saving; a changed filter records a new baseline.',
      bindings: [{ port: 'type', jsonPointer: '/type' }, { port: 'statuses', jsonPointer: '/statuses' }],
    }] } })];
  }
}

/** Admit every change; the binding's filter already narrows type and status. */
export function acceptEveryInvoiceChange(): boolean { return true; }

/** Name one Flow per change; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<InvoiceChangedEvent>): string { return `xero-invoice-change-${event.id}`; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<InvoiceChangedEvent>): InvoiceChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read the changed invoice by its InvoiceID. */
export function mapToGetInvoiceInput(change: InvoiceChange): GetInvoiceInput { return { invoiceId: change.change.invoice.invoiceId }; }

/** Validate and persist the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered invoice change before reading the invoice back from Xero."
export class RecordInvoiceChange implements Step<InvoiceChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordInvoiceChange'; }
  /** Store the change and read the invoice. */
  execute(context: Context, input: InvoiceChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!input.change?.invoice?.invoiceId?.trim()) return forceFail('change.invoice.invoiceId is required');
    if (!input.change.invoice.status) return forceFail('change.invoice.status is required');
    invoiceChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedInvoice'), input);
  }
}

/** Complete with the invoice's current state. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the invoice read back from Xero as the outcome and complete the Flow."
export class CompleteInvoiceChange implements Step<QueryResult<Invoice>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteInvoiceChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<Invoice>): StepDecision {
    const change = invoiceChangeAttribute.get(context);
    const outcome: InvoiceChangeOutcome = { invoiceId: change.change.invoice.invoiceId, deliveredStatus: change.change.invoice.status, current: result.value };
    invoiceChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
