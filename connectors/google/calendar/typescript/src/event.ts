// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Event, EventDateTime, EventAttendeeInput, EventAttendee } from './generated/models.js';

function record(value: unknown, name: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name} is invalid`);
  return value as Record<string, unknown>;
}

function field(value: Record<string, unknown>, name: string): string {
  if (value[name] !== undefined && value[name] !== null && typeof value[name] !== 'string') throw new Error(`${name} is invalid`);
  return (value[name] as string | undefined) ?? '';
}

function boolean(value: Record<string, unknown>, name: string): boolean {
  if (value[name] !== undefined && value[name] !== null && typeof value[name] !== 'boolean') throw new Error(`${name} is invalid`);
  return (value[name] as boolean | undefined) ?? false;
}

/** Parse a Go-compatible RFC 3339 instant with an explicit offset. */
export function parseInstant(value: string, name: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new Error(`${name} ${JSON.stringify(value)} must be RFC 3339 with an explicit offset, such as 2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z`);
  const year = match[1]!, month = match[2]!, day = match[3]!, hour = match[4]!, minute = match[5]!, second = match[6]!, fraction = match[7], offset = match[8]!;
  const y = Number(year), m = Number(month), d = Number(day), h = Number(hour), min = Number(minute), s = Number(second);
  const calendar = new Date(0);
  calendar.setUTCFullYear(y, m - 1, d);
  calendar.setUTCHours(0, 0, 0, 0);
  if (calendar.getUTCFullYear() !== y || calendar.getUTCMonth() !== m - 1 || calendar.getUTCDate() !== d || h > 23 || min > 59 || s > 59 || (offset !== 'Z' && Number(offset.slice(4)) > 59)) throw new Error(`${name} ${JSON.stringify(value)} must be RFC 3339 with an explicit offset, such as 2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z`);
  const offsetMinutes = offset === 'Z' ? 0 : (offset[0] === '+' ? 1 : -1) * (Number(offset.slice(1, 3)) * 60 + Number(offset.slice(4)));
  return calendar.getTime() + (h * 3600 + min * 60 + s) * 1000 + (fraction ? Number(`0.${fraction}`) * 1000 : 0) - offsetMinutes * 60_000;
}

/** Validate a calendar date, including leap days. */
export function parseDate(value: string, name: string): number {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error(`${name} ${JSON.stringify(value)} must use YYYY-MM-DD`);
  const [y, m, d] = value.split('-').map(Number) as [number, number, number];
  const result = new Date(0);
  result.setUTCFullYear(y, m - 1, d);
  result.setUTCHours(0, 0, 0, 0);
  if (result.getUTCFullYear() !== y || result.getUTCMonth() !== m - 1 || result.getUTCDate() !== d) throw new Error(`${name} ${JSON.stringify(value)} must use YYYY-MM-DD`);
  return result.getTime();
}

/** Check an IANA zone accepted by the host's zone database. */
export function validateTimeZone(value: string, name: string): void {
  if (!value || value.trim() !== value || value === 'Local') throw new Error(`${name} ${JSON.stringify(value)} must be an IANA time zone name such as America/Los_Angeles`);
  try { new Intl.DateTimeFormat('en-US', { timeZone: value }); }
  catch { throw new Error(`${name} ${JSON.stringify(value)} is not a known IANA time zone name`); }
}

/** Validate a bounded query window. */
export function validateWindow(min: string, max: string): void {
  if (!min || !max) throw new Error('timeMin and timeMax are both required');
  const start = parseInstant(min, 'timeMin'), end = parseInstant(max, 'timeMax');
  if (end <= start) throw new Error(`timeMax ${max} must be after timeMin ${min}`);
  if (end - start > 366 * 86_400_000) throw new Error('the timeMin to timeMax window cannot exceed 366 days');
}

/** Check one create or update boundary. */
export function validateBoundary(boundary: EventDateTime, name: string, returned = false): void {
  if ((!!boundary.date) === (!!boundary.dateTime)) throw new Error(returned ? `${name} must set exactly one of date and dateTime` : `${name} must set date for an all-day event or dateTime for a timed event`);
  if (boundary.date) {
    parseDate(boundary.date, `${name}.date`);
    if (!returned && boundary.timeZone) throw new Error(`${name} is an all-day date, which has no time zone`);
  } else {
    parseInstant(boundary.dateTime!, `${name}.dateTime`);
    if (!returned) {
      if (!boundary.timeZone) throw new Error(`${name}.timeZone is required for a timed event`);
      validateTimeZone(boundary.timeZone, `${name}.timeZone`);
    }
  }
}

/** Validate create and update event times. */
export function validateEventTimes(start: EventDateTime, end: EventDateTime): void {
  validateBoundary(start, 'start'); validateBoundary(end, 'end');
  if (!!start.date !== !!end.date) throw new Error('start and end must both be all-day dates or both be timed dateTime values');
  const startValue = start.date ? parseDate(start.date, 'start.date') : parseInstant(start.dateTime!, 'start.dateTime');
  const endValue = end.date ? parseDate(end.date, 'end.date') : parseInstant(end.dateTime!, 'end.dateTime');
  if (endValue <= startValue) throw new Error(start.date ? `all-day end date ${end.date} is exclusive and must follow start date ${start.date}` : `end ${end.dateTime} must be after start ${start.dateTime}`);
}

/** Convert one provider event to the exact public shape. */
export function decodeEvent(value: unknown, calendarId: string): Event {
  const source = record(value, 'event response');
  const id = field(source, 'id');
  if (!id) throw new Error('event response has no ID');
  const start = source.start == null ? {} as EventDateTime : decodeBoundary(source.start);
  const end = source.end == null ? {} as EventDateTime : decodeBoundary(source.end);
  const status = field(source, 'status');
  if (status !== 'cancelled' || source.start != null || source.end != null) {
    validateBoundary(start, 'start', true); validateBoundary(end, 'end', true);
    if (!!start.date !== !!end.date) throw new Error('start and end mix an all-day date with a timed dateTime');
  }
  const attendees = source.attendees == null ? [] : source.attendees;
  if (!Array.isArray(attendees)) throw new Error('attendees is invalid');
  const event: Event = {
    id, calendarId, status, start, end, isAllDay: !!start.date,
    transparency: field(source, 'transparency') || 'opaque',
    ...(field(source, 'summary') ? { summary: field(source, 'summary') } : {}),
    ...(field(source, 'description') ? { description: field(source, 'description') } : {}),
    ...(field(source, 'location') ? { location: field(source, 'location') } : {}),
    ...(field(source, 'visibility') ? { visibility: field(source, 'visibility') } : {}),
    ...(attendees.length ? { attendees: attendees.map(decodeAttendee) } : {}),
    ...(source.organizer != null && field(record(source.organizer, 'organizer'), 'email') ? { organizerEmail: field(record(source.organizer, 'organizer'), 'email') } : {}),
    ...(field(source, 'recurringEventId') ? { recurringEventId: field(source, 'recurringEventId') } : {}),
    ...(source.originalStartTime != null ? { originalStartTime: decodeBoundary(source.originalStartTime) } : {}),
    ...(source.conferenceData != null ? { conference: decodeConference(source.conferenceData) } : {}),
    ...(field(source, 'htmlLink') ? { htmlLink: field(source, 'htmlLink') } : {}),
    ...(field(source, 'iCalUID') ? { iCalUID: field(source, 'iCalUID') } : {}),
    ...(field(source, 'etag') ? { etag: field(source, 'etag') } : {}),
    ...(field(source, 'created') ? { created: field(source, 'created') } : {}),
    ...(field(source, 'updated') ? { updated: field(source, 'updated') } : {}),
  };
  return event;
}

function decodeBoundary(value: unknown): EventDateTime {
  const source = record(value, 'event boundary');
  const date = field(source, 'date'), dateTime = field(source, 'dateTime'), timeZone = field(source, 'timeZone');
  return { ...(date ? { date } : {}), ...(dateTime ? { dateTime } : {}), ...(timeZone ? { timeZone } : {}) };
}

function decodeAttendee(value: unknown): EventAttendee {
  const source = record(value, 'attendee');
  const email = field(source, 'email'), displayName = field(source, 'displayName'), responseStatus = field(source, 'responseStatus');
  return { email, isOptional: boolean(source, 'optional'), isOrganizer: boolean(source, 'organizer'), isSelf: boolean(source, 'self'), isResource: boolean(source, 'resource'), ...(displayName ? { displayName } : {}), ...(responseStatus ? { responseStatus } : {}) };
}

function decodeConference(value: unknown) {
  const source = record(value, 'conferenceData');
  const solution = source.conferenceSolution == null ? undefined : record(source.conferenceSolution, 'conferenceSolution');
  const key = solution?.key == null ? undefined : record(solution.key, 'conference key');
  const create = source.createRequest == null ? undefined : record(source.createRequest, 'createRequest');
  const createStatus = create?.status == null ? undefined : record(create.status, 'createRequest status');
  const points = source.entryPoints == null ? [] : source.entryPoints;
  if (!Array.isArray(points)) throw new Error('entryPoints is invalid');
  const video = points.map(point => record(point, 'entryPoint')).find(point => field(point, 'entryPointType') === 'video');
  const solutionType = key ? field(key, 'type') : '', conferenceId = field(source, 'conferenceId');
  const joinUrl = video ? field(video, 'uri') : '', createRequestStatus = createStatus ? field(createStatus, 'statusCode') : '';
  return { ...(solutionType ? { solutionType } : {}), ...(conferenceId ? { conferenceId } : {}), ...(joinUrl ? { joinUrl } : {}), ...(createRequestStatus ? { createRequestStatus } : {}) };
}

/** Encode only the attendee fields sent by Go. */
export function encodeAttendees(attendees: EventAttendeeInput[]): Record<string, unknown>[] {
  return attendees.map(attendee => ({ email: attendee.email, ...(attendee.isOptional ? { optional: true } : {}) }));
}

/** Compare attendee sets without depending on their order. */
export function sameAttendees(current: EventAttendee[], requested: EventAttendeeInput[]): boolean {
  if (current.length !== requested.length) return false;
  const options = new Map(requested.map(attendee => [attendee.email.toLowerCase(), !!attendee.isOptional]));
  return current.every(attendee => options.has(attendee.email.toLowerCase()) && options.get(attendee.email.toLowerCase()) === attendee.isOptional);
}

/** Emit explicit nulls to clear the opposite boundary form during a patch. */
export function encodeBoundary(boundary: EventDateTime): Record<string, unknown> {
  return { date: boundary.date || null, dateTime: boundary.dateTime || null, timeZone: boundary.date ? null : boundary.timeZone || null };
}

/** Send only nonempty EventDateTime fields for inserts. */
export function encodeCreateBoundary(boundary: EventDateTime): Record<string, string> {
  if (boundary.date) return { date: boundary.date };
  return { dateTime: boundary.dateTime!, timeZone: boundary.timeZone! };
}
