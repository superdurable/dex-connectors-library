// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  InvoiceChangeCreated, InvoiceChangeUpdated, defineInvoiceChangedTriggerBinding, newGetInvoiceStep,
  type Connection, type GetInvoiceInput, type Invoice, type InvoiceChangeKind, type InvoiceChangedEvent,
} from '@superdurable/connector-intuit-quickbooks';

/** Static Dex Web connection for QuickBooks. */
export const ConnectionName = 'quickbooks-invoice-watcher';
/** The stable invoiceChanged binding that starts this Flow. */
export const InvoiceChangedTriggerBinding = 'quickbooks-invoice-changes';
const readChangedInvoiceStepType = 'ReadChangedInvoice';

/** One invoiceChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface InvoiceChange { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered change. */ change: InvoiceChangedEvent }

/** The Flow result: the changed invoice read back from QuickBooks. */
export interface InvoiceChangeOutcome {
  /** The company the invoice belongs to. */ realmId: string;
  /** The changed invoice. */ invoiceId: string;
  /** The delivered change kind. */ changeKind: InvoiceChangeKind;
  /** Whether the invoice read back has no balance left. */ isPaid: boolean;
  /** The invoice as QuickBooks returns it now. */ current: Invoice;
}

/** The delivered change. */ export const invoiceChangeAttribute = new Attribute<InvoiceChange>('quickbooks-invoice-change', jsonCodec<InvoiceChange>());
/** The recorded outcome. */ export const invoiceChangeOutcomeAttribute = new Attribute<InvoiceChangeOutcome>('quickbooks-invoice-change-outcome', jsonCodec<InvoiceChangeOutcome>());

/** Records one QuickBooks invoice change. */
export class InvoiceChangedFlow implements Flow<InvoiceChange> {
  private readonly record = new RecordInvoiceChange();
  private readonly complete = new CompleteInvoiceChange();
  /** Bind the QuickBooks connection at registration time. */ constructor(private readonly connection: Connection) {
    if (connection?.reference.name !== ConnectionName) throw new TypeError('invoice change Flow requires the quickbooks-invoice-watcher connection');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'QuickBooksInvoiceChange'; }
  /** Register the record, read, and complete Steps. */
  getSteps(): StepList<InvoiceChange> {
    return StepList.startStep(this.record).otherSteps(
      newGetInvoiceStep<InvoiceChange>({
        stepType: readChangedInvoiceStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed invoice back, so the record holds its current balance rather than the polled copy.' },
        mapToOperationInput: mapToGetInvoiceInput,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [invoiceChangeAttribute, invoiceChangeOutcomeAttribute] }; }
  /** Declare the invoiceChanged binding; it watches the whole company and has no fields, so it has no picker units. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineInvoiceChangedTriggerBinding({ connectionName: ConnectionName, bindingName: InvoiceChangedTriggerBinding })];
  }
}

/** Admit every change; an application narrows it, for example to one customer. */
export function acceptEveryInvoiceChange(): boolean { return true; }

/** Name one Flow per invoice version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<InvoiceChangedEvent>): string { return `quickbooks-invoice-change-${event.id}`; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<InvoiceChangedEvent>): InvoiceChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read the changed invoice. */
export function mapToGetInvoiceInput(change: InvoiceChange): GetInvoiceInput { return { invoiceId: change.change.invoice.invoiceId }; }

/** Validate and persist the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered invoice change before reading the invoice back."
export class RecordInvoiceChange implements Step<InvoiceChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordInvoiceChange'; }
  /** Store the change and read the invoice. */
  execute(context: Context, input: InvoiceChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!input.change?.realmId || !input.change.invoice?.invoiceId) return forceFail('change.realmId and change.invoice.invoiceId are required');
    if (input.change.changeKind !== InvoiceChangeCreated && input.change.changeKind !== InvoiceChangeUpdated) return forceFail('change.changeKind must be created or updated');
    invoiceChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedInvoice'), input);
  }
}

/** Complete with whether the invoice is paid. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record whether the invoice read back from QuickBooks is paid and complete the Flow."
export class CompleteInvoiceChange implements Step<QueryResult<Invoice>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteInvoiceChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<Invoice>): StepDecision {
    const change = invoiceChangeAttribute.get(context);
    const outcome: InvoiceChangeOutcome = {
      realmId: change.change.realmId, invoiceId: change.change.invoice.invoiceId, changeKind: change.change.changeKind,
      isPaid: /^-?0*(\.0*)?$/.test(result.value.balance || '0'), current: result.value,
    };
    invoiceChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
