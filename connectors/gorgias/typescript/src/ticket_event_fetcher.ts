// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { GorgiasClient } from './client.js';
import { GoJSONTypeError, decodeGoInt64, decodeGoString, goFieldMembers, parseGoJSON, type GoJSONValue } from './go_json.js';
import { GoTime, parseGorgiasTime, parseHTTPDate } from './go_time.js';
import { TicketEventOverlapNanoseconds, decodeTicketEventCursor, encodeTicketEventCursor, pruneSeenEventKeys, seenEventKey, type DecodedTicketEventCursor, type TicketEventCursor } from './ticket_event_cursor.js';
import { TicketChangedTriggerDefinition, TicketEventTypes, deliversTicketEventType, type TicketChangedEvent, type TicketChangedTriggerConfiguration } from './trigger_definition.js';

/** The limit of each ticketChanged event request, Gorgias's maximum of 100. */
export const TicketEventPageSize = 100;

const operationName = 'ticketChanged';
const ticketObjectType = 'Ticket';
// Bounds the seed listing of events recorded within the overlap before the baseline.
const maximumSeedPages = 10;
const apiCallLimitPattern = /^[0-9]{1,6}\/[0-9]{1,6}$/;
const listCursorPattern = /^[A-Za-z0-9+/=_.:-]{1,2048}$/;
const eventFields = ['id', 'type', 'object_id', 'object_type', 'user_id', 'context', 'created_datetime'] as const;

/** One event of GET /api/events, decoded as Go decodes it into gorgiasEventWire. */
interface EventWire { id: number; type: string; objectId: number | undefined; objectType: string | undefined; userId: number | undefined; context: string | undefined; createdDatetime: string }

interface EventPage { events: EventWire[]; createdAt: GoTime[]; serverTime: GoTime | undefined; isRateBudgetLow: boolean; isPageSizeFilled: boolean }

/** Name one event as "gorgias-event-<id>"; it is a pure function of Gorgias's event ID, so a refetch yields it again. */
export function ticketEventKey(eventId: number): string { return `gorgias-event-${eventId}`; }

/** Report an X-Gorgias-Account-Api-Call-Limit value, such as 31/40, at three quarters or more. */
export function isRateBudgetLow(apiCallLimit: string): boolean {
  const [used, limit] = apiCallLimit.split('/');
  if (used === undefined || limit === undefined || !/^[0-9]+$/.test(used) || !/^[0-9]+$/.test(limit)) return false;
  return Number(limit) > 0 && Number(used) * 4 >= Number(limit) * 3;
}

/** The ChangeFetcher of one ticketChanged binding: the account's ticket events, oldest first. */
export class TicketEventFetcher implements ChangeFetcher<TriggerEvent<TicketChangedEvent>> {
  private readonly localNow: () => GoTime;

  /**
   * Bind the client and the binding's event type selection; it sends nothing. localNow replaces the client's
   * clock at nanosecond precision, as Go reads it, for tests that replay a Go recording.
   */
  constructor(private readonly client: GorgiasClient, private readonly connection: ConnectionRef, private readonly configuration: TicketChangedTriggerConfiguration, localNow?: () => GoTime) {
    if (!client || !connection || !configuration) throw new TypeError('Gorgias ticketChanged client, connection, and configuration are required');
    this.localNow = localNow ?? (() => GoTime.fromDate(client.currentTime()));
  }

  /**
   * Record the baseline: Gorgias's current time from the first response's Date header, or the local clock only
   * without one, and the events recorded within the overlap before it, so no earlier event is delivered.
   */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const localNow = this.localNow();
    let start = localNow.add(-TicketEventOverlapNanoseconds);
    const seen: string[] = [];
    let baseline = localNow;
    for (let pageNumber = 1; pageNumber <= maximumSeedPages; pageNumber++) {
      const listed = await this.listEventPage(context, start);
      if (pageNumber === 1 && listed.serverTime) baseline = listed.serverTime;
      listed.events.forEach((event, index) => {
        const createdAt = listed.createdAt[index]!;
        seen.push(seenEventKey(event.id, createdAt));
        if (createdAt.isAfter(baseline)) baseline = createdAt;
      });
      if (!listed.isPageSizeFilled) break;
      start = nextScanStart(start, listed.createdAt.at(-1)!);
    }
    const position: TicketEventCursor = { since: baseline.formatRFC3339Nano(), scanFrom: '', seen: pruneSeenEventKeys(seen, baseline.add(-TicketEventOverlapNanoseconds)) };
    return { token: encodeTicketEventCursor(position) };
  }

  /**
   * Read one page of ticket events recorded since the cursor's window start, oldest first. Only an undecodable
   * cursor rejects with markCursorInvalid; a created_datetime filter never expires, so a Gorgias 400 backs off
   * and retries from the same position. A 429 rejects with markRateLimited and Retry-After.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<TicketChangedEvent>>> {
    let decoded: DecodedTicketEventCursor;
    try { decoded = decodeTicketEventCursor(cursor.token); } catch (error) { throw markCursorInvalid(error); }
    const { start } = decoded;
    const listed = await this.listEventPage(context, start);
    const seen = new Set(decoded.position.seen);
    const nextSeen = [...decoded.position.seen];
    let highWater = decoded.since;
    const changes: TriggerEvent<TicketChangedEvent>[] = [];
    listed.events.forEach((event, index) => {
      const createdAt = listed.createdAt[index]!;
      const key = seenEventKey(event.id, createdAt);
      if (seen.has(key) || createdAt.isBefore(start)) return;
      seen.add(key);
      nextSeen.push(key);
      if (createdAt.isAfter(highWater)) highWater = createdAt;
      const change = this.ticketChange(event, createdAt);
      if (change) changes.push({ id: ticketEventKey(change.eventId), occurredAt: createdAt.toDate(), payload: change });
    });
    const next: TicketEventCursor = { since: highWater.formatRFC3339Nano(), scanFrom: '', seen: [] };
    let hasMorePages = false;
    if (listed.isPageSizeFilled) {
      // A full page may hide more events at or after its last one, so the next request starts there.
      next.scanFrom = nextScanStart(start, listed.createdAt.at(-1)!).formatRFC3339Nano();
      // Near Gorgias's rate limit, the rest waits for the next scheduled poll.
      hasMorePages = !listed.isRateBudgetLow;
    }
    next.seen = pruneSeenEventKeys(nextSeen, highWater.add(-TicketEventOverlapNanoseconds));
    return { changes, next: { token: encodeTicketEventCursor(next) }, hasMorePages };
  }

  // Other types and non-ticket objects only move the cursor.
  private ticketChange(event: EventWire, createdAt: GoTime): TicketChangedEvent | undefined {
    if (!deliversTicketEventType(this.configuration, event.type) || event.objectType !== ticketObjectType || event.objectId === undefined || event.objectId < 1) return undefined;
    const change: TicketChangedEvent = { eventId: event.id, eventType: event.type, ticketId: event.objectId, occurredAt: createdAt.formatRFC3339Nano() };
    if (event.userId !== undefined && event.userId > 0) change.userId = event.userId;
    if (event.context) change.context = event.context;
    return change;
  }

  private async listEventPage(context: TriggerContext, start: GoTime): Promise<EventPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, TicketChangedTriggerDefinition.trigger, context.signal);
    const { credentials } = await this.client.resolveCredentials(call, operationName);
    if (!credentials) throw new Error('Gorgias ticketChanged credentials are unavailable');
    const query = new URLSearchParams([
      ['order_by', 'created_datetime:asc'], ['created_datetime[gte]', start.formatQueryMicroseconds()], ['limit', String(TicketEventPageSize)],
      // Gorgias filters by every read type, so a binding's selection never changes what the cursor passes.
      ...TicketEventTypes.map((eventType): [string, string] => ['types', eventType]),
    ]);
    const result = await this.client.exchange(call, credentials, operationName, 'GET', '/events', query, undefined, context.signal);
    const failure = new Error(`Gorgias event list request: ${result.failure?.message ?? 'failed'}`);
    if (result.outcome === 'rateLimited') throw markRateLimited(failure, result.retryAfterMs);
    if (result.outcome !== 'succeeded' || !result.response) throw failure;
    let listed: EventPage;
    try { listed = decodeEventPage(result.response.body); }
    catch (error) { throw new Error(`Gorgias event list page is invalid: ${error instanceof Error ? error.message : String(error)}`); }
    listed.serverTime = parseHTTPDate(headerValue(result.response.headers, 'date'));
    const apiCallLimit = headerValue(result.response.headers, 'x-gorgias-account-api-call-limit');
    listed.isRateBudgetLow = apiCallLimitPattern.test(apiCallLimit) && isRateBudgetLow(apiCallLimit);
    return listed;
  }
}

/** Decode the documented bare array or Gorgias's usual {"data": [...]} list envelope as Go does; throws naming the problem. */
export function decodeEventPage(body: Uint8Array): EventPage {
  const neither = 'the response is neither an event array nor a Gorgias list';
  let document: GoJSONValue;
  try { document = parseGoJSON(new TextDecoder().decode(body)); } catch { throw new Error(neither); }
  let events: EventWire[];
  try { events = decodeEvents(document); }
  catch (error) {
    if (!(error instanceof GoJSONTypeError)) throw error;
    try { events = decodeListEnvelope(document); }
    catch (envelopeError) { if (envelopeError instanceof GoJSONTypeError) throw new Error(neither); throw envelopeError; }
  }
  if (events.length > TicketEventPageSize) throw new Error(`the page has ${events.length} events, more than ${TicketEventPageSize}`);
  const createdAt: GoTime[] = [];
  events.forEach((event, index) => {
    if (event.id < 1) throw new Error('an event has no ID');
    let created: GoTime;
    try { created = parseGorgiasTime(event.createdDatetime); } catch { throw new Error('an event created_datetime is not an ISO 8601 timestamp'); }
    // The cursor relies on ascending order; a page out of order would skip events.
    if (index > 0 && created.isBefore(createdAt[index - 1]!)) throw new Error('the events are not in ascending created_datetime order');
    createdAt.push(created);
  });
  return { events, createdAt, serverTime: undefined, isRateBudgetLow: false, isPageSizeFilled: events.length === TicketEventPageSize };
}

function decodeEvents(document: GoJSONValue): EventWire[] {
  if (document.kind === 'null') return [];
  if (document.kind !== 'array') throw new GoJSONTypeError('not an array');
  return document.elements.map(decodeEvent);
}

function decodeListEnvelope(document: GoJSONValue): EventWire[] {
  if (document.kind !== 'object') throw new GoJSONTypeError('not a list envelope');
  let data: EventWire[] | undefined;
  let nextCursor: string | undefined;
  for (const { field, value } of goFieldMembers(document.members, ['data', 'meta'])) {
    if (field === 'data') { data = value.kind === 'null' ? undefined : decodeEvents(value); continue; }
    if (value.kind === 'null') { nextCursor = undefined; continue; }
    if (value.kind !== 'object') throw new GoJSONTypeError('meta is not an object');
    for (const member of goFieldMembers(value.members, ['next_cursor'])) {
      if (member.value.kind !== 'null' && member.value.kind !== 'string') throw new GoJSONTypeError('next_cursor is not a string');
      nextCursor = member.value.kind === 'string' ? member.value.text : undefined;
    }
  }
  if (data === undefined) throw new GoJSONTypeError('the event page has no data array');
  if (nextCursor && !listCursorPattern.test(nextCursor)) throw new GoJSONTypeError('the event page has an invalid next cursor');
  return data;
}

function decodeEvent(item: GoJSONValue): EventWire {
  const event: EventWire = { id: 0, type: '', objectId: undefined, objectType: undefined, userId: undefined, context: undefined, createdDatetime: '' };
  if (item.kind === 'null') return event;
  if (item.kind !== 'object') throw new GoJSONTypeError('event is not an object');
  for (const { field, value } of goFieldMembers(item.members, eventFields)) {
    const isNull = value.kind === 'null';
    if (field === 'id') { if (!isNull) event.id = decodeGoInt64(value); }
    else if (field === 'type') { if (!isNull) event.type = decodeGoString(value); }
    else if (field === 'created_datetime') { if (!isNull) event.createdDatetime = decodeGoString(value); }
    else if (field === 'object_id') event.objectId = isNull ? undefined : decodeGoInt64(value);
    else if (field === 'user_id') event.userId = isNull ? undefined : decodeGoInt64(value);
    else if (field === 'object_type') event.objectType = isNull ? undefined : decodeGoString(value);
    else event.context = isNull ? undefined : decodeGoString(value);
  }
  return event;
}

// When every event of a full page shares start's microsecond, step one microsecond past it.
function nextScanStart(start: GoTime, lastCreatedAt: GoTime): GoTime {
  return lastCreatedAt.isAfter(start) ? lastCreatedAt : start.add(1000n);
}

function headerValue(headers: Record<string, string | string[] | undefined>, name: string): string {
  const value = headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
