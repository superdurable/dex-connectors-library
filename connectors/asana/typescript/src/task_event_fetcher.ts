// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { Call, ConnectionRef, TriggerContext, TriggerEvent, TriggerRef } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { Client, Exchange } from './client.js';
import { InvalidEventJSONError, compactRawJSON, parseRawJSON, type RawJSONValue } from './event_json.js';
import type { EventParentReference, TaskChangedEvent } from './trigger_models.js';

/** Keeps task and subtask events; project, section, and story events are dropped. */
const taskEventResourceType = 'task';
/** Asana's documented cap of events per sync token response. */
export const MaximumEventsPerPage = 100;
/** Asks for names and change details; gids are always returned. */
export const TaskEventFields = 'action,created_at,user.name,resource.name,resource.resource_type,resource.resource_subtype,'
  + 'parent.name,parent.resource_type,change.field,change.action,change.new_value,change.added_value,change.removed_value';
/** The five Asana Events API actions of a task. */
export const KnownTaskChangeActions: ReadonlySet<string> = new Set(['added', 'changed', 'removed', 'deleted', 'undeleted']);
const gidPattern = /^[0-9]{1,64}$/;
const rfc3339Pattern = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$/;
// Go's unicode.IsSpace set, which strings.TrimSpace removes.
const goSpace = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g;

/** A failed Events API poll; the kit backs off and retries without moving the cursor. */
export class TaskEventStreamError extends Error {
  /** Describe the failure without provider text. */
  constructor(message: string) { super(message); this.name = 'TaskEventStreamError'; }
}

/** Report whether value is an Asana gid: 1 to 64 ASCII digits. */
export function isAsanaGID(value: string): boolean { return gidPattern.test(value); }

interface EventListing { events: readonly RawJSONValue[]; syncToken: string; hasMore: boolean; isSyncInvalid: boolean }

/** The ChangeFetcher of one taskChanged binding: one project's event stream, as the Go fetcher reads it. */
export class TaskEventFetcher implements ChangeFetcher<TriggerEvent<TaskChangedEvent>> {
  /** Bind the client and the watched project gid; it sends nothing. */
  constructor(private readonly client: Client, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, private readonly projectId: string) {}

  /** Ask for the events without a sync token; Asana's 412 carries a fresh token, the baseline. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const listing = await this.listEvents(context, '');
    if (!listing.syncToken) throw new TaskEventStreamError('Asana event stream returned no sync token');
    return this.encodeCursor(listing.syncToken);
  }

  /**
   * Read the events after the cursor's sync token. An expired token (HTTP 412), an undecodable cursor, or a
   * cursor for another project is marked invalid, so the kit records a new baseline.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<TaskChangedEvent>>> {
    let cursorProjectId = '', syncToken = '';
    try {
      const position = parseRawJSON(Buffer.from(cursor.token, 'utf8'));
      if (position.kind !== 'object' && position.kind !== 'null') throw new InvalidEventJSONError('cursor must be an object');
      if (position.kind === 'object') { cursorProjectId = position.stringMember('projectId'); syncToken = position.stringMember('sync'); }
    } catch (error) {
      if (!(error instanceof InvalidEventJSONError)) throw error;
      syncToken = '';
    }
    if (!syncToken) throw markCursorInvalid(new TaskEventStreamError('Asana event cursor is not decodable'));
    if (cursorProjectId !== this.projectId) throw markCursorInvalid(new TaskEventStreamError('Asana event cursor belongs to another project'));
    const listing = await this.listEvents(context, syncToken);
    if (listing.isSyncInvalid) throw markCursorInvalid(new TaskEventStreamError('Asana sync token expired or is invalid (HTTP 412)'));
    if (!listing.syncToken) throw new TaskEventStreamError('Asana event page has no sync token');
    if (listing.events.length > MaximumEventsPerPage) throw new TaskEventStreamError(`Asana event page has ${listing.events.length} events, more than ${MaximumEventsPerPage}`);
    return { changes: this.convertEvents(syncToken, listing.events), next: this.encodeCursor(listing.syncToken), hasMorePages: listing.hasMore };
  }

  /**
   * Keep task events in Asana's order, each with a stable ID. Asana returns the same events for a
   * re-requested sync token, so the requested token plus an ordinal among identical events of the page keeps
   * IDs stable across refetches, and identical events on either side of a page boundary stay distinct.
   */
  convertEvents(requestedSyncToken: string, events: readonly RawJSONValue[]): Array<TriggerEvent<TaskChangedEvent>> {
    const now = this.client.currentTime();
    const occurrences = new Map<string, number>();
    const changes: Array<TriggerEvent<TaskChangedEvent>> = [];
    for (const event of events) {
      const resource = event.kind === 'object' ? event.optionalObject('resource') : undefined;
      if (!resource || resource.stringMember('resource_type') !== taskEventResourceType) continue;
      let payload: TaskChangedEvent;
      try { payload = convertTaskEvent(this.projectId, event, resource); }
      catch (error) { throw new TaskEventStreamError(`Asana event page has an invalid task event: ${error instanceof Error ? error.message : String(error)}`); }
      let identity = Buffer.concat([Buffer.from(requestedSyncToken, 'utf8'), Buffer.from([0]), taskEventIdentity(this.projectId, event)]);
      const identityKey = identity.toString('latin1');
      const occurrence = (occurrences.get(identityKey) ?? 0) + 1;
      occurrences.set(identityKey, occurrence);
      if (occurrence > 1) identity = Buffer.concat([identity, Buffer.from(`\u0000${occurrence}`, 'utf8')]);
      const digest = createHash('sha256').update(identity).digest();
      changes.push({ id: `asana-${digest.subarray(0, 16).toString('hex')}`, occurredAt: parseEventTime(payload.occurredAt, now), payload });
    }
    return changes;
  }

  private async listEvents(context: TriggerContext, syncToken: string): Promise<EventListing> {
    await waitForPollRequest(context);
    const call: Call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const token = await this.client.token(call);
    if (token === undefined) throw new TaskEventStreamError('Asana Trigger credentials are unavailable; save a personal access token for the connection');
    const query = new URLSearchParams({ resource: this.projectId, opt_fields: TaskEventFields });
    if (syncToken) query.set('sync', syncToken);
    return this.classifyEventListing(await this.client.exchange(call, token, 'GET', '/events', query, undefined, context.signal));
  }

  private classifyEventListing(exchange: Exchange): EventListing {
    if (!('response' in exchange)) {
      throw new TaskEventStreamError(exchange.error === 'interrupted' ? 'Asana event page response was interrupted' : 'Asana event stream request failed before a response');
    }
    const { response } = exchange;
    if (response.status === 412) {
      const listing = exchange.reflectsSecret ? undefined : decodeEventPage(response.body);
      if (!listing?.syncToken) throw new TaskEventStreamError('Asana answered HTTP 412 without a fresh sync token');
      return { events: [], syncToken: listing.syncToken, hasMore: false, isSyncInvalid: true };
    }
    // Asana's limits are per user and per domain; the kit pauses every binding of the connection.
    if (response.status === 429) throw markRateLimited(new TaskEventStreamError('Asana rate limited the event stream request (HTTP 429)'), this.client.retryAfter(exchange));
    if (response.status < 200 || response.status >= 300) throw new TaskEventStreamError(`Asana rejected the event stream request with HTTP ${response.status}`);
    if (exchange.isTooLarge) throw new TaskEventStreamError('Asana event page exceeds the configured maxResponseBytes');
    if (exchange.reflectsSecret) throw new TaskEventStreamError('Asana event page contains credential material');
    const listing = decodeEventPage(response.body);
    if (!listing) throw new TaskEventStreamError('Asana event page is not valid JSON');
    return listing;
  }

  private encodeCursor(syncToken: string): Cursor {
    return { token: encodeGoJSONValue({ projectId: this.projectId, sync: syncToken }) };
  }
}

/** Decode GET /events as Go's json.Unmarshal does, or undefined where Go would fail. */
function decodeEventPage(body: Uint8Array): EventListing | undefined {
  try {
    const page = parseRawJSON(body);
    if (page.kind === 'null') return { events: [], syncToken: '', hasMore: false, isSyncInvalid: false };
    if (page.kind !== 'object') return undefined;
    const events = page.arrayMember('data');
    for (const event of events) validateEventTypes(event);
    return { events, syncToken: page.stringMember('sync'), hasMore: page.booleanMember('has_more'), isSyncInvalid: false };
  } catch (error) {
    if (error instanceof InvalidEventJSONError) return undefined;
    throw error;
  }
}

/** Throw InvalidEventJSONError where Go's decoding into asanaEvent reports a type mismatch. */
function validateEventTypes(event: RawJSONValue): void {
  if (event.kind === 'null') return;
  if (event.kind !== 'object') throw new InvalidEventJSONError('event must be an object');
  event.stringMember('action');
  event.stringMember('created_at');
  const nestedFields: Array<[string, string[]]> = [
    ['user', ['gid', 'name']],
    ['resource', ['gid', 'name', 'resource_type', 'resource_subtype']],
    ['parent', ['gid', 'name', 'resource_type', 'resource_subtype']],
    ['change', ['field', 'action']],
  ];
  for (const [name, fields] of nestedFields) {
    const nested = event.optionalObject(name);
    if (nested) for (const field of fields) nested.stringMember(field);
  }
}

function convertTaskEvent(projectId: string, event: RawJSONValue, resource: RawJSONValue): TaskChangedEvent {
  const action = event.stringMember('action');
  if (!isAsanaGID(resource.stringMember('gid'))) throw new Error('task gid is not an Asana gid');
  if (!KnownTaskChangeActions.has(action)) throw new Error(`unknown action ${encodeGoJSONValue(action)}`);
  const change = event.optionalObject('change');
  const parent = event.optionalObject('parent');
  const user = event.optionalObject('user');
  const taskName = resource.stringMember('name');
  const taskSubtype = resource.stringMember('resource_subtype');
  const payload: TaskChangedEvent = {
    projectId, action, task: { id: resource.stringMember('gid'), ...(taskName ? { name: taskName } : {}) },
    ...(taskSubtype ? { taskSubtype } : {}), occurredAt: event.stringMember('created_at'),
  };
  if (change && action === 'changed') {
    const changedField = change.stringMember('field'), changeAction = change.stringMember('action');
    if (changedField) payload.changedField = changedField;
    if (changeAction) payload.changeAction = changeAction;
  }
  if (parent && isAsanaGID(parent.stringMember('gid'))) {
    const parentName = parent.stringMember('name');
    const reference: EventParentReference = { id: parent.stringMember('gid'), resourceType: parent.stringMember('resource_type'), ...(parentName ? { name: parentName } : {}) };
    payload.parent = reference;
  }
  if (user && isAsanaGID(user.stringMember('gid'))) {
    const userName = user.stringMember('name');
    payload.user = { id: user.stringMember('gid'), ...(userName ? { name: userName } : {}) };
  }
  return payload;
}

/** Join the event's immutable facts; names are left out so a rename keeps the ID. */
function taskEventIdentity(projectId: string, event: RawJSONValue): Buffer {
  const text = (value: string) => Buffer.from(value, 'utf8');
  const user = event.optionalObject('user'), parent = event.optionalObject('parent'), change = event.optionalObject('change');
  const parts: Buffer[] = [
    text(projectId), text(event.optionalObject('resource')?.stringMember('gid') ?? ''), text(event.stringMember('action')),
    text(event.stringMember('created_at')), text(user?.stringMember('gid') ?? ''), text(parent?.stringMember('gid') ?? ''),
  ];
  if (change) {
    parts.push(text(change.stringMember('field')), text(change.stringMember('action')),
      changeValueIdentity(change.member('new_value')), changeValueIdentity(change.member('added_value')), changeValueIdentity(change.member('removed_value')));
  }
  return Buffer.concat(parts.flatMap((part, index) => index === 0 ? [part] : [Buffer.from([0]), part]));
}

/** A referenced object's gid, or the compacted JSON of any other value, as Go computes it. */
function changeValueIdentity(value: RawJSONValue | undefined): Buffer {
  if (!value) return Buffer.alloc(0);
  if (value.kind === 'object') {
    const reference = value.member('gid');
    if (reference?.kind === 'string' && reference.text) return Buffer.from(`gid:${reference.text}`, 'utf8');
  }
  return Buffer.from(compactRawJSON(value.raw));
}

/** Read Asana's created_at as RFC 3339, falling back to now for a missing or unparsable time. */
function parseEventTime(value: string, now: Date): Date {
  const match = rfc3339Pattern.exec(value.replace(goSpace, ''));
  if (match) {
    const [, year, month, day, hour, minute, second, fraction, offset] = match;
    const calendarDay = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day)));
    const isValid = calendarDay.getUTCMonth() === Number(month) - 1 && calendarDay.getUTCDate() === Number(day)
      && Number(hour) < 24 && Number(minute) < 60 && Number(second) < 60 && (offset === 'Z' || Number(offset!.slice(1, 3)) < 24 && Number(offset!.slice(4)) < 60);
    const parsed = new Date(`${year}-${month}-${day}T${hour}:${minute}:${second}${(fraction ?? '').slice(0, 4)}${offset}`);
    if (isValid && !Number.isNaN(parsed.getTime())) return parsed;
  }
  return now;
}
