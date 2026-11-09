// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { GetInvoiceDefinition } from './generated/definitions.js';
import type { GetInvoiceInput, InvoiceSummary } from './generated/models.js';
import type { SageIntacctClient, SageIntacctExchange } from './client.js';

const emptyInvoice = (): InvoiceSummary => ({ key: '', id: '' });
const invoiceKeyPattern = /^[A-Za-z0-9_-]{1,128}$/;

/** Read one accounts receivable invoice by system-assigned key. */
export class GetInvoiceOperation implements Query<GetInvoiceInput, InvoiceSummary> {
  /** Bind the provider client. */ constructor(private readonly client: SageIntacctClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetInvoiceDefinition; }
  /** Project only the stable key and displayed ID into the Flow. */
  async invoke(call: Call, input: GetInvoiceInput): Promise<QueryAttempt<InvoiceSummary>> {
    if (!input || typeof input.invoiceKey !== 'string' || !invoiceKeyPattern.test(input.invoiceKey))
      return branch('defect', 'VALIDATION', 'invoice key is invalid');
    const exchange = await this.client.get(call, `/ia/api/v1/objects/accounts-receivable/invoice/${encodeURIComponent(input.invoiceKey)}`);
    const failed = classifyRead(exchange);
    if (failed) return failed;
    const document = parseDocument(exchange.body);
    const result = isObject(document?.['ia::result']) ? document['ia::result'] : undefined;
    if (!result || result.key !== input.invoiceKey || typeof result.id !== 'string' || !result.id)
      return branch('invalidResponse', 'PROTOCOL', 'Sage Intacct returned invalid invoice JSON');
    return newQueryBranch('found', { key: result.key, id: result.id });
  }
}

function classifyRead(exchange: SageIntacctExchange): QueryAttempt<InvoiceSummary> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Sage Intacct connection needs authorization');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Sage Intacct invoice request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Sage Intacct rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Sage Intacct is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 404) return branch('notFound', 'NOT_FOUND', 'Sage Intacct invoice was not found');
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Sage Intacct invoice response exceeded the configured limit');
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Sage Intacct rejected HTTP ${exchange.status}`);
  return undefined;
}
function parseDocument(body: Uint8Array): Record<string, unknown> | undefined {
  try { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); return isObject(value) ? value : undefined; }
  catch { return undefined; }
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<InvoiceSummary> {
  return newQueryBranch(branchId, emptyInvoice(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<InvoiceSummary> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'sage-intacct', operation: 'getInvoice', message };
}
