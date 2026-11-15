// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  DefaultKeyedSnapshotEntries, applyKeyedSnapshotChanges, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedItem, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshot } from '@superdurable/connectors-sdk/projectconfig';
import { summarizeError, type ZohoDeskClient } from './client.js';
import type { Ticket } from './generated/models.js';
import { count, decodeObject, decodeTicket, timestamp } from './ticket.js';
import {
  TicketChangeCreated, TicketChangeUpdated, TicketChangedTriggerDefinition,
  type TicketChangeKind, type TicketChangedEvent, type TicketChangedTriggerConfiguration,
} from './ticket_changed.js';

/** The limit of each ticketChanged search request, Zoho Desk's largest search page. */
export const TicketPollPageLimit = 100;
/**
 * How far before the newest modified time it read each ticketChanged sweep starts again, in milliseconds, because
 * Zoho Desk indexes changed tickets for search with a delay. A ticket version re-read inside the lookback is
 * recognized by the cursor snapshot and not delivered twice.
 */
export const TicketSearchIndexLookbackMs = 5 * 60 * 1000;
/** The cursor snapshot bound: 10,000 recent ticket versions, the least recently seen evicted first. */
export const TicketSnapshotOptions: KeyedSnapshotOptions = { maxEntries: DefaultKeyedSnapshotEntries, evictLeastRecentlySeen: true };

// from+limit stays within the 1,000 results Zoho Desk's search documents.
const maximumFrom = 1000 - TicketPollPageLimit;
const maximumBaselinePages = DefaultKeyedSnapshotEntries / TicketPollPageLimit;
const maximumServerClockSkewMs = 24 * 60 * 60 * 1000;
const operation = 'ticketChanged';

/** A modified-time keyset position; every time is Unix milliseconds. */
interface SearchPosition { sweepStart: number; floor: number; from: number; highWater: number }
interface SearchPage { tickets: Ticket[]; received: number; last: number | undefined; serverTime: number | undefined }

/** Format a Unix millisecond instant as Zoho Desk's UTC millisecond timestamp, such as 2026-03-02T09:00:00.000Z. */
export function formatZohoTimestamp(milliseconds: number): string { return new Date(milliseconds).toISOString(); }

/** Return the Unix milliseconds of a Go JSON timestamp, truncating any finer fraction. */
export function goTimestampMilliseconds(value: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?Z$/.exec(value);
  if (!match) throw new Error('timestamp is not a UTC Go time');
  const whole = Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]), Number(match[4]), Number(match[5]), Number(match[6]));
  return whole + Number((match[7] ?? '').slice(0, 3).padEnd(3, '0'));
}

/**
 * Return the Trigger event ID of one ticket version: zdesk- and 32 hex digits of SHA-256 over the organization,
 * ticket ID, and modified time, so a refetch after a crash yields the same ID and a later version a new one.
 */
export function ticketChangeEventId(organizationId: string, ticket: Ticket): string {
  const material = [organizationId, ticket.id, formatZohoTimestamp(goTimestampMilliseconds(ticket.modifiedAt))].join('\u0000');
  return 'zdesk-' + createHash('sha256').update(material, 'utf8').digest().subarray(0, 16).toString('hex');
}

/** Return the keyed-snapshot hash of one ticket version, as the Go connector computes it. */
export function ticketVersionHash(ticket: Ticket): string {
  const material = ticket.id + '\u0000' + formatZohoTimestamp(goTimestampMilliseconds(ticket.modifiedAt));
  return createHash('sha256').update(material, 'utf8').digest().subarray(0, 16).toString('hex');
}

/** Report created for an unknown ticket created at or after windowStart, else updated. */
export function ticketChangeKind(ticket: Ticket, isKnown: boolean, windowStart: number): TicketChangeKind {
  return !isKnown && goTimestampMilliseconds(ticket.createdAt) >= windowStart ? TicketChangeCreated : TicketChangeUpdated;
}

class TicketCursorInvalidError extends Error {}

/**
 * Reads one organization's tickets in ascending modified-time order for the poll Trigger kit, at parity with the Go
 * connector: the cursor is a modified-time keyset position (sweep start, floor, offset within one millisecond, and
 * high-water mark) plus a keyed snapshot of up to 10,000 recent ticket versions, so tickets changed during a sweep
 * are never skipped and re-read versions are never delivered twice. Its cursor token is the Go connector's JSON
 * byte for byte. A poller owns one fetcher and calls it from one task at a time.
 */
export class TicketChangeFetcher implements ChangeFetcher<TriggerEvent<TicketChangedEvent>> {
  private readonly departmentId: string;

  /** Bind the fetcher to one connection's client and one binding's department. */
  constructor(private readonly client: ZohoDeskClient, private readonly connection: ConnectionRef, configuration: TicketChangedTriggerConfiguration) {
    if (!client || !connection || !configuration) throw new TypeError('Zoho Desk ticketChanged client, connection, and configuration are required');
    this.departmentId = configuration.departmentId ?? '';
  }

  /**
   * Record every ticket modified inside the lookback before now as the baseline and deliver none. A local clock
   * skewed from Zoho Desk's Date header by more than a second yields to it, so a fast local clock skips nothing.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const now = this.client.currentTime().getTime();
    const start = now - TicketSearchIndexLookbackMs;
    let position: SearchPosition = { sweepStart: start, floor: start, from: 0, highWater: now };
    let snapshot: KeyedSnapshot | undefined;
    for (let pageNumber = 0; pageNumber < maximumBaselinePages; pageNumber++) {
      const page = await this.searchPage(context, position);
      snapshot = applyTicketVersions(snapshot, page.tickets);
      if (pageNumber === 0 && isServerClockSkewed(page.serverTime, now)) position = { ...position, highWater: page.serverTime! };
      const [next, hasMorePages] = nextSearchPosition(position, page);
      position = next;
      if (!hasMorePages) return this.encodeCursor(position, snapshot);
    }
    throw new Error(`Zoho Desk ticketChanged baseline exceeded ${maximumBaselinePages} search pages`);
  }

  /**
   * Read one search page after cursor and return the ticket versions the snapshot lacks. An undecodable cursor, one
   * for another organization or department, or a 400 or 422 naming modifiedTimeRange or from throws
   * markCursorInvalid, so the kit records a new baseline; a 429 throws markRateLimited with Retry-After.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<TicketChangedEvent>>> {
    let position: SearchPosition;
    try { position = this.decodeCursor(cursor.token); }
    catch (error) { throw markCursorInvalid(error); }
    const page = await this.searchPage(context, position);
    const known = new Map((cursor.snapshot?.entries ?? []).map(entry => [entry.key, entry.hash]));
    const changes: TriggerEvent<TicketChangedEvent>[] = [];
    for (const ticket of page.tickets) {
      const storedHash = known.get(ticket.id);
      if (storedHash !== undefined && storedHash === ticketVersionHash(ticket)) continue;
      const payload: TicketChangedEvent = {
        orgId: this.client.organizationId, ...(this.departmentId ? { departmentId: this.departmentId } : {}),
        changeKind: ticketChangeKind(ticket, storedHash !== undefined, position.sweepStart), ticket,
      };
      changes.push({ id: ticketChangeEventId(this.client.organizationId, ticket), occurredAt: new Date(goTimestampMilliseconds(ticket.modifiedAt)), payload });
    }
    const snapshot = applyTicketVersions(cursor.snapshot, page.tickets);
    const [next, hasMorePages] = nextSearchPosition(position, page);
    return { changes, next: this.encodeCursor(next, snapshot), hasMorePages };
  }

  private async searchPage(context: TriggerContext, position: SearchPosition): Promise<SearchPage> {
    await waitForPollRequest(context);
    // A fresh call per request gets its own operation deadline.
    const call = newTriggerCredentialCall(this.connection, TicketChangedTriggerDefinition.trigger, context.signal);
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) throw new Error(`Zoho Desk ticketChanged poll: ${session.failure.message}`);
    const result = await this.client.exchange(call, operation, session, 'GET', '/tickets/search', this.searchParameters(position));
    if ('outcome' in result) {
      const failure = new Error(result.outcome.failure.message);
      // Zoho Desk meters API credits per organization, so every binding of the connection pauses.
      if (result.outcome.disposition === 'rateLimited') throw markRateLimited(failure, result.outcome.retryAfterMs);
      const status = result.outcome.response?.status ?? 0;
      if (result.outcome.disposition === 'rejected' && (status === 400 || status === 422) &&
        summarizeError(result.outcome.response?.body ?? new Uint8Array()).errors.some(field => field.startsWith('/modifiedTimeRange') || field.startsWith('/from'))) {
        throw markCursorInvalid(failure);
      }
      throw failure;
    }
    return decodeSearchPage(result.response.status, result.response.body, serverTime(result.response.headers.date));
  }

  private searchParameters(position: SearchPosition): Record<string, string> {
    const upper = Math.max(this.client.currentTime().getTime(), position.floor);
    return {
      modifiedTimeRange: formatZohoTimestamp(position.floor) + ',' + formatZohoTimestamp(upper),
      sortBy: 'modifiedTime', from: String(position.from), limit: String(TicketPollPageLimit),
      ...(this.departmentId ? { departmentId: this.departmentId } : {}),
    };
  }

  private decodeCursor(token: string): SearchPosition {
    const fields = decodeCursorFields(token);
    if (fields.orgId !== this.client.organizationId || fields.departmentId !== this.departmentId) {
      throw new TicketCursorInvalidError('Zoho Desk ticket cursor belongs to another organization or department');
    }
    if (fields.from < 0 || fields.from > maximumFrom) throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable');
    return { sweepStart: cursorTime(fields.sweepStart), floor: cursorTime(fields.floor), from: fields.from, highWater: cursorTime(fields.highWater) };
  }

  /** Encode the position as Go's json.Marshal encodes the struct: field order and omitted empty fields. */
  private encodeCursor(position: SearchPosition, snapshot: KeyedSnapshot | undefined): Cursor {
    let token = `{"orgId":${JSON.stringify(this.client.organizationId)}`;
    if (this.departmentId) token += `,"departmentId":${JSON.stringify(this.departmentId)}`;
    token += `,"sweepStart":"${formatZohoTimestamp(position.sweepStart)}","floor":"${formatZohoTimestamp(position.floor)}"`;
    if (position.from) token += `,"from":${position.from}`;
    token += `,"highWater":"${formatZohoTimestamp(position.highWater)}"}`;
    return { token, ...(snapshot ? { snapshot } : {}) };
  }
}

/**
 * Advance by modified time, so a ticket changed during a sweep only moves later and is never skipped. A full page of
 * one instant pages by offset; past Zoho Desk's offset limit it steps one millisecond. A short page ends the sweep,
 * and the next sweep starts the lookback before the newest modified time read.
 */
export function nextSearchPosition(position: SearchPosition, page: SearchPage): [SearchPosition, boolean] {
  let highWater = position.highWater;
  for (const ticket of page.tickets) highWater = Math.max(highWater, goTimestampMilliseconds(ticket.modifiedAt));
  if (page.received < TicketPollPageLimit) {
    const start = highWater - TicketSearchIndexLookbackMs;
    return [{ sweepStart: start, floor: start, from: 0, highWater }, false];
  }
  const last = page.last ?? position.floor;
  if (last > position.floor) return [{ ...position, floor: last, from: 0, highWater }, true];
  if (position.from + TicketPollPageLimit <= maximumFrom) return [{ ...position, from: position.from + TicketPollPageLimit, highWater }, true];
  return [{ ...position, floor: position.floor + 1, from: 0, highWater }, true];
}

class TicketVersion implements KeyedItem {
  constructor(private readonly ticket: Ticket) {}
  snapshotKey(): string { return this.ticket.id; }
  snapshotHash(): string { return ticketVersionHash(this.ticket); }
  snapshotValue(): Uint8Array | undefined { return undefined; }
}

function applyTicketVersions(snapshot: KeyedSnapshot | undefined, tickets: Ticket[]): KeyedSnapshot {
  return applyKeyedSnapshotChanges(snapshot, tickets.map(ticket => new TicketVersion(ticket)), [], TicketSnapshotOptions);
}

/** Decode a {data, count} page in Zoho Desk's order; 204 is empty, and a twice-listed ticket keeps its newest version. */
function decodeSearchPage(status: number, body: Uint8Array, server: number | undefined): SearchPage {
  if (status === 204) return { tickets: [], received: 0, last: undefined, serverTime: server };
  let rows: unknown[];
  try {
    const document = decodeObject(body, 'invalid');
    if (!Array.isArray(document.data)) throw new Error('no data array');
    count(document.count);
    rows = document.data;
  } catch { throw new Error('Zoho Desk returned an invalid ticket search page'); }
  if (rows.length > TicketPollPageLimit) throw new Error('Zoho Desk ticket search page holds more tickets than requested');
  const tickets: Ticket[] = [];
  const positions = new Map<string, number>();
  let last: number | undefined;
  rows.forEach((row, index) => {
    let ticket: Ticket;
    try {
      if (!row || typeof row !== 'object' || Array.isArray(row)) throw new Error('response is not an object');
      ({ ticket } = decodeTicket(row as Record<string, unknown>));
    } catch (error) { throw new Error(`Zoho Desk ticket search result ${index}: ${error instanceof Error ? error.message : String(error)}`); }
    const searched: Ticket = { ...ticket };
    delete searched.descriptionHtml;
    delete searched.isDescriptionTruncated;
    const modified = goTimestampMilliseconds(searched.modifiedAt);
    last = modified;
    const listed = positions.get(searched.id);
    if (listed !== undefined) {
      if (modified > goTimestampMilliseconds(tickets[listed]!.modifiedAt)) tickets[listed] = searched;
      return;
    }
    positions.set(searched.id, tickets.length);
    tickets.push(searched);
  });
  return { tickets, received: rows.length, last, serverTime: server };
}

function serverTime(header: string | string[] | undefined): number | undefined {
  const value = Array.isArray(header) ? header[0] : header;
  if (!value) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

/** Report skew beyond the Date header's one-second precision but under 24 hours. */
function isServerClockSkewed(server: number | undefined, now: number): boolean {
  if (server === undefined) return false;
  const skew = Math.abs(server - now);
  return skew > 1000 && skew < maximumServerClockSkewMs;
}

interface CursorFields { orgId: string; departmentId: string; sweepStart: string; floor: string; from: number; highWater: string }
const cursorStringFields = ['orgId', 'departmentId', 'sweepStart', 'floor', 'highWater'] as const;
class GoNumberLiteral { constructor(readonly source: string) {} }

/**
 * Decode token as Go's encoding/json fills the cursor struct: keys match case-insensitively, the last duplicate
 * wins, unknown keys and null values are ignored, and a value of the wrong JSON type rejects the token.
 */
function decodeCursorFields(token: string): CursorFields {
  const fields: CursorFields = { orgId: '', departmentId: '', sweepStart: '', floor: '', from: 0, highWater: '' };
  let decoded: unknown;
  try {
    decoded = JSON.parse(token, (_key: string, parsed: unknown, context?: { source?: string }) =>
      typeof parsed === 'number' ? new GoNumberLiteral(context?.source ?? '') : parsed);
  } catch { throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable'); }
  if (decoded === null) return fields;
  if (typeof decoded !== 'object' || Array.isArray(decoded) || decoded instanceof GoNumberLiteral) throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable');
  for (const [key, value] of Object.entries(decoded as Record<string, unknown>)) {
    if (value === null) continue;
    const folded = key.toLowerCase();
    const field = cursorStringFields.find(name => name === key) ?? cursorStringFields.find(name => name.toLowerCase() === folded);
    if (field) {
      if (typeof value !== 'string') throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable');
      fields[field] = value;
    } else if (folded === 'from') {
      if (!(value instanceof GoNumberLiteral) || !/^-?(?:0|[1-9][0-9]*)$/.test(value.source) || !Number.isSafeInteger(Number(value.source))) {
        throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable');
      }
      fields.from = Number(value.source);
    }
  }
  return fields;
}

/** Parse a cursor time as Go's time.Parse(time.RFC3339Nano) does, in Unix milliseconds. */
function cursorTime(value: string): number {
  try { return goTimestampMilliseconds(timestamp('cursor', value)); }
  catch { throw new TicketCursorInvalidError('Zoho Desk ticket cursor is not decodable'); }
}
