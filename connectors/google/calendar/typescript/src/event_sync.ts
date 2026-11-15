// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Internal: the eventChanged sync fetcher, cursor document, and change conversion; index.ts does not export it.
import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { pathEscape, type CalendarClient, type CalendarResponse } from './client.js';
import type { Event } from './generated/models.js';
import { decodeEvent } from './event.js';
import { classifyFailureStatus, googleErrorReason } from './operations.js';
import { EventChangeKind, EventChangedTriggerDefinition, type EventChangedEvent } from './trigger.js';

/** Google reports no change type, so created means created and updated times within this many nanoseconds. */
const createdUpdatedToleranceNanoseconds = 1_000_000_000n;
/** Below MaxChangesPerPage and Google's own default. */
const eventSyncPageSize = 250;
/** Bounds a first full sync at one million events. */
const maximumBaselinePages = 4000;
const throttleReasons = new Set(['rateLimitExceeded', 'userRateLimitExceeded', 'quotaExceeded', 'dailyLimitExceeded']);
const rfc3339Pattern = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.(\d{1,9}))?(Z|[+-]\d\d:\d\d)$/;

/** Hash the event identity and version, so the ID stays short for any event ID length. */
function eventChangeId(calendarId: string, event: Event): string {
  const identity = [calendarId, event.id, event.etag ?? '', event.updated ?? '', event.status].join('\u0000');
  return `gcal-${createHash('sha256').update(identity, 'utf8').digest('hex').slice(0, 32)}`;
}

/** Return Unix nanoseconds for an RFC 3339 time as Go's time.RFC3339Nano parses it, or undefined. */
function parseRFC3339Nanoseconds(value: string | undefined): bigint | undefined {
  const match = rfc3339Pattern.exec(value ?? '');
  if (!match) return undefined;
  const [, year, month, day, hour, minute, second, fraction, offset] = match;
  const milliseconds = Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second));
  const moment = new Date(milliseconds);
  if (moment.getUTCDate() !== Number(day) || moment.getUTCMonth() !== Number(month) - 1 || Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59) return undefined;
  let offsetSeconds = 0;
  if (offset !== 'Z') {
    const offsetHours = Number(offset!.slice(1, 3)), offsetMinutes = Number(offset!.slice(4, 6));
    if (offsetHours > 23 || offsetMinutes > 59) return undefined;
    offsetSeconds = (offsetHours * 60 + offsetMinutes) * 60 * (offset!.startsWith('+') ? 1 : -1);
  }
  return (BigInt(milliseconds / 1000) - BigInt(offsetSeconds)) * 1_000_000_000n + BigInt((fraction ?? '').padEnd(9, '0'));
}

function eventChangeKind(event: Event): EventChangeKind {
  if (event.status === EventChangeKind.Cancelled) return EventChangeKind.Cancelled;
  const created = parseRFC3339Nanoseconds(event.created), updated = parseRFC3339Nanoseconds(event.updated);
  if (created !== undefined && updated !== undefined) {
    const difference = updated - created;
    if ((difference < 0n ? -difference : difference) <= createdUpdatedToleranceNanoseconds) return EventChangeKind.Created;
  }
  return EventChangeKind.Updated;
}

function eventChangeTime(event: Event, now: Date): Date {
  for (const value of [event.updated, event.created]) {
    const nanoseconds = parseRFC3339Nanoseconds(value);
    if (nanoseconds !== undefined) return new Date(Number(nanoseconds / 1_000_000n));
  }
  return now;
}

/** Convert one sync item; its ID is a pure function of the event version, so a refetch keeps it. */
export function newEventChangedTriggerEvent(calendarId: string, event: Event, now: Date): TriggerEvent<EventChangedEvent> {
  return { id: eventChangeId(calendarId, event), occurredAt: eventChangeTime(event, now), payload: { calendarId, changeKind: eventChangeKind(event), event } };
}

/** The committed position; pageToken is set while an incremental sync spans pages. */
interface EventSyncCursor { calendarId: string; syncToken: string; pageToken?: string }

/** Encode as Go's json.Marshal of eventSyncCursor: struct field order, pageToken omitted when empty. */
export function encodeEventSyncCursor(position: EventSyncCursor): Cursor {
  const pageToken = position.pageToken ? `,"pageToken":${encodeGoJSONValue(position.pageToken)}` : '';
  return { token: `{"calendarId":${encodeGoJSONValue(position.calendarId)},"syncToken":${encodeGoJSONValue(position.syncToken)}${pageToken}}` };
}

function decodeEventSyncCursor(cursor: Cursor): EventSyncCursor | undefined {
  let position: unknown;
  try { position = JSON.parse(cursor.token); } catch { return undefined; }
  if (!position || typeof position !== 'object' || Array.isArray(position)) return undefined;
  const fields = position as Record<string, unknown>;
  const values = ['calendarId', 'syncToken', 'pageToken'].map(name => fields[name] ?? '');
  if (values.some(value => typeof value !== 'string')) return undefined;
  const [calendarId, syncToken, pageToken] = values as [string, string, string];
  return syncToken ? { calendarId, syncToken, ...(pageToken ? { pageToken } : {}) } : undefined;
}

interface EventSyncPage { nextPageToken: string; nextSyncToken: string; items: unknown[] }

/** Reads one calendar's events.list incremental sync feed for the poll Trigger kit. */
export class EventSyncFetcher implements ChangeFetcher<TriggerEvent<EventChangedEvent>> {
  constructor(private readonly client: CalendarClient, private readonly connection: ConnectionRef, private readonly calendarId: string) {}

  async seedCursor(context: TriggerContext): Promise<Cursor> {
    let pageToken = '';
    const requestedPageTokens = new Set<string>();
    for (let page = 0; page < maximumBaselinePages; page++) {
      const listed = await this.listSyncPage(context, '', pageToken);
      if (listed.nextSyncToken) return encodeEventSyncCursor({ calendarId: this.calendarId, syncToken: listed.nextSyncToken });
      if (!listed.nextPageToken || requestedPageTokens.has(listed.nextPageToken)) throw new Error('Google Calendar full sync page has neither a new page token nor a sync token');
      requestedPageTokens.add(listed.nextPageToken);
      pageToken = listed.nextPageToken;
    }
    throw new Error(`Google Calendar full sync exceeded ${maximumBaselinePages} pages`);
  }

  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<EventChangedEvent>>> {
    const position = decodeEventSyncCursor(cursor);
    if (!position) throw markCursorInvalid(new Error('Google Calendar sync cursor is not decodable'));
    if (position.calendarId !== this.calendarId) throw markCursorInvalid(new Error('Google Calendar sync cursor belongs to another calendar'));
    const listed = await this.listSyncPage(context, position.syncToken, position.pageToken ?? '');
    if (listed.items.length > MaxChangesPerPage) throw new Error(`Google Calendar sync page has ${listed.items.length} events, more than ${MaxChangesPerPage}`);
    const now = new Date();
    const changes = listed.items.map(item => {
      let event: Event;
      try { event = decodeEvent(item, this.calendarId); }
      catch (error) { throw new Error(`Google Calendar sync page has an invalid event: ${error instanceof Error ? error.message : 'invalid event'}`); }
      return newEventChangedTriggerEvent(this.calendarId, event, now);
    });
    if (listed.nextPageToken) return { changes, next: encodeEventSyncCursor({ calendarId: this.calendarId, syncToken: position.syncToken, pageToken: listed.nextPageToken }), hasMorePages: true };
    if (listed.nextSyncToken) return { changes, next: encodeEventSyncCursor({ calendarId: this.calendarId, syncToken: listed.nextSyncToken }) };
    throw new Error('Google Calendar sync page has neither a page token nor a sync token');
  }

  /** Send one events.list request with only maxResults, syncToken, and pageToken. */
  private async listSyncPage(context: TriggerContext, syncToken: string, pageToken: string): Promise<EventSyncPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, EventChangedTriggerDefinition.trigger, context.signal);
    const accessToken = await this.client.resolveAccessToken(call);
    if (!accessToken) throw new Error('Google Calendar Trigger credentials are unavailable');
    const query: Record<string, string> = { maxResults: String(eventSyncPageSize) };
    if (syncToken) query.syncToken = syncToken;
    if (pageToken) query.pageToken = pageToken;
    let response: CalendarResponse;
    try { response = await this.client.request(call, accessToken, 'GET', `/calendars/${pathEscape(this.calendarId)}/events`, query); }
    catch { throw new Error('Google Calendar sync request failed: provider request failed'); }
    if (response.isTooLarge) throw new Error('Google Calendar sync request failed: provider response exceeds configured limit');
    if (response.status === 410 && syncToken) throw markCursorInvalid(new Error('Google Calendar sync token expired (HTTP 410)'));
    if (response.status < 200 || response.status >= 300) {
      const reason = googleErrorReason(response);
      if (throttleReasons.has(reason) || response.status === 429) {
        // Quotas are per user and project; an HTTP-date Retry-After still pauses the connection.
        const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
        const failure = new Error(`Google Calendar sync request: provider rate limited the request${reason ? ` (${reason})` : ''}`);
        throw markRateLimited(failure, parseRetryAfter(String(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? ''), new Date()));
      }
      throw new Error(`Google Calendar sync request: ${classifyFailureStatus(EventChangedTriggerDefinition.trigger.triggerName, response).failure.message}`);
    }
    return decodeEventSyncPage(response.body);
  }
}

function decodeEventSyncPage(body: Uint8Array): EventSyncPage {
  const invalid = new Error('Google Calendar sync page is not valid JSON');
  let listed: unknown;
  try { listed = JSON.parse(Buffer.from(body).toString('utf8')); } catch { throw invalid; }
  if (!listed || typeof listed !== 'object' || Array.isArray(listed)) throw invalid;
  const fields = listed as Record<string, unknown>;
  const nextPageToken = fields.nextPageToken ?? '', nextSyncToken = fields.nextSyncToken ?? '', items = fields.items ?? [];
  if (typeof nextPageToken !== 'string' || typeof nextSyncToken !== 'string' || !Array.isArray(items)) throw invalid;
  return { nextPageToken, nextSyncToken, items };
}
