// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetInvoiceDefinition, ListInvoicesDefinition } from './generated/definitions.js';
import type { GetInvoiceInput, InvoiceSummary, ListInvoicesInput, InvoiceList } from './generated/models.js';
import type { SageIntacctClient, SageIntacctExchange } from './client.js';

const invoicesPath = '/ia/api/v1/objects/accounts-receivable/invoice';
const invoiceKeyPattern = /^[A-Za-z0-9_-]{1,128}(?![\s\S])/;

/** Read one accounts receivable invoice by system-assigned key. */
export class GetInvoiceOperation implements Query<GetInvoiceInput, InvoiceSummary> {
  /** Bind the provider client. */ constructor(private readonly client: SageIntacctClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetInvoiceDefinition; }
  /** Project only the stable key and displayed ID into the Flow. */
  async invoke(call: Call, input: GetInvoiceInput): Promise<QueryAttempt<InvoiceSummary>> {
    if (!input || typeof input.invoiceKey !== 'string' || !invoiceKeyPattern.test(input.invoiceKey))
      return branch('getInvoice', { key: '', id: '' }, 'defect', 'VALIDATION', 'invoice key is invalid');
    const exchange = await this.client.get(call, `${invoicesPath}/${encodeURIComponent(input.invoiceKey)}`);
    const failed = classifyRead(exchange, 'getInvoice', { key: '', id: '' });
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const invoice = invoiceSummary(document?.['ia::result']);
    if (!invoice || invoice.key !== input.invoiceKey)
      return branch('getInvoice', { key: '', id: '' }, 'invalidResponse', 'PROTOCOL', 'Sage Intacct returned invalid invoice JSON');
    return newQueryBranch('found', invoice);
  }
}

/** Read the first page of Accounts Receivable invoice keys and IDs. */
export class ListInvoicesOperation implements Query<ListInvoicesInput, InvoiceList> {
  /** Bind the provider client. */
  constructor(private readonly client: SageIntacctClient) {}
  /** Return the manifest-generated invoice list contract. */
  definition(): QueryDefinition { return ListInvoicesDefinition; }
  /** Return the next link as opaque provider data after one GET. */
  async invoke(call: Call, input: ListInvoicesInput): Promise<QueryAttempt<InvoiceList>> {
    void input;
    const empty: InvoiceList = { invoices: [], next: '' };
    const exchange = await this.client.get(call, invoicesPath);
    const failed = classifyRead(exchange, 'listInvoices', empty);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const rows = document?.['ia::result'];
    const meta = document?.['ia::meta'];
    const next = isObject(meta) ? meta.next : undefined;
    if (!Array.isArray(rows) || (meta != null && !isObject(meta)) || (next != null && typeof next !== 'string'))
      return branch('listInvoices', empty, 'invalidResponse', 'PROTOCOL', 'Sage Intacct returned invalid invoice list JSON');
    const invoices: InvoiceSummary[] = [];
    for (const row of rows) {
      const invoice = invoiceSummary(row);
      if (!invoice) return branch('listInvoices', empty, 'invalidResponse', 'PROTOCOL', 'Sage Intacct returned an incomplete invoice identity');
      invoices.push(invoice);
    }
    return newQueryBranch('found', { invoices, next: next ?? '' });
  }
}

function invoiceSummary(value: unknown): InvoiceSummary | undefined {
  if (!isObject(value) || typeof value.key !== 'string' || !invoiceKeyPattern.test(value.key)
      || typeof value.id !== 'string' || !value.id) return undefined;
  return { key: value.key, id: value.id };
}

function classifyRead<T>(exchange: SageIntacctExchange, operation: string, empty: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return branch(operation, empty, 'defect', 'AUTHENTICATION', 'Sage Intacct connection needs authorization');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry(operation, 'TRANSPORT', 'Sage Intacct invoice request failed');
  if (exchange.status === 429) return retry(operation, 'RATE_LIMIT', 'Sage Intacct rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry(operation, 'AVAILABILITY', 'Sage Intacct is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404 && operation === 'getInvoice') return branch(operation, empty, 'notFound', 'NOT_FOUND', 'Sage Intacct invoice was not found');
  if (exchange.status !== 200) return branch(operation, empty, 'providerRejected', 'PROVIDER_REJECTION', `Sage Intacct rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return branch(operation, empty, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Sage Intacct invoice response exceeded the configured limit');
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'), (_key, item: unknown) =>
    isObject(item) ? Object.fromEntries(Object.entries(item).map(([key, field]) => [key.toLowerCase(), field])) : item); return isObject(value) ? value : undefined; }
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
  return { kind, provider: 'sage-intacct', operation, message };
}
