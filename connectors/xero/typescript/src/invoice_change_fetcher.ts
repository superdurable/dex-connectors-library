// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { Call, ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { XeroClient, XeroExchange } from './client.js';
import type { Invoice } from './generated/models.js';
import { InvoiceChangeCursorInvalidError, decodeInvoiceChangeCursor, encodeInvoiceChangeCursor, invoiceChangeWindowStart, paddedMilliseconds, pruneSeenInvoiceChanges, type InvoiceChangeCursor } from './invoice_change_cursor.js';
import { decodeInvoiceChangePage } from './operations.js';
import { InvoiceChangedTriggerDefinition, InvoiceStatuses, InvoiceTypes, type InvoiceChangedEvent, type InvoiceChangedTriggerConfiguration } from './trigger_definition.js';

/** Xero's documented paged-invoice size; every paged invoice carries its line items. */
export const InvoiceChangePageSize = 100;
/** The most pages of invoices changed within one overlap window that a binding pages through. */
export const MaximumInvoiceChangeWindowPages = 1_000;

const operationName = 'invoiceChanged';
const ascendingUpdateOrder = 'UpdatedDateUTC ASC';
const descendingUpdateOrder = 'UpdatedDateUTC DESC';
const zeroTimeMs = Date.parse('0001-01-01T00:00:00Z');

/** Reject an unknown type or status and a repeated status, as Go's Validate does; blank filters are valid. */
export function validateInvoiceChangedTriggerConfiguration(configuration: InvoiceChangedTriggerConfiguration): void {
  if (!configuration || typeof configuration !== 'object') throw new TypeError('Xero invoiceChanged configuration is invalid');
  if (configuration.type && !(InvoiceTypes as readonly string[]).includes(configuration.type)) throw new TypeError('type must be ACCREC for a sales invoice or ACCPAY for a bill');
  const statuses = configuration.statuses ?? [];
  if (statuses.some(status => !(InvoiceStatuses as readonly string[]).includes(status))) throw new TypeError('statuses must contain Xero statuses: DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED');
  statuses.forEach((status, index) => { if (statuses.indexOf(status) !== index) throw new TypeError(`statuses lists ${status} more than once`); });
}

/** Return the canonical filter the cursor records, so a changed filter records a new baseline. */
export function invoiceChangeFilterKey(configuration: InvoiceChangedTriggerConfiguration): string {
  const statuses = [...(configuration.statuses ?? [])].sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
  return `type=${configuration.type ?? ''};statuses=${statuses.join(',')}`;
}

/** Hash the invoice ID and change instant, so a refetch yields the same short, URL-safe Trigger event ID. */
export function invoiceChangeEventId(invoiceId: string, updatedAtMs: number): string {
  return `xero-invoice-${createHash('sha256').update(`${invoiceId.toLowerCase()}\x00${updatedAtMs}`).digest('hex').slice(0, 32)}`;
}

/** Name a delivered change sortably: 13-digit milliseconds, then a hash of the invoice ID. */
export function seenInvoiceChangeKey(invoiceId: string, updatedAtMs: number): string {
  return `${paddedMilliseconds(updatedAtMs)}-${createHash('sha256').update(invoiceId.toLowerCase()).digest('hex').slice(0, 12)}`;
}

interface ListedInvoiceChanges { invoices: Array<{ invoice: Invoice; updatedAtMs: number }>; serverTimeMs?: number; tenantId: string; hasMorePages: boolean }

/** Pause the connection for Xero's minute, concurrency, and daily limits, and back off this binding otherwise. */
function invoiceChangePollFailure(result: XeroExchange): Error {
  const failure = new Error(`Xero invoiceChanged poll: ${result.failure?.message ?? 'Xero returned no response'}`);
  if (result.outcome === 'dailyLimit') {
    // The daily limit resets hours later; Retry-After carries the full delay, uncapped.
    const seconds = Number(result.receipt.metadata?.retryAfterSeconds ?? 0);
    return markRateLimited(failure, Number.isSafeInteger(seconds) ? seconds * 1000 : 0);
  }
  if (result.outcome === 'retry' && result.failure?.kind === 'RATE_LIMIT') return markRateLimited(failure, result.retryAfterMs);
  return failure;
}

/**
 * Polls GET /Invoices ordered by UpdatedDateUTC for the poll Trigger kit, at parity with the Go connector. The cursor
 * is the newest delivered UpdatedDateUTC and the changes already delivered inside the five-second re-read window,
 * encoded as Go's cursor document byte for byte. A poller owns one fetcher and calls it one poll at a time.
 */
export class InvoiceChangeFetcher implements ChangeFetcher<TriggerEvent<InvoiceChangedEvent>> {
  private readonly configuration: InvoiceChangedTriggerConfiguration;

  /** Bind the fetcher to one connection's client and one validated binding configuration. */
  constructor(private readonly client: XeroClient, private readonly connection: ConnectionRef, configuration: InvoiceChangedTriggerConfiguration = {}) {
    if (!client || !connection) throw new TypeError('Xero invoiceChanged client and connection are required');
    validateInvoiceChangedTriggerConfiguration(configuration);
    this.configuration = configuration;
  }

  /** Read the most recently changed invoices once and start at Xero's server time, so existing invoices are never delivered. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const listed = await this.listPage(context, undefined, 1, descendingUpdateOrder);
    let since = listed.serverTimeMs ?? this.client.currentTime().getTime();
    for (const { updatedAtMs } of listed.invoices) if (updatedAtMs > since) since = updatedAtMs;
    const position: InvoiceChangeCursor = { filter: invoiceChangeFilterKey(this.configuration), tenantId: listed.tenantId, since, page: 0, seen: [] };
    const windowStart = invoiceChangeWindowStart(since);
    for (const { invoice, updatedAtMs } of listed.invoices) if (updatedAtMs >= windowStart) position.seen.push(seenInvoiceChangeKey(invoice.invoiceId, updatedAtMs));
    position.seen = pruneSeenInvoiceChanges(position.seen, position.since);
    return encodeInvoiceChangeCursor(position);
  }

  /**
   * Read one page of invoices changed since the cursor's overlap window, skipping the changes already delivered. A
   * cursor that cannot be decoded, or one for another filter or organisation, rejects with markCursorInvalid, so the
   * kit records a new baseline; a Xero 429 rejects with markRateLimited.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<InvoiceChangedEvent>>> {
    let position: InvoiceChangeCursor;
    try { position = decodeInvoiceChangeCursor(cursor.token); }
    catch (error) { throw markCursorInvalid(error instanceof InvoiceChangeCursorInvalidError ? error : new InvoiceChangeCursorInvalidError()); }
    if (position.since <= 0 || position.page < 0) throw markCursorInvalid(new InvoiceChangeCursorInvalidError());
    if (position.filter !== invoiceChangeFilterKey(this.configuration)) throw markCursorInvalid(new Error('Xero invoice cursor belongs to another invoice filter'));
    const pageNumber = Math.max(position.page, 1);
    const windowStart = invoiceChangeWindowStart(position.since);
    const listed = await this.listPage(context, windowStart, pageNumber, ascendingUpdateOrder);
    if (listed.tenantId !== position.tenantId) throw markCursorInvalid(new Error('Xero invoice cursor belongs to another organisation'));
    const seen = new Set(position.seen);
    const next: InvoiceChangeCursor = { ...position, seen: [...position.seen] };
    let newestUpdate = position.since;
    const changes: TriggerEvent<InvoiceChangedEvent>[] = [];
    for (const { invoice, updatedAtMs } of listed.invoices) {
      if (updatedAtMs === zeroTimeMs) throw new Error('Xero listed an invoice without UpdatedDateUTC');
      if (updatedAtMs > newestUpdate) newestUpdate = updatedAtMs;
      const key = seenInvoiceChangeKey(invoice.invoiceId, updatedAtMs);
      if (updatedAtMs < windowStart || seen.has(key)) continue;
      seen.add(key);
      next.seen.push(key);
      changes.push({ id: invoiceChangeEventId(invoice.invoiceId, updatedAtMs), occurredAt: new Date(updatedAtMs), payload: { invoice, ...(listed.tenantId ? { tenantId: listed.tenantId } : {}) } });
    }
    let hasMorePages = false;
    if (!listed.hasMorePages) { next.since = newestUpdate; next.page = 0; }
    else if (invoiceChangeWindowStart(newestUpdate) > windowStart) { next.since = newestUpdate; next.page = 0; hasMorePages = true; }
    else if (pageNumber >= MaximumInvoiceChangeWindowPages) throw new Error(`more than ${MaximumInvoiceChangeWindowPages} pages of Xero invoices changed within one 5s window`);
    else {
      // Every invoice on this full page changed inside the window, so only the page number can advance.
      next.page = pageNumber + 1; hasMorePages = true;
    }
    next.seen = pruneSeenInvoiceChanges(next.seen, next.since);
    return { changes, next: encodeInvoiceChangeCursor(next), hasMorePages };
  }

  // Send one GET /Invoices request with the binding's filters, line items, and the given order.
  private async listPage(context: TriggerContext, modifiedSinceMs: number | undefined, page: number, order: string): Promise<ListedInvoiceChanges> {
    await waitForPollRequest(context);
    const query: Record<string, string> = { page: String(page), pageSize: String(InvoiceChangePageSize), unitdp: '4', order };
    if (this.configuration.statuses?.length) query.Statuses = this.configuration.statuses.join(',');
    if (this.configuration.type) query.where = `Type=="${this.configuration.type}"`;
    const call: Call = newTriggerCredentialCall(this.connection, InvoiceChangedTriggerDefinition.trigger, context.signal);
    const result = await this.client.exchange(call, operationName, { method: 'GET', path: '/Invoices', query, ...(modifiedSinceMs !== undefined ? { modifiedSince: new Date(modifiedSinceMs).toISOString() } : {}) });
    if (result.outcome !== 'succeeded') throw invoiceChangePollFailure(result);
    const decoded = decodeInvoiceChangePage(result.response!.body, page, InvoiceChangePageSize, MaxChangesPerPage);
    return {
      invoices: decoded.invoices.map(invoice => ({ invoice, updatedAtMs: Date.parse(invoice.updatedAt) })),
      ...(decoded.serverTimeMs !== undefined ? { serverTimeMs: decoded.serverTimeMs } : {}),
      tenantId: String(result.receipt.metadata?.tenantId ?? ''), hasMorePages: decoded.hasMorePages,
    };
  }
}
