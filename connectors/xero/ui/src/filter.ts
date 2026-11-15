// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** InvoiceType is Xero's own invoice type value; "" keeps both. */
export type InvoiceType = "" | "ACCREC" | "ACCPAY";

/** invoiceTypes lists the type choices in display order. */
export const invoiceTypes: {value: InvoiceType; label: string}[] = [
  {value: "", label: "Sales invoices and bills"},
  {value: "ACCREC", label: "Sales invoices (ACCREC)"},
  {value: "ACCPAY", label: "Bills (ACCPAY)"},
];

/** invoiceStatuses lists Xero's invoice statuses in the order Xero documents them. */
export const invoiceStatuses = ["DRAFT", "SUBMITTED", "AUTHORISED", "PAID", "VOIDED", "DELETED"] as const;

/** InvoiceStatus is one of Xero's own invoice status values. */
export type InvoiceStatus = (typeof invoiceStatuses)[number];

/** InvoiceChangeFilter is the value the invoiceChangeFilter unit saves, keyed by its output ports. */
export interface InvoiceChangeFilter {
  type: InvoiceType;
  statuses: InvoiceStatus[];
}

/**
 * readInvoiceChangeFilter reads a saved unit value, dropping anything that is not one of Xero's values,
 * so a hand-edited configuration never reaches the form as an unknown choice.
 */
export function readInvoiceChangeFilter(value: Record<string, unknown>): InvoiceChangeFilter {
  const type = invoiceTypes.some((choice) => choice.value === value.type) ? value.type as InvoiceType : "";
  const saved = Array.isArray(value.statuses) ? value.statuses : [];
  return {type, statuses: invoiceStatuses.filter((status) => saved.includes(status))};
}

/** savedInvoiceChangeFilter is the unit value to save: blank choices are omitted, statuses in Xero's order. */
export function savedInvoiceChangeFilter(filter: InvoiceChangeFilter): Record<string, unknown> {
  const value: Record<string, unknown> = {};
  if (filter.type !== "") value.type = filter.type;
  const statuses = invoiceStatuses.filter((status) => filter.statuses.includes(status));
  if (statuses.length > 0) value.statuses = statuses;
  return value;
}
