// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ZendeskClient, ZendeskExchange } from './client.js';
import type { Ticket } from './generated/models.js';
import { decodeTicketWire } from './operations.js';
import { TicketExportCursorInvalidError, decodeTicketExportCursor, encodeTicketExportCursor, type TicketExportCursor } from './ticket_export_cursor.js';
import {
  TicketChangeCreated, TicketChangeDeleted, TicketChangeUpdated, TicketChangedTriggerDefinition, TicketStatusDeleted,
  type TicketChangeKind, type TicketChangedEvent, type TicketChangedTriggerConfiguration,
} from './trigger_definition.js';

const ticketExportPath = '/incremental/tickets/cursor';
const operationName = 'ticketChanged';
// Keeps a page of tickets with descriptions well under the default response limit.
const ticketExportPageSize = 100;
const minimumTicketExportPageSize = 10;
const maximumBaselinePages = 100;
// Zendesk serves no data for the latest minute and rejects a start_time inside it.
const ticketExportStartTimeLagSeconds = 61n;
// Tolerates a Worker clock up to four minutes ahead of Zendesk's.
const ticketExportSeedLagSeconds = 300n;
const createdUpdatedToleranceNanoseconds = 1_000_000_000n;
const goZeroTime = '0001-01-01T00:00:00Z';

/** An HTTP 400 or 422 answer to a cursor request. */
class TicketExportCursorRejectedError extends Error {}

interface ExportTicket { ticket: Ticket; generatedTimestamp: number }
interface TicketExportPage { tickets: ExportTicket[]; afterCursor: string | null; isEndOfStream: boolean }
// An export request starts at cursor, or at startTime when cursor is empty.
interface TicketExportPosition { cursor: string; startTime: bigint }

/** Derive the change kind: deleted from the status, created when updatedAt is within 1s of createdAt. */
export function ticketChangeKind(ticket: Ticket): TicketChangeKind {
  if (ticket.status === TicketStatusDeleted) return TicketChangeDeleted;
  if (ticket.createdAt !== goZeroTime) {
    const difference = goTimeNanoseconds(ticket.updatedAt) - goTimeNanoseconds(ticket.createdAt);
    if ((difference < 0n ? -difference : difference) <= createdUpdatedToleranceNanoseconds) return TicketChangeCreated;
  }
  return TicketChangeUpdated;
}

/**
 * Return the Trigger event ID, a hash of the ticket ID, generated_timestamp, updated_at, and status. It is a pure
 * function of the ticket version, so a refetch after a crash or a cursor restart yields the same ID, and it equals
 * the Go connector's.
 */
export function ticketChangeEventId(ticket: Ticket, generatedTimestamp: number): string {
  const identity = [String(ticket.id), String(generatedTimestamp), `${ticket.updatedAt.slice(0, 19)}Z`, ticket.status].join('\u0000');
  return `zendesk-ticket-${createHash('sha256').update(identity, 'utf8').digest('hex').slice(0, 32)}`;
}

/** Convert one export ticket; occurredAt is the later of updated_at and generated_timestamp. */
export function newTicketChangedTriggerEvent(ticket: Ticket, generatedTimestamp: number): TriggerEvent<TicketChangedEvent> {
  const payload: TicketChangedEvent = { changeKind: ticketChangeKind(ticket), ticket };
  let occurredAt = new Date(Date.parse(ticket.updatedAt));
  if (generatedTimestamp > 0) {
    const generatedAt = new Date(generatedTimestamp * 1000);
    payload.generatedAt = `${generatedAt.toISOString().slice(0, 19)}Z`;
    if (generatedAt.getTime() > occurredAt.getTime()) occurredAt = generatedAt;
  }
  return { id: ticketChangeEventId(ticket, generatedTimestamp), occurredAt, payload };
}

// A Go UTC RFC 3339 time as nanoseconds since the Unix epoch.
function goTimeNanoseconds(value: string): bigint {
  const seconds = BigInt(Math.floor(Date.parse(`${value.slice(0, 19)}Z`) / 1000));
  const fraction = /\.(\d+)Z$/.exec(value)?.[1] ?? '';
  return seconds * 1_000_000_000n + BigInt(fraction.padEnd(9, '0').slice(0, 9) || '0');
}

function unixSeconds(moment: Date): bigint { return BigInt(Math.floor(moment.getTime() / 1000)); }
const minimumBigInt = (left: bigint, right: bigint): bigint => (left < right ? left : right);

/**
 * Keep the page's after_cursor and move resumeAt to the newest generated_timestamp. Later pages may hold more
 * changes in that second; at the end of the stream every change of that second was served, so resumeAt moves
 * one second past it.
 */
function advanceTicketExportCursor(position: TicketExportCursor, listed: TicketExportPage): TicketExportCursor {
  let resumeAt = position.resumeAt;
  for (const exported of listed.tickets) {
    const candidate = BigInt(exported.generatedTimestamp) + (listed.isEndOfStream ? 1n : 0n);
    if (candidate > resumeAt) resumeAt = candidate;
  }
  return { ...position, afterCursor: listed.afterCursor || position.afterCursor, resumeAt };
}

/** Decode one export page as the Go connector's strict struct decoding accepts it. */
function decodeTicketExportPage(body: Uint8Array, agentTicketURL: string): TicketExportPage {
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')); } catch { throw new Error('Zendesk incremental export page is not valid JSON'); }
  if (document === null) document = {};
  if (typeof document !== 'object' || Array.isArray(document)) throw new Error('Zendesk incremental export page is not valid JSON');
  const { tickets: rows = null, after_cursor: afterCursor = null, end_of_stream: isEndOfStream = null } = document as Record<string, unknown>;
  if ((rows !== null && !Array.isArray(rows)) || (afterCursor !== null && typeof afterCursor !== 'string') || (isEndOfStream !== null && typeof isEndOfStream !== 'boolean')) {
    throw new Error('Zendesk incremental export page is not valid JSON');
  }
  const ticketRows = (rows ?? []) as unknown[];
  if (ticketRows.length > MaxChangesPerPage) throw new Error(`Zendesk incremental export page has ${ticketRows.length} tickets, more than ${MaxChangesPerPage}`);
  const tickets = ticketRows.map(row => {
    try {
      const ticket = decodeTicketWire(row, agentTicketURL);
      const generated = (row as Record<string, unknown>).generated_timestamp;
      if (generated !== undefined && generated !== null && (typeof generated !== 'number' || !Number.isSafeInteger(generated))) throw new Error('generated_timestamp is not an integer');
      return { ticket, generatedTimestamp: (generated as number | null | undefined) ?? 0 };
    } catch (error) {
      throw new Error(`Zendesk incremental export page has an invalid ticket: ${error instanceof Error ? error.message : String(error)}`);
    }
  });
  return { tickets, afterCursor: afterCursor as string | null, isEndOfStream: isEndOfStream === true };
}

/** Map a failed export request to the kit's signals; messages never carry the token. */
function classifyTicketExportFailure(result: Exclude<ZendeskExchange, { outcome: 'success' }>, position: TicketExportPosition): Error {
  const failure = new Error(`Zendesk incremental export: ${result.failure.message}`);
  const status = result.response.status;
  // The limit is per account, so every binding of the connection pauses.
  if (status === 429) return markRateLimited(failure, result.retryAfterMs ?? 0);
  if (position.cursor && (status === 400 || status === 422)) return new TicketExportCursorRejectedError(`Zendesk rejected the incremental export cursor: ${failure.message}`);
  if (status === 403) return new Error(`${failure.message}; Zendesk allows the incremental ticket export only for administrators, so connect an administrator's email and API token`);
  return failure;
}

/**
 * Reads the account's cursor-based incremental ticket export for the poll Trigger kit, at parity with the Go
 * connector. A poller owns one fetcher and calls it one poll at a time; its page size starts at 100 and halves,
 * down to 10, after a response larger than maxResponseBytes, for the life of the fetcher.
 */
export class TicketExportFetcher implements ChangeFetcher<TriggerEvent<TicketChangedEvent>> {
  private readonly isIncludingDeleted: boolean;
  private pageSize = ticketExportPageSize;

  /** Bind the fetcher to one connection's client and one binding's validated configuration. */
  constructor(private readonly client: ZendeskClient, private readonly connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration) {
    if (!client || !connection || !configuration) throw new TypeError('Zendesk ticketChanged client, connection, and configuration are required');
    this.isIncludingDeleted = configuration.includeDeleted === true;
  }

  /**
   * Start the export five minutes ago and drain it to the end of the stream, keeping only the final cursor, so
   * tickets changed before the first poll are never delivered. Rejects after 100 pages without the end.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    let position: TicketExportCursor = { afterCursor: '', resumeAt: unixSeconds(this.client.currentTime()) - ticketExportSeedLagSeconds, includeDeleted: this.isIncludingDeleted };
    let request: TicketExportPosition = { cursor: '', startTime: position.resumeAt };
    for (let page = 0; page < maximumBaselinePages; page++) {
      const listed = await this.listExportPage(context, request);
      position = advanceTicketExportCursor(position, listed);
      if (listed.isEndOfStream) return encodeTicketExportCursor(position);
      if (!position.afterCursor || position.afterCursor === request.cursor) throw new Error('Zendesk incremental export page has neither end_of_stream nor a new after_cursor');
      request = { cursor: position.afterCursor, startTime: 0n };
    }
    throw new Error(`Zendesk incremental export baseline exceeded ${maximumBaselinePages} pages`);
  }

  /**
   * Read one export page after cursor. A cursor Zendesk rejects with HTTP 400 or 422, a cursor without an
   * afterCursor, or a cursor saved with another includeDeleted restarts from its resumeAt time. An undecodable
   * cursor rejects with markCursorInvalid, so the kit records a new baseline. A rejected start_time restart fails
   * the poll instead, because a new baseline would also lose the changes after resumeAt. A 429 rejects with
   * markRateLimited and Retry-After.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<TicketChangedEvent>>> {
    let position: TicketExportCursor;
    try { position = decodeTicketExportCursor(cursor.token); } catch (error) { throw markCursorInvalid(error instanceof TicketExportCursorInvalidError ? error : new TicketExportCursorInvalidError()); }
    const restart: TicketExportPosition = { cursor: '', startTime: minimumBigInt(position.resumeAt, unixSeconds(this.client.currentTime()) - ticketExportStartTimeLagSeconds) };
    let request = restart;
    if (position.afterCursor && position.includeDeleted === this.isIncludingDeleted) request = { cursor: position.afterCursor, startTime: 0n };
    let listed: TicketExportPage;
    try { listed = await this.listExportPage(context, request); }
    catch (error) {
      if (!(error instanceof TicketExportCursorRejectedError)) throw error;
      position = { ...position, afterCursor: '' };
      request = restart;
      listed = await this.listExportPage(context, request);
    }
    const changes = listed.tickets
      .filter(exported => this.isIncludingDeleted || exported.ticket.status !== TicketStatusDeleted)
      .map(exported => newTicketChangedTriggerEvent(exported.ticket, exported.generatedTimestamp));
    const next = advanceTicketExportCursor({ ...position, includeDeleted: this.isIncludingDeleted }, listed);
    if (!listed.isEndOfStream && (!next.afterCursor || next.afterCursor === request.cursor)) throw new Error('Zendesk incremental export page has neither end_of_stream nor a new after_cursor');
    return { changes, next: encodeTicketExportCursor(next), hasMorePages: !listed.isEndOfStream };
  }

  // Halves per_page while Zendesk's answer exceeds maxResponseBytes.
  private async listExportPage(context: TriggerContext, position: TicketExportPosition): Promise<TicketExportPage> {
    for (;;) {
      const listed = await this.sendExportRequest(context, position);
      if (listed) return listed;
      if (this.pageSize <= minimumTicketExportPageSize) throw new Error(`Zendesk incremental export page of ${this.pageSize} tickets exceeds maxResponseBytes; raise maxResponseBytes`);
      this.pageSize = Math.max(Math.floor(this.pageSize / 2), minimumTicketExportPageSize);
    }
  }

  // Resolves undefined for an oversized response.
  private async sendExportRequest(context: TriggerContext, position: TicketExportPosition): Promise<TicketExportPage | undefined> {
    await waitForPollRequest(context);
    await this.client.exportRequestLimiter.waitForRequestStart(context.signal);
    const call = newTriggerCredentialCall(this.connection, TicketChangedTriggerDefinition.trigger, context.signal);
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) throw new Error('Zendesk Trigger credentials are unavailable');
    const query: Record<string, string> = { per_page: String(this.pageSize) };
    if (position.cursor) query.cursor = position.cursor;
    else query.start_time = position.startTime.toString();
    if (!this.isIncludingDeleted) query.exclude_deleted = 'true';
    const result = await this.client.exchange(call, credentials, operationName, 'GET', ticketExportPath, query);
    if (result.outcome === 'invalid') {
      if (result.failure.kind === 'RESPONSE_TOO_LARGE') return undefined;
      throw new Error(`Zendesk incremental export: ${result.failure.message}`);
    }
    if (result.outcome !== 'success') throw classifyTicketExportFailure(result, position);
    return decodeTicketExportPage(result.response.body, this.client.agentTicketURL);
  }
}
