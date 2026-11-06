// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CreateEventDefinition, GetEventDefinition, ListEventsDefinition, QueryFreeBusyDefinition, UpdateEventDefinition } from './generated/definitions.js';
import type { BusyInterval, CalendarFreeBusy, CreateEventInput, CreateEventOutput, Event, EventDateTime, GetEventInput, ListEventsInput, ListEventsOutput, QueryFreeBusyInput, QueryFreeBusyOutput, UpdateEventInput, UpdateEventOutput } from './generated/models.js';
import { CalendarClient, header, pathEscape, type CalendarResponse } from './client.js';
import { decodeEvent, encodeAttendees, encodeBoundary, encodeCreateBoundary, parseInstant, sameAttendees, validateEventTimes, validateTimeZone, validateWindow } from './event.js';

const zeroEvent: Event = { id: '', calendarId: '', status: '', start: {}, end: {}, isAllDay: false, transparency: '' };
const zeroList: ListEventsOutput = { calendarId: '', timeMin: '', timeMax: '', timeZone: '', events: [] };
const zeroCreate: CreateEventOutput = { event: zeroEvent, wasAlreadyCreated: false };
const zeroUpdate: UpdateEventOutput = { event: zeroEvent, wasAlreadyApplied: false };
const zeroBusy: QueryFreeBusyOutput = { timeMin: '', timeMax: '', timeZone: '', calendars: [] };

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'google-calendar', operation, message }; }
function message(error: unknown): string { return error instanceof Error ? error.message : 'invalid provider response'; }
function jsonBody(response: CalendarResponse): unknown { return JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('response is not an object'); return value as Record<string, unknown>; }
function stringField(value: Record<string, unknown>, key: string): string { if (value[key] !== undefined && value[key] !== null && typeof value[key] !== 'string') throw new Error(`${key} is invalid`); return (value[key] as string | undefined) ?? ''; }
function requireCalendarID(value: string): void { if (!value || value.trim() !== value) throw new Error('calendarId is required, such as primary or a calendar ID from the calendar picker'); }
function requireEventID(value: string): void { if (!value || value.trim() !== value) throw new Error('eventId is required'); }
function requireSendUpdates(value: string | undefined): void { if (value && !['none', 'all', 'externalOnly'].includes(value)) throw new Error(`sendUpdates ${JSON.stringify(value)} must be blank, none, all, or externalOnly`); }
function eventPath(calendarId: string, eventId: string): string { return `/calendars/${pathEscape(calendarId)}/events/${pathEscape(eventId)}`; }
function calendarEventsPath(calendarId: string): string { return `/calendars/${pathEscape(calendarId)}/events`; }

function reason(response: CalendarResponse): string {
  try {
    const envelope = object(jsonBody(response));
    const error = object(envelope.error);
    const details = error.errors;
    if (!Array.isArray(details)) return '';
    const allowed = new Set(['badRequest', 'invalid', 'required', 'timeRangeEmpty', 'notFound', 'deleted', 'duplicate', 'conditionNotMet', 'forbidden', 'forbiddenForNonOrganizer', 'requiredAccessLevel', 'insufficientPermissions', 'quotaExceeded', 'dailyLimitExceeded', 'rateLimitExceeded', 'userRateLimitExceeded', 'backendError', 'invalidAttendeeEmail', 'cannotChangeOrganizer', 'eventTypeRestriction', 'authError']);
    for (const entry of details) { const code = stringField(object(entry), 'reason'); if (allowed.has(code)) return code; }
  } catch { /* Malformed Google error bodies have no safe reason. */ }
  return '';
}

function classify(operation: string, response: CalendarResponse): { retry: boolean; delay: number; failure: Failure } {
  const code = reason(response);
  const describe = (text: string) => text + (code ? ` (${code})` : '');
  const isRate = response.status === 429 || (response.status === 403 && ['rateLimitExceeded', 'userRateLimitExceeded'].includes(code));
  const isUnavailable = response.status === 408 || response.status >= 500;
  if (isRate || isUnavailable) {
    const raw = header(response, 'retry-after');
    if (raw && (!/^[+-]?\d+$/.test(raw) || Number(raw) < 0 || !Number.isSafeInteger(Number(raw)))) return { retry: true, delay: 0, failure: failure(operation, 'PROTOCOL', 'provider returned an invalid Retry-After header') };
    return { retry: true, delay: raw ? Number(raw) * 1000 : 0, failure: failure(operation, isRate ? 'RATE_LIMIT' : 'AVAILABILITY', describe(isRate ? 'provider rate limited the request' : 'provider is temporarily unavailable')) };
  }
  if (['quotaExceeded', 'dailyLimitExceeded'].includes(code)) return { retry: false, delay: 0, failure: failure(operation, 'QUOTA_EXHAUSTED', describe('provider quota is exhausted')) };
  const kinds: Record<number, Failure['kind']> = { 400: 'VALIDATION', 401: 'AUTHENTICATION', 403: 'AUTHORIZATION', 404: 'NOT_FOUND', 410: 'NOT_FOUND', 409: 'CONFLICT', 412: 'CONFLICT' };
  return { retry: false, delay: 0, failure: failure(operation, kinds[response.status] ?? 'PROVIDER_REJECTION', describe(`provider rejected the request with HTTP ${response.status}`)) };
}

type ReadOutcome = { kind: 'found' | 'missing' | 'retry' | 'rejected' | 'invalid' | 'defect'; event: Event; response?: CalendarResponse; failure?: Failure; delay: number };
async function readEvent(client: CalendarClient, call: Call, token: string, operation: string, calendarId: string, eventId: string, timeZone = ''): Promise<ReadOutcome> {
  let response: CalendarResponse;
  try { response = await client.request(call, token, 'GET', eventPath(calendarId, eventId), timeZone ? { timeZone } : undefined); }
  catch { return { kind: 'retry', event: zeroEvent, failure: failure(operation, 'AVAILABILITY', 'provider is unavailable'), delay: 0 }; }
  if (response.isTooLarge) return { kind: 'invalid', event: zeroEvent, response, failure: failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'), delay: 0 };
  if (response.status === 404 || response.status === 410) return { kind: 'missing', event: zeroEvent, response, failure: failure(operation, 'NOT_FOUND', 'calendar or event was not found'), delay: 0 };
  if (response.status < 200 || response.status >= 300) {
    const result = classify(operation, response);
    return { kind: result.retry ? 'retry' : 'rejected', event: zeroEvent, response, failure: result.failure, delay: result.delay };
  }
  try { return { kind: 'found', event: decodeEvent(jsonBody(response), calendarId), response, delay: 0 }; }
  catch (error) { return { kind: 'invalid', event: zeroEvent, response, failure: failure(operation, 'PROTOCOL', `provider returned an invalid event: ${message(error)}`), delay: 0 }; }
}

/** Query one bounded page of Calendar events. */
export class ListEventsOperation implements Query<ListEventsInput, ListEventsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CalendarClient) {}
  /** Return the manifest definition. */ definition() { return ListEventsDefinition; }
  /** List one page and classify the provider result. */
  async invoke(call: Call, input: ListEventsInput): Promise<QueryAttempt<ListEventsOutput>> {
    const operation = 'listEvents';
    const pageSize = input.pageSize || 50;
    try { requireCalendarID(input.calendarId); validateWindow(input.timeMin, input.timeMax); if (input.timeZone) validateTimeZone(input.timeZone, 'timeZone'); if (pageSize < 1 || pageSize > 250 || !Number.isInteger(pageSize)) throw new Error('pageSize must be between 1 and 250, or zero for the default'); }
    catch (error) { return newQueryBranch('defect', zeroList, zeroReceipt(), failure(operation, 'VALIDATION', message(error))); }
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newQueryBranch('defect', zeroList, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const query: Record<string, string> = { singleEvents: 'true', orderBy: 'startTime', showDeleted: 'false', timeMin: input.timeMin, timeMax: input.timeMax, maxResults: String(pageSize) };
    if (input.query) query.q = input.query;
    if (input.pageToken) query.pageToken = input.pageToken;
    if (input.timeZone) query.timeZone = input.timeZone;
    let response: CalendarResponse;
    try { response = await this.client.request(call, token, 'GET', calendarEventsPath(input.calendarId), query); }
    catch { return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is unavailable')); }
    const receipt = this.client.receipt(call, response);
    if (response.isTooLarge) return newQueryBranch('invalidResponse', zeroList, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (response.status === 404 || response.status === 410) return newQueryBranch('notFound', zeroList, receipt, failure(operation, 'NOT_FOUND', 'calendar was not found'));
    if (response.status < 200 || response.status >= 300) { const result = classify(operation, response); return result.retry ? newQueryRetry(result.failure, result.delay) : newQueryBranch('providerRejected', zeroList, receipt, result.failure); }
    try {
      const page = object(jsonBody(response));
      const timeZone = stringField(page, 'timeZone');
      if (timeZone) validateTimeZone(timeZone, 'timeZone');
      if (page.items != null && !Array.isArray(page.items)) throw new Error('items is invalid');
      const events = ((page.items as unknown[] | undefined) ?? []).map(item => decodeEvent(item, input.calendarId)).filter(event => event.status !== 'cancelled');
      const nextPageToken = stringField(page, 'nextPageToken');
      return newQueryBranch('listed', { calendarId: input.calendarId, timeMin: input.timeMin, timeMax: input.timeMax, timeZone, events, ...(nextPageToken ? { nextPageToken } : {}) }, receipt);
    } catch (error) { return newQueryBranch('invalidResponse', zeroList, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid event page: ${message(error)}`)); }
  }
}

/** Query one Google Calendar event. */
export class GetEventOperation implements Query<GetEventInput, Event> {
  /** Bind the provider client. */ constructor(private readonly client: CalendarClient) {}
  /** Return the manifest definition. */ definition() { return GetEventDefinition; }
  /** Read an event, including cancelled events. */
  async invoke(call: Call, input: GetEventInput): Promise<QueryAttempt<Event>> {
    const operation = 'getEvent';
    try { requireCalendarID(input.calendarId); requireEventID(input.eventId); if (input.timeZone) validateTimeZone(input.timeZone, 'timeZone'); }
    catch (error) { return newQueryBranch('defect', zeroEvent, zeroReceipt(), failure(operation, 'VALIDATION', message(error))); }
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newQueryBranch('defect', zeroEvent, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const read = await readEvent(this.client, call, token, operation, input.calendarId, input.eventId, input.timeZone);
    if (read.kind === 'retry') return newQueryRetry(read.failure!, read.delay);
    if (read.kind === 'defect') return newQueryBranch('defect', zeroEvent, zeroReceipt(), read.failure);
    const receipt = this.client.receipt(call, read.response, read.event.id);
    if (read.kind === 'found') return newQueryBranch('found', read.event, receipt);
    const branch = read.kind === 'missing' ? 'notFound' : read.kind === 'invalid' ? 'invalidResponse' : 'providerRejected';
    return newQueryBranch(branch, zeroEvent, receipt, read.failure);
  }
}

/** Insert under a stable event ID and reconcile duplicate writes. */
export class CreateEventOperation implements Mutation<CreateEventInput, CreateEventOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CalendarClient) {}
  /** Return the manifest definition. */ definition() { return CreateEventDefinition; }
  /** Use the supplied event ID or the dashless Call ID. */ idempotencyKey(callId: string, input: CreateEventInput): string { return input.eventId || callId.replace(/-/g, ''); }
  /** Read, insert, and read back an event under its stable ID. */
  async invoke(call: Call, input: CreateEventInput): Promise<MutationAttempt<CreateEventOutput>> {
    const operation = 'createEvent', eventId = call.idempotencyKey || this.idempotencyKey(call.id, input);
    try { requireCalendarID(input.calendarId); if (eventId.length < 5 || eventId.length > 1024 || !/^[a-v0-9]+$/.test(eventId)) throw new Error('eventId must be 5 to 1024 lowercase base32hex characters (a-v and 0-9)'); if (!input.summary?.trim()) throw new Error('summary is required'); validateEventTimes(input.start, input.end); validateAttendees(input.attendees ?? []); requireSendUpdates(input.sendUpdates); }
    catch (error) { return newMutationBranch('defect', zeroCreate, zeroReceipt(), failure(operation, 'VALIDATION', message(error))); }
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newMutationBranch('defect', zeroCreate, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const existing = await readEvent(this.client, call, token, operation, input.calendarId, eventId);
    if (existing.kind !== 'missing') return this.existing(call, existing, eventId);
    const query: Record<string, string> = {};
    if (input.requestConference) query.conferenceDataVersion = '1';
    if (input.sendUpdates) query.sendUpdates = input.sendUpdates;
    const payload: Record<string, unknown> = { id: eventId, summary: input.summary, start: encodeCreateBoundary(input.start), end: encodeCreateBoundary(input.end) };
    if (input.description) payload.description = input.description;
    if (input.attendees?.length) payload.attendees = encodeAttendees(input.attendees);
    if (input.requestConference) payload.conferenceData = { createRequest: { requestId: eventId, conferenceSolutionKey: { type: 'hangoutsMeet' } } };
    let response: CalendarResponse;
    try { response = await this.client.request(call, token, 'POST', calendarEventsPath(input.calendarId), query, payload); }
    catch { return newMutationRetry(failure(operation, 'TRANSPORT', 'insert outcome is unknown; the stable event ID makes the retry safe')); }
    const receipt = this.client.receipt(call, response, eventId);
    if (response.isTooLarge) return newMutationBranch('invalidResponse', zeroCreate, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (response.status === 409) return this.existing(call, await readEvent(this.client, call, token, operation, input.calendarId, eventId), eventId);
    if (response.status < 200 || response.status >= 300) { const result = classify(operation, response); return result.retry ? newMutationRetry(result.failure, result.delay) : newMutationBranch('providerRejected', zeroCreate, receipt, result.failure); }
    try { const event = decodeEvent(jsonBody(response), input.calendarId); if (event.id !== eventId) throw new Error('provider returned an event with another ID'); return newMutationBranch('created', { event, wasAlreadyCreated: false }, receipt); }
    catch (error) { return newMutationBranch('invalidResponse', zeroCreate, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid event: ${message(error)}`)); }
  }
  private existing(call: Call, read: ReadOutcome, eventId: string): MutationAttempt<CreateEventOutput> {
    const operation = 'createEvent';
    if (read.kind === 'retry') return newMutationRetry(read.failure!, read.delay);
    if (read.kind === 'defect') return newMutationBranch('defect', zeroCreate, zeroReceipt(), read.failure);
    const receipt = this.client.receipt(call, read.response, eventId);
    if (read.kind === 'found') return read.event.status === 'cancelled' ? newMutationBranch('conflict', { event: read.event, wasAlreadyCreated: false }, receipt, failure(operation, 'CONFLICT', 'event ID belongs to a deleted event')) : newMutationBranch('created', { event: read.event, wasAlreadyCreated: true }, receipt);
    if (read.kind === 'missing') return newMutationBranch('conflict', zeroCreate, receipt, failure(operation, 'CONFLICT', 'event ID is used by an event the connection cannot read'));
    return newMutationBranch(read.kind === 'invalid' ? 'invalidResponse' : 'providerRejected', zeroCreate, receipt, read.failure);
  }
}

/** Patch an event with an ETag after comparing the current fields. */
export class UpdateEventOperation implements Mutation<UpdateEventInput, UpdateEventOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CalendarClient) {}
  /** Return the manifest definition. */ definition() { return UpdateEventDefinition; }
  /** Updates deduplicate by reading current state before each patch. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, compare, and conditionally patch one event. */
  async invoke(call: Call, input: UpdateEventInput): Promise<MutationAttempt<UpdateEventOutput>> {
    const operation = 'updateEvent';
    try { requireCalendarID(input.calendarId); requireEventID(input.eventId); if (input.summary === undefined && input.description === undefined && input.start == null && input.end == null && input.attendees == null) throw new Error('set at least one of summary, description, start and end, or attendees'); if (input.summary !== undefined && !input.summary.trim()) throw new Error('summary cannot be blank'); if (!!input.start !== !!input.end) throw new Error('set start and end together so the event keeps one unambiguous time range'); if (input.start && input.end) validateEventTimes(input.start, input.end); if (input.attendees) validateAttendees(input.attendees); requireSendUpdates(input.sendUpdates); }
    catch (error) { return newMutationBranch('defect', zeroUpdate, zeroReceipt(), failure(operation, 'VALIDATION', message(error))); }
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newMutationBranch('defect', zeroUpdate, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const current = await readEvent(this.client, call, token, operation, input.calendarId, input.eventId);
    if (current.kind === 'retry') return newMutationRetry(current.failure!, current.delay);
    if (current.kind === 'defect') return newMutationBranch('defect', zeroUpdate, zeroReceipt(), current.failure);
    const readReceipt = this.client.receipt(call, current.response, input.eventId);
    if (current.kind !== 'found') return newMutationBranch(current.kind === 'missing' ? 'notFound' : current.kind === 'invalid' ? 'invalidResponse' : 'providerRejected', zeroUpdate, readReceipt, current.failure);
    if (current.event.status === 'cancelled') return newMutationBranch('notFound', zeroUpdate, readReceipt, failure(operation, 'NOT_FOUND', 'event is cancelled'));
    if (alreadyApplied(current.event, input)) return newMutationBranch('updated', { event: current.event, wasAlreadyApplied: true }, readReceipt);
    const payload: Record<string, unknown> = {};
    if (input.summary !== undefined) payload.summary = input.summary;
    if (input.description !== undefined) payload.description = input.description;
    if (input.start && input.end) { payload.start = encodeBoundary(input.start); payload.end = encodeBoundary(input.end); }
    if (input.attendees != null) payload.attendees = encodeAttendees(input.attendees);
    let response: CalendarResponse;
    try { response = await this.client.request(call, token, 'PATCH', eventPath(input.calendarId, input.eventId), input.sendUpdates ? { sendUpdates: input.sendUpdates } : undefined, payload, current.event.etag); }
    catch { return newMutationRetry(failure(operation, 'TRANSPORT', 'patch outcome is unknown; the retry rereads the event first')); }
    const receipt = this.client.receipt(call, response, input.eventId);
    if (response.isTooLarge) return newMutationBranch('invalidResponse', zeroUpdate, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (response.status === 412) return newMutationRetry(failure(operation, 'CONFLICT', 'event changed after it was read'));
    if (response.status === 404 || response.status === 410) return newMutationBranch('notFound', zeroUpdate, receipt, failure(operation, 'NOT_FOUND', 'calendar or event was not found'));
    if (response.status < 200 || response.status >= 300) { const result = classify(operation, response); return result.retry ? newMutationRetry(result.failure, result.delay) : newMutationBranch('providerRejected', zeroUpdate, receipt, result.failure); }
    try { return newMutationBranch('updated', { event: decodeEvent(jsonBody(response), input.calendarId), wasAlreadyApplied: false }, receipt); }
    catch (error) { return newMutationBranch('invalidResponse', zeroUpdate, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid event: ${message(error)}`)); }
  }
}

function alreadyApplied(current: Event, input: UpdateEventInput): boolean {
  if (input.summary !== undefined && current.summary !== input.summary) return false;
  if (input.description !== undefined && (current.description ?? '') !== input.description) return false;
  if (input.start && input.end && (!sameBoundary(current.start, input.start) || !sameBoundary(current.end, input.end))) return false;
  return input.attendees == null || sameAttendees(current.attendees ?? [], input.attendees);
}
function sameBoundary(current: EventDateTime, requested: EventDateTime): boolean { return current.date || requested.date ? current.date === requested.date && !!current.date === !!requested.date : current.timeZone === requested.timeZone && parseInstant(current.dateTime ?? '', 'dateTime') === parseInstant(requested.dateTime ?? '', 'dateTime'); }

/** Query bounded free/busy intervals for one or more calendars. */
export class QueryFreeBusyOperation implements Query<QueryFreeBusyInput, QueryFreeBusyOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CalendarClient) {}
  /** Return the manifest definition. */ definition() { return QueryFreeBusyDefinition; }
  /** Query availability while preserving requested calendar order. */
  async invoke(call: Call, input: QueryFreeBusyInput): Promise<QueryAttempt<QueryFreeBusyOutput>> {
    const operation = 'queryFreeBusy';
    try { if (!input.calendarIds || input.calendarIds.length < 1 || input.calendarIds.length > 50) throw new Error('calendarIds must list 1 to 50 calendars'); const seen = new Set<string>(); for (const id of input.calendarIds) { requireCalendarID(id); if (seen.has(id)) throw new Error(`calendar ID ${JSON.stringify(id)} is duplicated`); seen.add(id); } validateWindow(input.timeMin, input.timeMax); if (input.timeZone) validateTimeZone(input.timeZone, 'timeZone'); }
    catch (error) { return newQueryBranch('defect', zeroBusy, zeroReceipt(), failure(operation, 'VALIDATION', message(error))); }
    const token = await this.client.resolveAccessToken(call);
    if (!token) return newQueryBranch('defect', zeroBusy, zeroReceipt(), failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
    const payload = { timeMin: input.timeMin, timeMax: input.timeMax, items: input.calendarIds!.map(id => ({ id })), ...(input.timeZone ? { timeZone: input.timeZone } : {}) };
    let response: CalendarResponse;
    try { response = await this.client.request(call, token, 'POST', '/freeBusy', undefined, payload); }
    catch { return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is unavailable')); }
    const receipt = this.client.receipt(call, response);
    if (response.isTooLarge) return newQueryBranch('invalidResponse', zeroBusy, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured limit'));
    if (response.status < 200 || response.status >= 300) { const result = classify(operation, response); return result.retry ? newQueryRetry(result.failure, result.delay) : newQueryBranch('providerRejected', zeroBusy, receipt, result.failure); }
    try {
      const data = object(jsonBody(response));
      const providerCalendars = data.calendars == null ? {} : object(data.calendars);
      const calendars: CalendarFreeBusy[] = input.calendarIds!.map(calendarId => {
        const entry = providerCalendars[calendarId] ?? Object.entries(providerCalendars).find(([key]) => key.toLowerCase() === calendarId.toLowerCase())?.[1];
        if (entry === undefined) throw new Error(`calendar ${JSON.stringify(calendarId)} is missing from the response`);
        const availability = object(entry);
        const busyInput = availability.busy == null ? [] : availability.busy;
        if (!Array.isArray(busyInput)) throw new Error('busy is invalid');
        const busy: BusyInterval[] = busyInput.map(interval => { const value = object(interval); const start = stringField(value, 'start'), end = stringField(value, 'end'); if (parseInstant(end, 'busy.end') <= parseInstant(start, 'busy.start')) throw new Error(`calendar ${JSON.stringify(calendarId)} has a busy interval without two ordered RFC 3339 offsets`); return { start, end }; });
        const errorsInput = availability.errors == null ? [] : availability.errors;
        if (!Array.isArray(errorsInput)) throw new Error('errors is invalid');
        const errors = errorsInput.map(error => { const value = object(error); return { domain: stringField(value, 'domain'), reason: stringField(value, 'reason') }; });
        return { calendarId, busy, ...(errors.length ? { errors } : {}) };
      });
      const output = { timeMin: stringField(data, 'timeMin') || input.timeMin, timeMax: stringField(data, 'timeMax') || input.timeMax, timeZone: input.timeZone || 'UTC', calendars };
      return calendars.some(calendar => calendar.errors?.length) ? newQueryBranch('incomplete', output, receipt, failure(operation, 'PROVIDER_REJECTION', 'provider could not report availability for every calendar')) : newQueryBranch('queried', output, receipt);
    } catch (error) { return newQueryBranch('invalidResponse', zeroBusy, receipt, failure(operation, 'PROTOCOL', `provider returned an invalid free/busy response: ${message(error)}`)); }
  }
}

function validateAttendees(attendees: { email: string; isOptional?: boolean }[]): void {
  const seen = new Set<string>();
  for (const attendee of attendees) {
    const email = attendee.email;
    const atom = "[A-Za-z0-9!#$%&'*+/=?^_`{|}~\\-\\u0080-\\uFFFF]+";
    const segments = new RegExp(`^${atom}(?:\\.${atom})*$`, 'u');
    const pieces = email.split('@');
    const domain = pieces[1] ?? '';
    const isDomain = segments.test(domain) || /^\[[A-Za-z0-9:.-]+\]$/.test(domain);
    if (pieces.length !== 2 || !segments.test(pieces[0] ?? '') || !isDomain) throw new Error(`attendee email ${JSON.stringify(email)} must be one bare address such as person@example.com`);
    const canonical = email.toLowerCase();
    if (seen.has(canonical)) throw new Error(`attendee email ${JSON.stringify(email)} is duplicated`);
    seen.add(canonical);
  }
}
