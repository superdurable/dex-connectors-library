// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { InvoiceSchema } from './generated/models.js';

/** An invoice at its first version: QuickBooks reports SyncToken 0. */
export const InvoiceChangeCreated = 'created';
/**
 * Any later version, including an edit, an applied payment that lowers Balance, an email that sets EmailStatus,
 * and a void that zeroes the amounts. An invoice created and edited between two polls arrives once, as updated.
 */
export const InvoiceChangeUpdated = 'updated';
/** The derived kind of one invoiceChanged delivery. */
export type InvoiceChangeKind = typeof InvoiceChangeCreated | typeof InvoiceChangeUpdated;

/**
 * The invoiceChanged binding configuration. A binding watches every invoice of the connection's company, so it has
 * no fields; an application narrows the changes it starts Flows for in its Trigger target's accept function.
 */
export const InvoiceChangedTriggerConfigurationSchema = z.strictObject({});
/** The validated invoiceChanged binding configuration. */
export type InvoiceChangedTriggerConfiguration = z.infer<typeof InvoiceChangedTriggerConfigurationSchema>;

/**
 * One invoice version read by the invoiceChanged poll; field names match the Go connector. QuickBooks's invoice
 * query does not return deleted invoices, so a deletion is never delivered.
 */
export const InvoiceChangedEventSchema = z.object({
  /** The QuickBooks company the invoice belongs to. */ realmId: z.string(),
  /** The connection's QuickBooks environment, production or sandbox. */ environment: z.string(),
  /** created or updated; see InvoiceChangeKind. */ changeKind: z.enum([InvoiceChangeCreated, InvoiceChangeUpdated]),
  /** The invoice as QuickBooks returned it in the poll, with its syncToken and updatedAt. */ invoice: InvoiceSchema,
});
/** One created or changed QuickBooks invoice version. */
export type InvoiceChangedEvent = z.infer<typeof InvoiceChangedEventSchema>;

/** Manifest definition of invoiceChanged. */
export const InvoiceChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'invoiceChanged' },
  description: "Receive each invoice created or changed in the connected QuickBooks company, including payments, emails, and voids that update it, read by polling invoices by MetaData.LastUpdatedTime; the first run records a baseline and delivers no existing invoices, and deleted invoices are not reported.",
};

/** One static application binding of invoiceChanged. */
export interface InvoiceChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Connector Studio configuration units; invoiceChanged has none, so omit it. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static invoiceChanged binding for Flow Definition tooling and Dex Web. */
export function defineInvoiceChangedTriggerBinding(config: InvoiceChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: InvoiceChangedTriggerDefinition, ...config });
}
