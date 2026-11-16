// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { Call, ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { QuickBooksClient } from './client.js';
import type { Invoice } from './generated/models.js';
import { InvoiceChangeCursorInvalidError, InvoiceChangeLookbackMs, InvoiceWindow, MaximumSeenInvoiceVersions, decodeInvoiceChangeCursor, encodeInvoiceChangeCursor, formatRFC3339Seconds, parseRFC3339Nanoseconds, second } from './invoice_change_cursor.js';
import { decodeInvoiceQueryPage } from './operations.js';
import { InvoiceChangeCreated, InvoiceChangeUpdated, InvoiceChangedTriggerDefinition, type InvoiceChangedEvent } from './trigger_definition.js';

/** The MAXRESULTS of each invoiceChanged query; QuickBooks itself accepts up to 1000. */
export const InvoiceChangePageSize = 100;

const lookback = BigInt(InvoiceChangeLookbackMs) * 1_000_000n;
const maximumSeedPages = MaximumSeenInvoiceVersions / InvoiceChangePageSize;
const operationName = 'invoiceChanged';
const newInvoiceSyncToken = '0';

interface PolledInvoice { invoice: Invoice; updatedAt: bigint }

/** Name one invoice version as "<Id>:<SyncToken>", as the cursor stores it. */
export function invoiceVersionKey(invoice: Invoice): string { return `${invoice.invoiceId}:${invoice.syncToken ?? ''}`; }

/**
 * Return the Trigger event ID, a hash of the environment, company, Id, SyncToken, and LastUpdatedTime second. It is
 * a pure function of the invoice version, so a refetch after a crash yields the same ID.
 */
export function invoiceChangeEventId(realmId: string, environment: string, invoice: Invoice): string {
  const updated = invoice.updatedAt ? formatRFC3339Seconds(parseRFC3339Nanoseconds(invoice.updatedAt)) : '';
  const digest = createHash('sha256').update([environment, realmId, invoice.invoiceId, invoice.syncToken ?? '', updated].join('\x00')).digest('hex');
  return `qbo-invoice-${digest.slice(0, 32)}`;
}

function newInvoiceChangedTriggerEvent(realmId: string, environment: string, invoice: Invoice): TriggerEvent<InvoiceChangedEvent> {
  const changeKind = invoice.syncToken === newInvoiceSyncToken ? InvoiceChangeCreated : InvoiceChangeUpdated;
  return { id: invoiceChangeEventId(realmId, environment, invoice), occurredAt: new Date(invoice.updatedAt!), payload: { realmId, environment, changeKind, invoice } };
}

const quoteQueryLiteral = (value: string): string => `'${value.replaceAll("'", "\\'")}'`;

/**
 * Polls one company's invoices ordered by MetaData.LastUpdatedTime for the poll Trigger kit, at parity with the Go
 * connector: the cursor is a LastUpdatedTime window rereading 30 seconds before the newest change and the
 * "<Id>:<SyncToken>" versions delivered inside it, encoded as Go's cursor document byte for byte. A poller owns one
 * fetcher and calls it one poll at a time.
 */
export class InvoiceChangeFetcher implements ChangeFetcher<TriggerEvent<InvoiceChangedEvent>> {
  /** Bind the fetcher to one connection's client. */
  constructor(private readonly client: QuickBooksClient, private readonly connection: ConnectionRef) {
    if (!client || !connection) throw new TypeError('QuickBooks invoiceChanged client and connection are required');
  }

  /**
   * Record every version in the 30 seconds before the newest LastUpdatedTime as the baseline, so existing invoices
   * are never delivered; an empty company delivers its first invoice.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    let realmId = '';
    const baseline: PolledInvoice[] = [];
    let newest: bigint | undefined;
    let isWindowComplete = false;
    for (let page = 0; page < maximumSeedPages && !isWindowComplete; page++) {
      const statement = `select * from Invoice ORDERBY MetaData.LastUpdatedTime DESC STARTPOSITION ${1 + page * InvoiceChangePageSize} MAXRESULTS ${InvoiceChangePageSize}`;
      const queried = await this.queryInvoices(context, statement, false);
      realmId = queried.realmId;
      isWindowComplete = queried.invoices.length < InvoiceChangePageSize;
      for (const polled of queried.invoices) {
        newest ??= polled.updatedAt;
        if (polled.updatedAt < newest - lookback) { isWindowComplete = true; break; }
        baseline.push(polled);
      }
    }
    const window = new InvoiceWindow();
    if (baseline.length && newest !== undefined) {
      window.since = newest;
      window.from = newest - lookback;
      // The scan stopped inside the window: its oldest second may be partial, so start after it.
      if (!isWindowComplete) window.from = baseline[baseline.length - 1]!.updatedAt + second;
      for (const polled of baseline) if (polled.updatedAt >= window.from) window.seen.set(invoiceVersionKey(polled.invoice), polled.updatedAt);
      if (window.from > window.since) {
        window.from = window.since + second;
        window.since = window.from;
      }
      window.pruneToBound();
    }
    return encodeInvoiceChangeCursor(realmId, this.client.environment, window);
  }

  /**
   * Read one page of invoices changed at or after the window's lower bound, oldest first, skipping versions already
   * delivered or baselined. An undecodable cursor, one for another company or environment, or a QuickBooks
   * validation Fault for the watermark query rejects with markCursorInvalid, so the kit records a new baseline; a
   * 429 rejects with markRateLimited.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<InvoiceChangedEvent>>> {
    let decoded: ReturnType<typeof decodeInvoiceChangeCursor>;
    try { decoded = decodeInvoiceChangeCursor(cursor.token); }
    catch (error) { throw markCursorInvalid(error instanceof InvoiceChangeCursorInvalidError ? error : new InvoiceChangeCursorInvalidError()); }
    const { position, window } = decoded;
    const environment = this.client.environment;
    if (position.environment !== environment) throw markCursorInvalid(new Error('QuickBooks invoice cursor belongs to another environment'));
    let statement = 'select * from Invoice';
    if (position.from) statement += ` where MetaData.LastUpdatedTime >= ${quoteQueryLiteral(position.from)}`;
    statement += ` ORDERBY MetaData.LastUpdatedTime STARTPOSITION ${1 + window.offset} MAXRESULTS ${InvoiceChangePageSize}`;
    const { realmId, invoices } = await this.queryInvoices(context, statement, position.from !== '');
    if (realmId !== position.realmId) throw markCursorInvalid(new Error('QuickBooks invoice cursor belongs to another company'));
    const changes: TriggerEvent<InvoiceChangedEvent>[] = [];
    let previous = window.from;
    for (const polled of invoices) {
      if (polled.updatedAt < previous) throw new Error('QuickBooks invoice page is not in LastUpdatedTime order');
      previous = polled.updatedAt;
      if (window.seen.has(invoiceVersionKey(polled.invoice))) continue;
      changes.push(newInvoiceChangedTriggerEvent(realmId, environment, polled.invoice));
    }
    const isFullPage = invoices.length === InvoiceChangePageSize;
    const following = window.advance(invoices.map(polled => [invoiceVersionKey(polled.invoice), polled.updatedAt] as const), isFullPage);
    return { changes, next: encodeInvoiceChangeCursor(position.realmId, position.environment, following), hasMorePages: isFullPage };
  }

  private async queryInvoices(context: TriggerContext, statement: string, hasWatermark: boolean): Promise<{ realmId: string; invoices: PolledInvoice[] }> {
    await waitForPollRequest(context);
    const call: Call = newTriggerCredentialCall(this.connection, InvoiceChangedTriggerDefinition.trigger, context.signal);
    const result = await this.client.exchange(call, operationName, { method: 'GET', path: '/query', query: { query: statement } });
    if (result.disposition !== 'succeeded') {
      const failure = new Error(`QuickBooks invoice poll: ${result.failure?.message ?? 'QuickBooks returned no response'}`);
      if (result.failure?.kind === 'RATE_LIMIT') throw markRateLimited(failure, result.retryAfterMs);
      if (hasWatermark && result.status === 400 && result.failure?.kind === 'VALIDATION') throw markCursorInvalid(failure);
      throw failure;
    }
    const realmId = String(result.receipt.metadata?.realmId ?? '');
    let decoded: Invoice[];
    try { decoded = decodeInvoiceQueryPage(result.body ?? new Uint8Array()); }
    catch (error) { throw new Error(`QuickBooks invoice poll returned an invalid page: ${error instanceof Error ? error.message : 'invalid invoice'}`); }
    if (decoded.length > InvoiceChangePageSize) throw new Error('QuickBooks invoice poll returned more invoices than requested');
    const invoices = decoded.map(invoice => {
      if (!invoice.updatedAt) throw new Error('QuickBooks invoice poll returned an invoice without MetaData.LastUpdatedTime');
      const updatedAt = parseRFC3339Nanoseconds(invoice.updatedAt);
      const remainder = ((updatedAt % second) + second) % second;
      return { invoice, updatedAt: updatedAt - remainder };
    });
    return { realmId, invoices };
  }
}
