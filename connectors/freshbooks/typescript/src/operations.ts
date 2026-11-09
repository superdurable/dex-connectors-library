// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetInvoiceDefinition } from './generated/definitions.js';
import type { GetInvoiceInput, Invoice } from './generated/models.js';
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
      return branch('defect', 'VALIDATION', 'FreshBooks account ID or invoice ID is invalid');
    const exchange = await this.client.get(call, `/accounting/account/${input.accountId}/invoices/invoices/${input.invoiceId}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const response = isObject(document?.response) ? document.response : undefined;
    const result = isObject(response?.result) ? response.result : undefined;
    const invoice = isObject(result?.invoice) ? result.invoice : undefined;
    const amount = isObject(invoice?.amount) ? invoice.amount : undefined;
    if (!invoice || !amount || invoice.accountid !== input.accountId || invoice.invoiceid !== input.invoiceId ||
      typeof invoice.invoice_number !== 'string' || !invoice.invoice_number ||
      typeof amount.amount !== 'string' || !/^-?[0-9]+(?:\.[0-9]+)?$/.test(amount.amount) ||
      typeof amount.code !== 'string' || !/^[A-Z]{3}$/.test(amount.code) ||
      typeof invoice.payment_status !== 'string' || !invoice.payment_status)
      return branch('invalidResponse', 'PROTOCOL', 'FreshBooks returned invalid invoice JSON');
    return newQueryBranch('found', { accountId: input.accountId, invoiceId: input.invoiceId,
      invoiceNumber: invoice.invoice_number, amount: amount.amount, currencyCode: amount.code, paymentStatus: invoice.payment_status });
  }
}

function classifyRead(exchange: FreshBooksExchange): QueryAttempt<Invoice> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'FreshBooks OAuth credentials are unavailable or invalid');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'FreshBooks request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'FreshBooks rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'FreshBooks is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'FreshBooks response exceeded the configured limit');
  if (exchange.error) return retry('TRANSPORT', 'FreshBooks response was interrupted');
  if (exchange.status === 404) return newQueryBranch('missing', emptyInvoice());
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `FreshBooks rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<Invoice> {
  return newQueryBranch(branchId, emptyInvoice(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Invoice> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'freshbooks', operation: 'getInvoice', message };
}
