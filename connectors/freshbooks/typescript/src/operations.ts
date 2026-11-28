// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetInvoiceDefinition, ListInvoicesDefinition } from './generated/definitions.js';
import type { GetInvoiceInput, Invoice, InvoicePage, ListInvoicesInput } from './generated/models.js';
import type { FreshBooksClient, FreshBooksExchange } from './client.js';

const emptyInvoice = (): Invoice => ({ accountId: '', invoiceId: 0, invoiceNumber: '', amount: '', currencyCode: '', paymentStatus: '' });
const accountIDPattern = /^[A-Za-z0-9_-]{1,64}$/;

/** Read one invoice from the authorized FreshBooks account. */
export class GetInvoiceOperation implements Query<GetInvoiceInput, Invoice> {
  /** Bind the provider client. */ constructor(private readonly client: FreshBooksClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetInvoiceDefinition; }
  /** Read one bounded response and project selected financial fields. */
  async invoke(call: Call, input: GetInvoiceInput): Promise<QueryAttempt<Invoice>> {
    if (!input || !accountIDPattern.test(input.accountId) || !Number.isSafeInteger(input.invoiceId) || input.invoiceId <= 0)
      return branch('getInvoice', emptyInvoice(), 'defect', 'VALIDATION', 'FreshBooks account ID or invoice ID is invalid');
    const exchange = await this.client.get(call, `/accounting/account/${input.accountId}/invoices/invoices/${input.invoiceId}`);
    const failed = classifyRead(exchange, 'getInvoice', emptyInvoice());
    if (failed) return failed;
    const invoice = decodeInvoice(invoiceResult(exchange.body)?.invoice, input);
    if (!invoice) return branch('getInvoice', emptyInvoice(), 'invalidResponse', 'PROTOCOL', 'FreshBooks returned invalid invoice JSON');
    return newQueryBranch('found', invoice);
  }
}

/** Read one invoice page using the existing accounting account OAuth grant. */
export class ListInvoicesOperation implements Query<ListInvoicesInput, InvoicePage> {
  /** Bind the provider client. */
  constructor(private readonly client: FreshBooksClient) {}

  /** Return the generated listInvoices contract. */
  definition(): QueryDefinition { return ListInvoicesDefinition; }

  /** Read up to 100 invoices with pagination metadata in provider order. */
  async invoke(call: Call, input: ListInvoicesInput): Promise<QueryAttempt<InvoicePage>> {
    const page = input?.page === 0 || input?.page === undefined ? 1 : input.page;
    const empty: InvoicePage = { accountId: '', invoices: [], page: 0, pageCount: 0, total: 0 };
    if (!input || typeof input.accountId !== 'string' || !accountIDPattern.test(input.accountId) ||
        !Number.isSafeInteger(page) || page < 1 || page > 10000) {
      return branch('listInvoices', empty, 'defect', 'VALIDATION', 'FreshBooks account ID or page is invalid');
    }
    const path = `/accounting/account/${input.accountId}/invoices/invoices?page=${page}&per_page=100`;
    const exchange = await this.client.get(call, path);
    const failure = classifyRead(exchange, 'listInvoices', empty);
    if (failure) return failure;
    const output = decodePage(invoiceResult(exchange.body), input.accountId, page);
    if (!output) return branch('listInvoices', empty, 'invalidResponse', 'PROTOCOL', 'FreshBooks returned invalid invoice page JSON');
    return newQueryBranch('listed', output);
  }
}

function invoiceResult(body: Uint8Array): Record<string, unknown> | undefined {
  const document = parseDocument(body);
  const response = isObject(document?.response) ? document.response : undefined;
  return isObject(response?.result) ? response.result : undefined;
}

function decodePage(result: Record<string, unknown> | undefined, accountId: string, page: number): InvoicePage | undefined {
  if (!result || !Array.isArray(result.invoices) || result.page !== page) return undefined;
  const { pages = 0, total = 0 } = result;
  if (typeof pages !== 'number' || !Number.isSafeInteger(pages) || pages < 0 ||
      typeof total !== 'number' || !Number.isSafeInteger(total) || total < 0) return undefined;
  const invoices: Invoice[] = [];
  for (const row of result.invoices) {
    if (!isObject(row) || typeof row.invoiceid !== 'number' || !Number.isSafeInteger(row.invoiceid) || row.invoiceid <= 0) return undefined;
    const invoice = decodeInvoice(row, { accountId, invoiceId: row.invoiceid });
    if (!invoice) return undefined;
    invoices.push(invoice);
  }
  return { accountId, invoices, page, pageCount: pages, total };
}

function decodeInvoice(raw: unknown, input: GetInvoiceInput): Invoice | undefined {
  if (!isObject(raw)) return undefined;
  const invoice = raw;
    const amount = isObject(invoice?.amount) ? invoice.amount : undefined;
    if (!invoice || !amount || invoice.accountid !== input.accountId || invoice.invoiceid !== input.invoiceId ||
      typeof invoice.invoice_number !== 'string' || !invoice.invoice_number ||
      typeof amount.amount !== 'string' || !/^-?[0-9]+(?:\.[0-9]+)?$/.test(amount.amount) ||
      typeof amount.code !== 'string' || !/^[A-Z]{3}$/.test(amount.code) ||
      typeof invoice.payment_status !== 'string' || !invoice.payment_status)
      return undefined;
    return { accountId: input.accountId, invoiceId: input.invoiceId,
      invoiceNumber: invoice.invoice_number, amount: amount.amount, currencyCode: amount.code, paymentStatus: invoice.payment_status };
}

function classifyRead<T>(exchange: FreshBooksExchange, operation: string, empty: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return branch(operation, empty, 'defect', 'AUTHENTICATION', 'FreshBooks OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry(operation, 'TRANSPORT', 'FreshBooks request failed');
  if (exchange.status === 429) return retry(operation, 'RATE_LIMIT', 'FreshBooks rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry(operation, 'AVAILABILITY', 'FreshBooks is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch(operation, empty, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'FreshBooks response exceeded the configured limit');
  if (exchange.error) return retry(operation, 'TRANSPORT', 'FreshBooks response was interrupted');
  if (exchange.status === 404) {
    if (operation === 'listInvoices') return branch(operation, empty, 'missing', 'NOT_FOUND', 'FreshBooks did not find the account');
    return newQueryBranch('missing', empty);
  }
  if (exchange.status !== 200) return branch(operation, empty, 'providerRejected', 'PROVIDER_REJECTION', `FreshBooks rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch<T>(operation: string, empty: T, branchId: string, kind: Failure['kind'], message: string): QueryAttempt<T> {
  return newQueryBranch(branchId, empty, {}, failure(operation, kind, message));
}
function retry<T>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<T> {
  return newQueryRetry(failure(operation, kind, message), delay);
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'freshbooks', operation, message };
}
