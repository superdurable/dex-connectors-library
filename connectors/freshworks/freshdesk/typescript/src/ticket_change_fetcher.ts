// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { FreshdeskClient } from './client.js';
import type { Ticket } from './generated/models.js';
import { decodeTicketWire } from './operations.js';
import { TicketChangedTriggerDefinition, type TicketChangeKind, type TicketChangedEvent, type TicketChangedTriggerConfiguration } from './trigger_definition.js';

/** The per_page of each ticketChanged list request, Freshdesk's maximum of 100. */
export const TicketChangePageSize = 100;
/** The last page Freshdesk serves for List All Tickets (30,000 tickets). */
export const MaxTicketListPage = 300;
/**
 * How far, in milliseconds, before the committed updated_at high-water mark each poll window starts, so a
 * ticket whose update commits a little after its timestamp is still read. Tickets already delivered in the
 * window are recognized by ID and updated_at and not delivered again.
 */
export const TicketChangeOverlapMs = 60_000;

// Bounds the cursor's dedupe set under the 64 KiB cursor token limit.
const maximumDeliveredTicketKeys = 1500;
const maximumSeedPages = 15;
const maximumClockSkewMs = 2000;
// The Go connector's defaultRequestTimeout, part of the settled window floor.
const requestTimeoutMs = 12_000;
const zeroTime = Number.NEGATIVE_INFINITY;
const ticketListTimePattern = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/;
const goIntegerPattern = /^[+-]?\d+$/;
const operationName = 'ticketChanged';

/** Thrown for a ticketChanged cursor token that is not a valid Go cursor document. */
export class TicketChangeCursorInvalidError extends Error {
  /** Describe the rejected token without echoing it. */
  constructor() { super('Freshdesk ticketChanged cursor is not decodable'); this.name = 'TicketChangeCursorInvalidError'; }
}

/** The Go connector's cursor; field order is the Go struct's, so the encoding matches byte for byte. */
interface TicketChangeCursor { updatedSince: string; windowStart: string; page: number; floor: string; delivered: string[] }

interface TicketListPage { tickets: Ticket[]; hasNextPage: boolean; serverTimeMs: number | undefined }

interface DecodedTicketChangeCursor { position: TicketChangeCursor; sinceMs: number; floorMs: number; windowStartMs: number; pageNumber: number }

const unixSeconds = (milliseconds: number): number => Math.floor(milliseconds / 1000);
const truncateToSecond = (milliseconds: number): number => unixSeconds(milliseconds) * 1000;
const formatTicketListTime = (milliseconds: number): string => `${new Date(truncateToSecond(milliseconds)).toISOString().slice(0, 19)}Z`;

function parseTicketListTime(text: unknown): number {
  if (typeof text !== 'string' || !ticketListTimePattern.test(text)) throw new TicketChangeCursorInvalidError();
  const parsed = Date.parse(text);
  // Date.parse rolls an impossible date such as February 30 over; Go rejects it.
  if (!Number.isFinite(parsed) || formatTicketListTime(parsed) !== text) throw new TicketChangeCursorInvalidError();
  return parsed;
}

/** Name one delivered ticket version as "<id>@<updated_at Unix seconds>", as the cursor stores it. */
export function deliveredTicketKey(ticket: Ticket): string { return `${ticket.id}@${unixSeconds(Date.parse(ticket.updatedAt))}`; }

/**
 * Return the Trigger event ID "freshdesk-ticket-<id>-<updated_at Unix seconds>". It is a pure function of the
 * ticket version, so a refetch after a crash yields the same ID and a later change yields a new one.
 */
export function ticketChangeEventId(ticket: Ticket): string { return `freshdesk-ticket-${ticket.id}-${unixSeconds(Date.parse(ticket.updatedAt))}`; }

/** Report whether the binding delivers kind: every kind when changeKinds is empty or omitted. */
export function deliversTicketChangeKind(configuration: TicketChangedTriggerConfiguration, kind: TicketChangeKind): boolean {
  const kinds = configuration.changeKinds ?? [];
  return kinds.length === 0 || kinds.includes(kind);
}

function steadyTicketWindowStart(highWaterMs: number, floorMs: number): number {
  const windowStart = highWaterMs - TicketChangeOverlapMs;
  return floorMs > windowStart ? floorMs : windowStart;
}

// The floor is kept only while it still raises the steady window start.
function formatTicketCursorFloor(floorMs: number, highWaterMs: number): string {
  return floorMs > highWaterMs - TicketChangeOverlapMs ? formatTicketListTime(floorMs) : '';
}

/**
 * Keep keys updated at or after windowStartMs, the newest 1,500. When the bound drops keys, the returned floor
 * is the earliest second whose keys are all kept, so a later window that starts there rereads no dropped
 * version; otherwise it is negative infinity.
 */
export function pruneDeliveredTicketKeys(keys: readonly string[], windowStartMs: number): { pruned: string[]; floorMs: number } {
  const windowSecond = BigInt(unixSeconds(windowStartMs));
  const kept: { key: string; updatedAt: bigint }[] = [];
  const seen = new Set<string>();
  for (const key of keys) {
    const separator = key.indexOf('@');
    if (separator < 0 || seen.has(key)) continue;
    const text = key.slice(separator + 1);
    if (!goIntegerPattern.test(text)) continue;
    const updatedAt = BigInt(text);
    if (updatedAt < -(1n << 63n) || updatedAt >= 1n << 63n || updatedAt < windowSecond) continue;
    seen.add(key);
    kept.push({ key, updatedAt });
  }
  kept.sort((left, right) => (left.updatedAt < right.updatedAt ? -1 : left.updatedAt > right.updatedAt ? 1 : 0));
  let floorMs = zeroTime;
  let retained = kept;
  const dropped = kept.length - maximumDeliveredTicketKeys;
  if (dropped > 0) {
    let floorSecond = kept[dropped]!.updatedAt;
    // Part of this second was dropped, so the window starts after it and its kept keys go too.
    if (kept[dropped - 1]!.updatedAt === floorSecond) floorSecond += 1n;
    floorMs = Number(floorSecond) * 1000;
    retained = kept.slice(dropped).filter(entry => entry.updatedAt >= floorSecond);
  }
  return { pruned: retained.map(entry => entry.key), floorMs };
}

// Go replaces unpaired surrogates and escapes these for HTML safety; JSON.stringify does neither.
function encodeGoJSONString(text: string): string {
  return JSON.stringify(text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD')).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode the cursor as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings. */
function encodeTicketChangeCursor(position: TicketChangeCursor): Cursor {
  const members = [`"updatedSince":${encodeGoJSONString(position.updatedSince)}`];
  if (position.windowStart) members.push(`"windowStart":${encodeGoJSONString(position.windowStart)}`);
  if (position.page) members.push(`"page":${position.page}`);
  if (position.floor) members.push(`"floor":${encodeGoJSONString(position.floor)}`);
  if (position.delivered.length) members.push(`"delivered":[${position.delivered.map(encodeGoJSONString).join(',')}]`);
  return { token: `{${members.join(',')}}` };
}

/**
 * Decode the token as Go's encoding/json fills the cursor struct: keys match case-insensitively, the last
 * duplicate wins, unknown keys and null values are ignored, and a value of the wrong JSON type rejects it.
 */
function decodeCursorPosition(token: string): TicketChangeCursor {
  let document: unknown;
  try { document = JSON.parse(token) as unknown; } catch { throw new TicketChangeCursorInvalidError(); }
  const position: TicketChangeCursor = { updatedSince: '', windowStart: '', page: 0, floor: '', delivered: [] };
  if (document === null) return position;
  if (typeof document !== 'object' || Array.isArray(document)) throw new TicketChangeCursorInvalidError();
  for (const [name, value] of Object.entries(document as Record<string, unknown>)) {
    if (value === null) continue;
    const folded = name.toLowerCase();
    if (folded === 'updatedsince' || folded === 'windowstart' || folded === 'floor') {
      if (typeof value !== 'string') throw new TicketChangeCursorInvalidError();
      if (folded === 'updatedsince') position.updatedSince = value;
      else if (folded === 'windowstart') position.windowStart = value;
      else position.floor = value;
    } else if (folded === 'page') {
      if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TicketChangeCursorInvalidError();
      position.page = value;
    } else if (folded === 'delivered') {
      if (!Array.isArray(value) || value.some(item => item !== null && typeof item !== 'string')) throw new TicketChangeCursorInvalidError();
      position.delivered = value.map(item => (typeof item === 'string' ? item : ''));
    }
  }
  return position;
}

function decodeTicketChangeCursor(cursor: Cursor): DecodedTicketChangeCursor {
  const position = decodeCursorPosition(cursor.token);
  const sinceMs = parseTicketListTime(position.updatedSince);
  const floorMs = position.floor ? parseTicketListTime(position.floor) : zeroTime;
  const steady = steadyTicketWindowStart(sinceMs, floorMs);
  if (position.page === 0 && position.windowStart === '') return { position, sinceMs, floorMs, windowStartMs: steady, pageNumber: 1 };
  const backlogStartMs = parseTicketListTime(position.windowStart);
  if (backlogStartMs > sinceMs || position.page === 1 || position.page < 0 || position.page > MaxTicketListPage) throw new TicketChangeCursorInvalidError();
  // A backlog window never starts before the floor.
  if (position.page === 0) return { position, sinceMs, floorMs, windowStartMs: Math.max(floorMs, backlogStartMs), pageNumber: 1 };
  // Offset paging repeats the exact query of its first page.
  return { position, sinceMs, floorMs, windowStartMs: backlogStartMs, pageNumber: position.page };
}

function headerValues(headers: Record<string, string | string[] | undefined>, name: string): string[] {
  const value = headers[name];
  return value === undefined ? [] : Array.isArray(value) ? value : [value];
}

function parseHTTPDate(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? truncateToSecond(parsed) : undefined;
}

function decodeTicketList(body: Uint8Array): Ticket[] {
  let rows: unknown;
  try { rows = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { throw new TypeError('the response is not a ticket array'); }
  if (rows === null) return [];
  if (!Array.isArray(rows)) throw new TypeError('the response is not a ticket array');
  if (rows.length > TicketChangePageSize) throw new TypeError(`the page has ${rows.length} tickets, more than ${TicketChangePageSize}`);
  return rows.map(row => decodeTicketWire(row));
}

/**
 * Reads Freshdesk's List All Tickets in updated_at order for the poll Trigger kit, at parity with the Go
 * connector: the cursor is the updated_at high-water mark, a one-minute overlap window, and the
 * "<id>@<updated_at>" keys delivered inside it, encoded as Go's cursor document byte for byte. A poller owns
 * one fetcher and calls it one poll at a time.
 */
export class TicketChangeFetcher implements ChangeFetcher<TriggerEvent<TicketChangedEvent>> {
  /** Bind the fetcher to one connection's client and one binding's validated configuration. */
  constructor(private readonly client: FreshdeskClient, private readonly connection: ConnectionRef, private readonly configuration: TicketChangedTriggerConfiguration) {
    if (!client || !connection || !configuration) throw new TypeError('Freshdesk ticketChanged client, connection, and configuration are required');
  }

  /**
   * Record the baseline without reporting existing tickets: the high-water mark is Freshdesk's current time
   * from the response Date header, or the local clock without one, and tickets changed within the overlap
   * before it are remembered as delivered.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const baselineMs = truncateToSecond(this.client.currentTime().getTime());
    let seeded = await this.listSeedWindow(context, baselineMs);
    // A skewed local clock would put the window in the wrong place, so list again on Freshdesk's clock.
    if (seeded.serverTimeMs !== undefined && Math.abs(seeded.serverTimeMs - baselineMs) > maximumClockSkewMs) seeded = await this.listSeedWindow(context, seeded.serverTimeMs);
    return encodeTicketChangeCursor(seeded.position);
  }

  private async listSeedWindow(context: TriggerContext, baselineMs: number): Promise<{ position: TicketChangeCursor; serverTimeMs: number | undefined }> {
    const delivered: string[] = [];
    let serverTimeMs: number | undefined;
    let sinceMs = baselineMs;
    for (let pageNumber = 1; pageNumber <= maximumSeedPages; pageNumber++) {
      const listed = await this.listTicketPage(context, baselineMs - TicketChangeOverlapMs, pageNumber);
      if (pageNumber === 1) serverTimeMs = listed.serverTimeMs;
      for (const ticket of listed.tickets) {
        delivered.push(deliveredTicketKey(ticket));
        sinceMs = Math.max(sinceMs, Date.parse(ticket.updatedAt));
      }
      if (!listed.hasNextPage) break;
    }
    const { pruned, floorMs } = pruneDeliveredTicketKeys(delivered, sinceMs - TicketChangeOverlapMs);
    return { position: { updatedSince: formatTicketListTime(sinceMs), windowStart: '', page: 0, floor: formatTicketCursorFloor(floorMs, sinceMs), delivered: pruned }, serverTimeMs };
  }

  /**
   * Read one page of tickets updated since the cursor's window start, oldest first. An undecodable cursor and a
   * Freshdesk 400 for the cursor's query reject with markCursorInvalid, so the kit records a new baseline; a
   * 429 rejects with markRateLimited and Retry-After.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<TicketChangedEvent>>> {
    let decoded: DecodedTicketChangeCursor;
    try { decoded = decodeTicketChangeCursor(cursor); } catch (error) { throw markCursorInvalid(error); }
    const listed = await this.listTicketPage(context, decoded.windowStartMs, decoded.pageNumber);
    const delivered = new Set(decoded.position.delivered);
    const deliveredTicketIDs = new Set(decoded.position.delivered.map(key => key.split('@', 1)[0]));
    // A backlog window starts at the last second read, so creation is judged from the committed position.
    const createdFloorMs = Math.min(decoded.sinceMs - TicketChangeOverlapMs, decoded.windowStartMs);
    const next: TicketChangeCursor = { updatedSince: '', windowStart: '', page: 0, floor: '', delivered: [...decoded.position.delivered] };
    let highWaterMs = decoded.sinceMs;
    const changes: TriggerEvent<TicketChangedEvent>[] = [];
    for (const ticket of listed.tickets) {
      const key = deliveredTicketKey(ticket);
      const updatedAtMs = Date.parse(ticket.updatedAt);
      if (delivered.has(key) || updatedAtMs < decoded.windowStartMs) continue;
      // A ticket created inside the window that the binding never delivered is new to the binding.
      const kind: TicketChangeKind = Date.parse(ticket.createdAt) >= createdFloorMs && !deliveredTicketIDs.has(String(ticket.id)) ? 'created' : 'updated';
      delivered.add(key);
      next.delivered.push(key);
      highWaterMs = Math.max(highWaterMs, updatedAtMs);
      if (deliversTicketChangeKind(this.configuration, kind)) changes.push({ id: ticketChangeEventId(ticket), occurredAt: new Date(updatedAtMs), payload: { changeKind: kind, ticket } });
    }
    next.updatedSince = formatTicketListTime(highWaterMs);
    let floorMs = decoded.floorMs;
    let nextWindowStartMs = steadyTicketWindowStart(highWaterMs, floorMs);
    let hasMorePages = false;
    const lastTicket = listed.tickets.at(-1);
    if (decoded.pageNumber >= MaxTicketListPage && listed.tickets.length === TicketChangePageSize) {
      // Freshdesk serves no page past 300 and names none, so the rest of this one second is skipped.
      hasMorePages = true;
      floorMs = decoded.windowStartMs + 1000;
      nextWindowStartMs = steadyTicketWindowStart(highWaterMs, floorMs);
    } else if (!listed.hasNextPage || !lastTicket) {
      // The window is read completely.
    } else if (Date.parse(lastTicket.updatedAt) > decoded.windowStartMs) {
      // Offsets shift when a listed ticket changes, so restart at the last second read.
      hasMorePages = true;
      nextWindowStartMs = Date.parse(lastTicket.updatedAt);
      next.windowStart = formatTicketListTime(nextWindowStartMs);
    } else {
      // More than a page shares one second, so only an offset makes progress.
      hasMorePages = true;
      nextWindowStartMs = decoded.windowStartMs;
      next.windowStart = formatTicketListTime(decoded.windowStartMs);
      next.page = decoded.pageNumber + 1;
    }
    if (!hasMorePages && listed.serverTimeMs !== undefined) {
      // Every update older than this has committed and been read, so a quiet helpdesk stops rereading.
      floorMs = Math.max(floorMs, listed.serverTimeMs - TicketChangeOverlapMs - requestTimeoutMs);
      nextWindowStartMs = steadyTicketWindowStart(highWaterMs, floorMs);
    }
    const { pruned, floorMs: overflowFloorMs } = pruneDeliveredTicketKeys(next.delivered, Math.min(steadyTicketWindowStart(highWaterMs, floorMs), nextWindowStartMs));
    next.delivered = pruned;
    floorMs = Math.max(floorMs, overflowFloorMs);
    next.floor = formatTicketCursorFloor(floorMs, highWaterMs);
    return { changes, next: encodeTicketChangeCursor(next), hasMorePages };
  }

  private async listTicketPage(context: TriggerContext, windowStartMs: number, pageNumber: number): Promise<TicketListPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, TicketChangedTriggerDefinition.trigger, context.signal);
    const credential = await this.client.resolveCredential(call, operationName);
    if (!('key' in credential)) throw new Error('Freshdesk ticketChanged credentials are unavailable');
    // Keys in sorted order, as Go's url.Values encodes them.
    const query = new URLSearchParams([
      ['order_by', 'updated_at'], ['order_type', 'asc'], ['page', String(pageNumber)],
      ['per_page', String(TicketChangePageSize)], ['updated_since', formatTicketListTime(windowStartMs)],
    ]);
    const result = await this.client.exchange(call, operationName, credential.key, 'GET', '/tickets', query, undefined, context.signal);
    if (result.outcome !== 'succeeded') {
      const failure = new Error(`Freshdesk ticket list request: ${result.failure.message}`);
      if (result.outcome === 'rateLimited') throw markRateLimited(failure, result.retryAfterMs);
      if (result.outcome === 'rejected' && result.response?.status === 400) throw markCursorInvalid(failure);
      throw failure;
    }
    let tickets: Ticket[];
    try { tickets = decodeTicketList(result.response.body); }
    catch (error) { throw new Error(`Freshdesk ticket list page is invalid: ${error instanceof Error ? error.message : String(error)}`); }
    const headers = result.response.headers;
    return { tickets, hasNextPage: headerValues(headers, 'link').some(link => link.includes('rel="next"')), serverTimeMs: parseHTTPDate(headerValues(headers, 'date')[0]) };
  }
}
