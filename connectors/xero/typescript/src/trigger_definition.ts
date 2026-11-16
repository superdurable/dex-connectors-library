// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mustTriggerBindingDefinition, type ConnectorConfigurationUI, type TriggerBindingDefinition, type TriggerDefinition } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';
import { InvoiceSchema } from './generated/models.js';

/** Every Xero invoice status an invoiceChanged binding may filter on. */
export const InvoiceStatuses = ['DRAFT', 'SUBMITTED', 'DELETED', 'AUTHORISED', 'PAID', 'VOIDED'] as const;
/** ACCREC sales invoices and ACCPAY bills. */
export const InvoiceTypes = ['ACCREC', 'ACCPAY'] as const;

/**
 * The invoiceChanged binding configuration that the invoiceChangeFilter unit saves in Dex Web. Both filters are
 * optional and combine; unknown fields are rejected and a null field keeps its zero value, as Go's strict decoding
 * does. Changing the filter of a binding that already polled makes its next poll record a new baseline.
 */
export const InvoiceChangedTriggerConfigurationSchema = z.strictObject({
  /** Keeps only ACCREC sales invoices or ACCPAY bills; blank keeps both. */ type: z.string().nullish(),
  /** Keeps invoices whose status after the change is one of these; empty keeps every status, including DELETED and VOIDED. */
  statuses: z.array(z.string()).nullish(),
}).transform(({ type, statuses }) => ({ ...(type ? { type } : {}), ...(statuses?.length ? { statuses } : {}) }));
/** The validated invoiceChanged binding configuration. */
export type InvoiceChangedTriggerConfiguration = { type?: string; statuses?: string[] };

/**
 * One sales invoice or bill as Xero listed it after a change; field names match the Go connector. Xero reports no
 * change type in a list, so a creation and an update look alike: compare invoice.status with the application's own
 * record when the difference matters.
 */
export const InvoiceChangedEventSchema = z.object({
  /** The invoice with its line items, exact decimal totals, and updatedAt, the UpdatedDateUTC of this change. */ invoice: InvoiceSchema,
  /** The Xero-Tenant-Id an OAuth connection read the invoice with; omitted for a Custom Connection. */ tenantId: z.string().optional(),
});
/** One created or changed Xero invoice or bill. */
export type InvoiceChangedEvent = z.infer<typeof InvoiceChangedEventSchema>;

/** Manifest definition of invoiceChanged. */
export const InvoiceChangedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'invoiceChanged' },
  description: 'Receive each Xero sales invoice or bill created or changed in the organisation, with its line items and exact decimal totals, read by polling GET /Invoices with If-Modified-Since; the first run records a baseline and delivers no existing invoices.',
};

/** The Dex Web unit that saves an invoiceChanged binding's type and statuses filter. */
export const UIUnitInvoiceChangeFilter = 'invoiceChangeFilter';
/** The invoiceChangeFilter port holding ACCREC, ACCPAY, or blank; bind it to /type. */
export const UIInvoiceChangeFilterPortType = 'type';
/** The invoiceChangeFilter port holding the selected statuses; bind it to /statuses. */
export const UIInvoiceChangeFilterPortStatuses = 'statuses';

/** One static application binding of invoiceChanged. */
export interface InvoiceChangedTriggerBindingConfig {
  /** Static connection name. */ connectionName: string;
  /** Static binding name. */ bindingName: string;
  /** Dex Web configuration units, usually one invoiceChangeFilter unit bound to /type and /statuses. */ configurationUI?: ConnectorConfigurationUI;
}

/** Declare a static invoiceChanged binding for Flow Definition tooling and Dex Web. */
export function defineInvoiceChangedTriggerBinding(config: InvoiceChangedTriggerBindingConfig): TriggerBindingDefinition {
  return mustTriggerBindingDefinition({ definition: InvoiceChangedTriggerDefinition, ...config });
}
