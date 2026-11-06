// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { isIP } from 'node:net';
import type { CalendlyClient, CalendlyCredentialResult, CalendlyExchange } from './client.js';
import { failure } from './client.js';
import { ListScheduledEventsDefinition, GetScheduledEventDefinition, ListEventInviteesDefinition, CancelScheduledEventDefinition, CreateSchedulingLinkDefinition, CreateWebhookSubscriptionDefinition } from './generated/definitions.js';
import type { ListScheduledEventsInput, ScheduledEventPage, GetScheduledEventInput, ScheduledEvent, ListEventInviteesInput, EventInviteePage, Invitee, Cancellation, CancelScheduledEventInput, ScheduledEventCancellation, CreateSchedulingLinkInput, SchedulingLink, CreateWebhookSubscriptionInput, WebhookSubscription, Credentials } from './generated/models.js';

const base = 'https://api.calendly.com';
const resourceId = /^[A-Za-z0-9_-]{1,64}$/;
const pageTokenPattern = /^[A-Za-z0-9_\-.~=+/]{1,512}$/;
const zeroEvent = (): ScheduledEvent => ({ uri: '', status: '', startTime: '0001-01-01T00:00:00Z', endTime: '0001-01-01T00:00:00Z', location: {}, inviteesCounter: { total: 0, active: 0, limit: 0 } });
const zeroEventPage = (): ScheduledEventPage => ({ events: [], minStartTime: '0001-01-01T00:00:00Z', maxStartTime: '0001-01-01T00:00:00Z' });
const zeroInviteePage = (): EventInviteePage => ({ scheduledEventUri: '', invitees: [] });
const zeroCancellation = (uri: string): ScheduledEventCancellation => ({ scheduledEventUri: uri, alreadyCanceled: false, cancellation: {} });
const zeroLink = (uri: string): SchedulingLink => ({ bookingUrl: '', eventTypeUri: uri });
const zeroSubscription = (): WebhookSubscription => ({ uri: '', callbackUrl: '', state: '', events: [], scope: '', organizationUri: '', wasCreated: false });

function parseResourceURI(uri: string, collection: string, noun: string): string {
  const prefix = `${base}/${collection}/`;
  const id = uri.startsWith(prefix) ? uri.slice(prefix.length) : '';
  if (!resourceId.test(id)) throw new TypeError(`${noun} URI must look like ${prefix}<id>`);
  return id;
}

function validatePage(size = 0, token = ''): void {
  if (!Number.isInteger(size) || size < 0 || size > 100) throw new TypeError("pageSize must be from 1 through 100, or 0 for Calendly's default of 20");
  if (token && !pageTokenPattern.test(token)) throw new TypeError('pageToken must be the nextPageToken value of an earlier page');
}

function isPlainEmailAddress(value: string): boolean {
  if (!value || value.trim() !== value || value.includes('"') || value.includes('<') || value.includes('>')) return false;
  if (Array.from(value).some(character => character.codePointAt(0)! <= 32 || character.codePointAt(0) === 127)) return false;
  const at = value.indexOf('@');
  if (at <= 0 || at !== value.lastIndexOf('@')) return false;
  const local = value.slice(0, at), domain = value.slice(at + 1);
  const atom = /^[^()<>\x5B\x5D:;@\\,"]+$/u;
  if (!local.split('.').every(part => part && atom.test(part))) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return isIP(domain.slice(1, -1)) !== 0;
  return domain.split('.').every(part => part && atom.test(part));
}

function parseObject(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid Calendly response');
  return value as Record<string, unknown>;
}

function stringField(record: Record<string, unknown>, name: string): string {
  const value = record[name];
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid Calendly string field');
  return value;
}

function numberField(record: Record<string, unknown>, name: string): number {
  const value = record[name];
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new TypeError('invalid Calendly number field');
  return Math.trunc(value);
}

function integerField(record: Record<string, unknown>, name: string): number {
  const value = record[name];
  if (value === undefined || value === null) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('invalid Calendly integer field');
  return value;
}

function booleanField(record: Record<string, unknown>, name: string): boolean {
  const value = record[name];
  if (value === undefined || value === null) return false;
  if (typeof value !== 'boolean') throw new TypeError('invalid Calendly boolean field');
  return value;
}

function arrayField(record: Record<string, unknown>, name: string): unknown[] {
  const value = record[name];
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) throw new TypeError('invalid Calendly array field');
  return value;
}

function parseBody(exchange: CalendlyExchange): Record<string, unknown> {
  if (exchange.kind !== 'response') throw new TypeError('missing Calendly response');
  return parseObject(JSON.parse(Buffer.from(exchange.response.body).toString('utf8'), (key, value: unknown, context?: { source?: string }) => { if (key === 'position' && typeof value === 'number' && !/^-?\d+$/.test(context?.source ?? '')) throw new TypeError('invalid Calendly integer'); return value; }));
}

function responseResource(exchange: CalendlyExchange): Record<string, unknown> { return parseObject(parseBody(exchange).resource); }

function paginationToken(response: Record<string, unknown>): string {
  const pagination = response.pagination === undefined || response.pagination === null ? {} : parseObject(response.pagination);
  const token = stringField(pagination, 'next_page_token');
  if (token && !pageTokenPattern.test(token)) throw new TypeError('invalid Calendly pagination');
  return token;
}

function rfc3339(value: string, optional = false): string {
  if (optional && !value) return '';
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new TypeError('invalid Calendly timestamp');
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction = '', zone] = match;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText), hour = Number(hourText), minute = Number(minuteText), second = Number(secondText);
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) throw new TypeError('invalid Calendly timestamp');
  const calendar = new Date(0);
  calendar.setUTCFullYear(year, month - 1, day);
  calendar.setUTCHours(0, 0, 0, 0);
  if (calendar.getUTCFullYear() !== year || calendar.getUTCMonth() !== month - 1 || calendar.getUTCDate() !== day) throw new TypeError('invalid Calendly timestamp');
  const zoneHours = zone === 'Z' ? 0 : Number(zone!.slice(1, 3));
  const zoneMinutes = zone === 'Z' ? 0 : Number(zone!.slice(4, 6));
  if (zoneMinutes > 59) throw new TypeError('invalid Calendly timestamp');
  const offset = zone === 'Z' ? 0 : (zone!.startsWith('+') ? 1 : -1) * (zoneHours * 60 + zoneMinutes);
  const milliseconds = Number((fraction + '000').slice(0, 3));
  const instant = new Date(calendar.getTime() + ((hour * 60 + minute - offset) * 60 + second) * 1000 + milliseconds);
  if (!Number.isFinite(instant.getTime())) throw new TypeError('invalid Calendly timestamp');
  const nanos = fraction.padEnd(9, '0').slice(0, 9);
  const suffix = nanos.replace(/0+$/, '');
  return instant.toISOString().slice(0, 19) + (suffix ? '.' + suffix : '') + 'Z';
}

function queryTimestamp(value: string): string { return rfc3339(value).replace(/(?:\.\d+)?Z$/, match => {
  const fraction = match.slice(1, -1);
  return '.' + (fraction + '000000').slice(0, 6) + 'Z';
}); }

function optionalTimestamp(record: Record<string, unknown>, name: string): string | undefined {
  const value = stringField(record, name);
  return value ? rfc3339(value) : undefined;
}

function decodeCancellation(value: unknown): Cancellation {
  const record = parseObject(value);
  return { ...(stringField(record, 'canceled_by') ? { canceledBy: stringField(record, 'canceled_by') } : {}), ...(stringField(record, 'reason') ? { reason: stringField(record, 'reason') } : {}), ...(stringField(record, 'canceler_type') ? { cancelerType: stringField(record, 'canceler_type') } : {}), ...(optionalTimestamp(record, 'created_at') ? { createdAt: optionalTimestamp(record, 'created_at') } : {}) };
}

function decodeEvent(value: unknown): ScheduledEvent {
  const record = parseObject(value);
  const uri = stringField(record, 'uri'); parseResourceURI(uri, 'scheduled_events', 'scheduled event');
  const status = stringField(record, 'status');
  if (status !== 'active' && status !== 'canceled') throw new TypeError('invalid event status');
  const location = record.location == null ? {} : parseObject(record.location);
  const counter = record.invitees_counter == null ? {} : parseObject(record.invitees_counter);
  const rawLocation = location.location;
  const hosts = arrayField(record, 'event_memberships').map(item => {
    const host = parseObject(item);
    return { userUri: stringField(host, 'user'), ...(stringField(host, 'user_email') ? { email: stringField(host, 'user_email') } : {}), ...(stringField(host, 'user_name') ? { name: stringField(host, 'user_name') } : {}) };
  });
  return { uri, status, startTime: rfc3339(stringField(record, 'start_time')), endTime: rfc3339(stringField(record, 'end_time')),
    ...(stringField(record, 'name') ? { name: stringField(record, 'name') } : {}), ...(stringField(record, 'event_type') ? { eventTypeUri: stringField(record, 'event_type') } : {}),
    location: { ...(stringField(location, 'type') ? { type: stringField(location, 'type') } : {}), ...(typeof rawLocation === 'string' && rawLocation.trim() ? { location: rawLocation.trim() } : {}), ...(stringField(location, 'join_url') ? { joinUrl: stringField(location, 'join_url') } : {}), ...(stringField(location, 'status') ? { status: stringField(location, 'status') } : {}) },
    inviteesCounter: { total: numberField(counter, 'total'), active: numberField(counter, 'active'), limit: numberField(counter, 'limit') },
    ...(hosts.length ? { hosts } : {}), ...(record.cancellation != null ? { cancellation: decodeCancellation(record.cancellation) } : {}), ...(optionalTimestamp(record, 'created_at') ? { createdAt: optionalTimestamp(record, 'created_at') } : {}), ...(optionalTimestamp(record, 'updated_at') ? { updatedAt: optionalTimestamp(record, 'updated_at') } : {}) };
}

function decodeInvitee(value: unknown, expectedEventURI: string): Invitee {
  const record = parseObject(value);
  const uri = stringField(record, 'uri');
  const match = /^https:\/\/api\.calendly\.com\/scheduled_events\/([A-Za-z0-9_-]{1,64})\/invitees\/([A-Za-z0-9_-]{1,64})$/.exec(uri);
  if (!match || stringField(record, 'event') !== `${base}/scheduled_events/${match[1]}` || stringField(record, 'event') !== expectedEventURI || !stringField(record, 'email')) throw new TypeError('invalid invitee');
  const status = stringField(record, 'status');
  if (!['active', 'canceled'].includes(status)) throw new TypeError('invalid invitee status');
  const answers = arrayField(record, 'questions_and_answers').map(item => { const answer = parseObject(item); return { question: stringField(answer, 'question'), answer: stringField(answer, 'answer'), position: integerField(answer, 'position') }; });
  return { uri, scheduledEventUri: expectedEventURI, email: stringField(record, 'email'), status, rescheduled: booleanField(record, 'rescheduled'),
    ...(stringField(record, 'name') ? { name: stringField(record, 'name') } : {}), ...(stringField(record, 'first_name') ? { firstName: stringField(record, 'first_name') } : {}), ...(stringField(record, 'last_name') ? { lastName: stringField(record, 'last_name') } : {}), ...(stringField(record, 'timezone') ? { timezone: stringField(record, 'timezone') } : {}), ...(answers.length ? { questionsAndAnswers: answers } : {}),
    ...(stringField(record, 'old_invitee') ? { oldInviteeUri: stringField(record, 'old_invitee') } : {}), ...(stringField(record, 'new_invitee') ? { newInviteeUri: stringField(record, 'new_invitee') } : {}), ...(stringField(record, 'cancel_url') ? { cancelUrl: stringField(record, 'cancel_url') } : {}), ...(stringField(record, 'reschedule_url') ? { rescheduleUrl: stringField(record, 'reschedule_url') } : {}),
    ...(record.cancellation != null ? { cancellation: decodeCancellation(record.cancellation) } : {}), ...(optionalTimestamp(record, 'created_at') ? { createdAt: optionalTimestamp(record, 'created_at') } : {}), ...(optionalTimestamp(record, 'updated_at') ? { updatedAt: optionalTimestamp(record, 'updated_at') } : {}) };
}

function credentialQuery<T>(operation: string, result: CalendlyCredentialResult, zero: T): QueryAttempt<T> | undefined {
  if (result.kind === 'resolved') return undefined;
  return result.kind === 'transient' ? newQueryRetry(failure(operation, 'AVAILABILITY', result.message)) : newQueryBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', result.message));
}

function credentialMutation<T>(operation: string, result: CalendlyCredentialResult, zero: T): MutationAttempt<T> | undefined {
  if (result.kind === 'resolved') return undefined;
  return result.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', result.message)) : newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', result.message));
}

function classifyQuery<T>(client: CalendlyClient, call: Call, operation: string, exchange: CalendlyExchange, zero: T, objectURI = '', hasNotFound = false): QueryAttempt<T> | undefined {
  if (exchange.kind === 'tooLarge') return newQueryBranch('invalidResponse', zero, client.receipt(call, objectURI), failure(operation, 'RESPONSE_TOO_LARGE', 'Calendly response exceeds the configured maxResponseBytes'));
  if (exchange.kind === 'malformed') return newQueryBranch('invalidResponse', zero, client.receipt(call, objectURI), failure(operation, 'PROTOCOL', 'Calendly returned a malformed response'));
  if (exchange.kind === 'defect') return newQueryBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'Calendly request could not be built'));
  if (exchange.kind !== 'response') return newQueryRetry(failure(operation, 'TRANSPORT', 'Calendly could not be reached'));
  const response = exchange.response;
  if (response.status >= 200 && response.status < 300) return undefined;
  const outcome = client.failure(operation, response);
  if (outcome.isRetry) return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(response.status === 404 && hasNotFound ? 'notFound' : 'providerRejected', zero, client.receipt(call, objectURI), outcome.failure);
}

async function currentUser(client: CalendlyClient, call: Call, credentials: Credentials): Promise<{ userUri: string; organizationUri: string; exchange: CalendlyExchange }> {
  const exchange = await client.exchange(call, credentials, 'GET', '/users/me');
  if (exchange.kind !== 'response' || exchange.response.status !== 200) return { userUri: '', organizationUri: '', exchange };
  try {
    const resource = responseResource(exchange);
    const userUri = stringField(resource, 'uri'); const organizationUri = stringField(resource, 'current_organization');
    parseResourceURI(userUri, 'users', 'user'); parseResourceURI(organizationUri, 'organizations', 'organization');
    return { userUri, organizationUri, exchange };
  } catch { return { userUri: '', organizationUri: '', exchange: { kind: 'malformed' } }; }
}

/** List one page of scheduled events. */
export class ListScheduledEventsOperation implements Query<ListScheduledEventsInput, ScheduledEventPage> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return ListScheduledEventsDefinition; }
  /** Resolve scope, send, and decode one page. */
  async invoke(call: Call, input: ListScheduledEventsInput): Promise<QueryAttempt<ScheduledEventPage>> {
    try {
      if (input.userUri) parseResourceURI(input.userUri, 'users', 'user');
      if (input.organizationUri) parseResourceURI(input.organizationUri, 'organizations', 'organization');
      if (!input.minStartTime || !input.maxStartTime) throw new TypeError('listScheduledEvents requires both minStartTime and maxStartTime');
      if (rfc3339(input.minStartTime) === '0001-01-01T00:00:00Z' || rfc3339(input.maxStartTime) === '0001-01-01T00:00:00Z') throw new TypeError('listScheduledEvents requires both minStartTime and maxStartTime');
      if (Date.parse(rfc3339(input.maxStartTime)) <= Date.parse(rfc3339(input.minStartTime))) throw new TypeError('maxStartTime must be after minStartTime');
      if (input.status && !['active', 'canceled'].includes(input.status)) throw new TypeError('status must be active, canceled, or blank');
      if (input.sort && !['start_time:asc', 'start_time:desc'].includes(input.sort)) throw new TypeError('sort must be start_time:asc, start_time:desc, or blank');
      if (input.inviteeEmail && !isPlainEmailAddress(input.inviteeEmail)) throw new TypeError('inviteeEmail must be one plain email address');
      validatePage(input.pageSize, input.pageToken);
    } catch (error) { return newQueryBranch('defect', zeroEventPage(), {}, failure('listScheduledEvents', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    const credentialAttempt = credentialQuery('listScheduledEvents', resolved, zeroEventPage()); if (credentialAttempt) return credentialAttempt;
    const credentials = (resolved as { credentials: Credentials }).credentials;
    let userUri = input.userUri ?? '';
    const organizationUri = input.organizationUri ?? '';
    if (!userUri && !organizationUri) {
      const current = await currentUser(this.client, call, credentials);
      const attempted = classifyQuery(this.client, call, 'listScheduledEvents', current.exchange, zeroEventPage()); if (attempted) return attempted;
      userUri = current.userUri;
    }
    const query: Record<string, string> = { min_start_time: queryTimestamp(input.minStartTime), max_start_time: queryTimestamp(input.maxStartTime), sort: input.sort || 'start_time:asc' };
    if (userUri) query.user = userUri;
    if (organizationUri) query.organization = organizationUri;
    if (input.status) query.status = input.status;
    if (input.inviteeEmail) query.invitee_email = input.inviteeEmail;
    if (input.pageSize) query.count = String(input.pageSize);
    if (input.pageToken) query.page_token = input.pageToken;
    const exchange = await this.client.exchange(call, credentials, 'GET', '/scheduled_events', query);
    const attempted = classifyQuery(this.client, call, 'listScheduledEvents', exchange, zeroEventPage()); if (attempted) return attempted;
    try {
      const response = parseBody(exchange);
      const events = arrayField(response, 'collection').map(decodeEvent);
      const nextPageToken = paginationToken(response);
      return newQueryBranch('listed', { events, ...(nextPageToken ? { nextPageToken } : {}), ...(userUri ? { userUri } : {}), ...(organizationUri ? { organizationUri } : {}), minStartTime: rfc3339(input.minStartTime), maxStartTime: rfc3339(input.maxStartTime) }, this.client.receipt(call));
    } catch { return newQueryBranch('invalidResponse', zeroEventPage(), this.client.receipt(call), failure('listScheduledEvents', 'PROTOCOL', 'Calendly returned a malformed response')); }
  }
}

/** Read one scheduled event. */
export class GetScheduledEventOperation implements Query<GetScheduledEventInput, ScheduledEvent> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return GetScheduledEventDefinition; }
  /** Send and decode the requested event. */
  async invoke(call: Call, input: GetScheduledEventInput): Promise<QueryAttempt<ScheduledEvent>> {
    let id: string;
    try { id = parseResourceURI(input.scheduledEventUri, 'scheduled_events', 'scheduled event'); }
    catch (error) { return newQueryBranch('defect', zeroEvent(), {}, failure('getScheduledEvent', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    const credentialAttempt = credentialQuery('getScheduledEvent', resolved, zeroEvent()); if (credentialAttempt) return credentialAttempt;
    const exchange = await this.client.exchange(call, (resolved as { credentials: Credentials }).credentials, 'GET', `/scheduled_events/${id}`);
    const attempted = classifyQuery(this.client, call, 'getScheduledEvent', exchange, zeroEvent(), input.scheduledEventUri, true); if (attempted) return attempted;
    try {
      const event = decodeEvent(responseResource(exchange));
      if (event.uri !== input.scheduledEventUri) throw new TypeError('wrong event');
      return newQueryBranch('found', event, this.client.receipt(call, event.uri));
    } catch { return newQueryBranch('invalidResponse', zeroEvent(), this.client.receipt(call, input.scheduledEventUri), failure('getScheduledEvent', 'PROTOCOL', 'Calendly returned a malformed response')); }
  }
}

/** List invitees of one scheduled event. */
export class ListEventInviteesOperation implements Query<ListEventInviteesInput, EventInviteePage> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return ListEventInviteesDefinition; }
  /** Send and decode one invitee page. */
  async invoke(call: Call, input: ListEventInviteesInput): Promise<QueryAttempt<EventInviteePage>> {
    let id: string;
    try {
      id = parseResourceURI(input.scheduledEventUri, 'scheduled_events', 'scheduled event');
      if (input.status && !['active', 'canceled'].includes(input.status)) throw new TypeError('status must be active, canceled, or blank');
      if (input.email && !isPlainEmailAddress(input.email)) throw new TypeError('email must be one plain email address');
      validatePage(input.pageSize, input.pageToken);
    } catch (error) { return newQueryBranch('defect', zeroInviteePage(), {}, failure('listEventInvitees', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    const credentialAttempt = credentialQuery('listEventInvitees', resolved, zeroInviteePage()); if (credentialAttempt) return credentialAttempt;
    const query: Record<string, string> = { sort: 'created_at:asc' };
    if (input.status) query.status = input.status;
    if (input.email) query.email = input.email;
    if (input.pageSize) query.count = String(input.pageSize);
    if (input.pageToken) query.page_token = input.pageToken;
    const exchange = await this.client.exchange(call, (resolved as { credentials: Credentials }).credentials, 'GET', `/scheduled_events/${id}/invitees`, query);
    const attempted = classifyQuery(this.client, call, 'listEventInvitees', exchange, zeroInviteePage(), input.scheduledEventUri, true); if (attempted) return attempted;
    try {
      const response = parseBody(exchange);
      const invitees = arrayField(response, 'collection').map(item => decodeInvitee(item, input.scheduledEventUri));
      const nextPageToken = paginationToken(response);
      return newQueryBranch('listed', { scheduledEventUri: input.scheduledEventUri, invitees, ...(nextPageToken ? { nextPageToken } : {}) }, this.client.receipt(call, input.scheduledEventUri));
    } catch { return newQueryBranch('invalidResponse', zeroInviteePage(), this.client.receipt(call, input.scheduledEventUri), failure('listEventInvitees', 'PROTOCOL', 'Calendly returned a malformed response')); }
  }
}

function classifyCancellationError(client: CalendlyClient, call: Call, exchange: CalendlyExchange, output: ScheduledEventCancellation): MutationAttempt<ScheduledEventCancellation> | undefined {
  if (exchange.kind === 'defect') return newMutationBranch('defect', output, {}, failure('cancelScheduledEvent', 'LOCAL_DEFECT', 'Calendly request could not be built'));
  if (exchange.kind === 'tooLarge') return newMutationBranch('invalidResponse', output, client.receipt(call, output.scheduledEventUri), failure('cancelScheduledEvent', 'RESPONSE_TOO_LARGE', 'Calendly response exceeds the configured maxResponseBytes'));
  if (exchange.kind !== 'response') return newMutationRetry(failure('cancelScheduledEvent', 'TRANSPORT', "Calendly's answer to the cancellation was lost; the retry reads the event back"));
  return undefined;
}

/** Cancel a scheduled event with safe read-back after a refusal. */
export class CancelScheduledEventOperation implements Mutation<CancelScheduledEventInput, ScheduledEventCancellation> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return CancelScheduledEventDefinition; }
  /** Correlate retries with the Dex Call ID. */ idempotencyKey(callId: string) { return callId; }
  /** Send a cancellation or inspect a 403 to detect an earlier cancellation. */
  async invoke(call: Call, input: CancelScheduledEventInput): Promise<MutationAttempt<ScheduledEventCancellation>> {
    const output = zeroCancellation(input.scheduledEventUri);
    let id: string;
    try {
      id = parseResourceURI(input.scheduledEventUri, 'scheduled_events', 'scheduled event');
      if (Array.from(input.reason ?? '').length > 10_000) throw new TypeError('cancellation reason cannot exceed 10000 characters');
    } catch (error) { return newMutationBranch('defect', output, {}, failure('cancelScheduledEvent', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    const credentialAttempt = credentialMutation('cancelScheduledEvent', resolved, output); if (credentialAttempt) return credentialAttempt;
    const credentials = (resolved as { credentials: Credentials }).credentials;
    const exchange = await this.client.exchange(call, credentials, 'POST', `/scheduled_events/${id}/cancellation`, undefined, input.reason ? { reason: input.reason } : {});
    const errorAttempt = classifyCancellationError(this.client, call, exchange, output); if (errorAttempt) return errorAttempt;
    const response = (exchange as Extract<CalendlyExchange, { kind: 'response' }>).response;
    if (response.status === 200 || response.status === 201) {
      try { output.cancellation = decodeCancellation(responseResource(exchange)); return newMutationBranch('canceled', output, this.client.receipt(call, input.scheduledEventUri)); }
      catch { return newMutationBranch('invalidResponse', output, this.client.receipt(call, input.scheduledEventUri), failure('cancelScheduledEvent', 'PROTOCOL', 'Calendly returned a malformed response')); }
    }
    if (response.status === 403) return this.readBackRefusedCancellation(call, credentials, id, output, exchange);
    if (response.status === 404) return newMutationBranch('notFound', output, this.client.receipt(call, input.scheduledEventUri), failure('cancelScheduledEvent', 'NOT_FOUND', 'the Calendly scheduled event was not found'));
    const outcome = this.client.failure('cancelScheduledEvent', response);
    return outcome.isRetry ? newMutationRetry(outcome.failure, outcome.retryAfterMs) : newMutationBranch('providerRejected', output, this.client.receipt(call, input.scheduledEventUri), outcome.failure);
  }

  private async readBackRefusedCancellation(call: Call, credentials: Credentials, id: string, output: ScheduledEventCancellation, refusal: CalendlyExchange): Promise<MutationAttempt<ScheduledEventCancellation>> {
    const exchange = await this.client.exchange(call, credentials, 'GET', `/scheduled_events/${id}`);
    const errorAttempt = classifyCancellationError(this.client, call, exchange, output); if (errorAttempt) return errorAttempt;
    const response = (exchange as Extract<CalendlyExchange, { kind: 'response' }>).response;
    if (response.status === 404) return newMutationBranch('notFound', output, this.client.receipt(call, output.scheduledEventUri), failure('cancelScheduledEvent', 'NOT_FOUND', 'the Calendly scheduled event was not found'));
    if (response.status < 200 || response.status >= 300) {
      const readOutcome = this.client.failure('cancelScheduledEvent', response);
      if (readOutcome.isRetry) return newMutationRetry(readOutcome.failure, readOutcome.retryAfterMs);
      const original = this.client.failure('cancelScheduledEvent', (refusal as Extract<CalendlyExchange, { kind: 'response' }>).response);
      return newMutationBranch('providerRejected', output, this.client.receipt(call, output.scheduledEventUri), original.failure);
    }
    let event: ScheduledEvent;
    try { event = decodeEvent(responseResource(exchange)); if (event.uri !== output.scheduledEventUri) throw new TypeError('wrong event'); }
    catch { return newMutationBranch('invalidResponse', output, this.client.receipt(call, output.scheduledEventUri), failure('cancelScheduledEvent', 'PROTOCOL', 'Calendly returned a malformed response')); }
    if (event.status === 'canceled') return newMutationBranch('canceled', { ...output, alreadyCanceled: true, ...(event.cancellation ? { cancellation: event.cancellation } : {}) }, this.client.receipt(call, output.scheduledEventUri));
    const refusalResponse = (refusal as Extract<CalendlyExchange, { kind: 'response' }>).response;
    let failureKind: Parameters<typeof failure>[1] = 'AUTHORIZATION';
    let message = 'Calendly refused to cancel the active event; it may have already started, or the token may not cancel it';
    try { if (stringField(parseObject(JSON.parse(Buffer.from(refusalResponse.body).toString('utf8'))), 'title') === 'Insufficient scope') message = 'the Calendly token lacks the scheduled_events:write scope needed to cancel events'; }
    catch { /* A malformed provider error body does not change the refusal. */ }
    if (message === 'Calendly refused to cancel the active event; it may have already started, or the token may not cancel it' && Date.parse(event.startTime) <= this.client.now().getTime()) { failureKind = 'CONFLICT'; message = 'Calendly cannot cancel an event that has already started'; }
    return newMutationBranch('providerRejected', output, this.client.receipt(call, output.scheduledEventUri), failure('cancelScheduledEvent', failureKind, message));
  }
}

/** Create a single-use link under a durable single-dispatch fence. */
export class CreateSchedulingLinkOperation implements Mutation<CreateSchedulingLinkInput, SchedulingLink> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return CreateSchedulingLinkDefinition; }
  /** Correlate the unsafe write with the Dex Call ID. */ idempotencyKey(callId: string) { return callId; }
  /** Dispatch once and classify an unconfirmable write as uncertain. */
  async invoke(call: Call, input: CreateSchedulingLinkInput): Promise<MutationAttempt<SchedulingLink>> {
    const output = zeroLink(input.eventTypeUri);
    let fence;
    try { fence = newFence(call, CreateSchedulingLinkDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { writeWindowFraction: 5 / 6 }); }
    catch (error) { return newMutationBranch('defect', output, {}, failure('createSchedulingLink', 'LOCAL_DEFECT', (error as Error).message)); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(output, failure('createSchedulingLink', 'PROTOCOL', 'the scheduling link dispatch checkpoint cannot be trusted'), this.client.receipt(call));
    if (decision.kind === 'readBackRequired') return newMutationUncertain(output, failure('createSchedulingLink', decision.marker?.callId === call.id ? 'TRANSPORT' : 'PROTOCOL', decision.marker?.callId === call.id ? 'a prior scheduling link request may have been accepted, and Calendly offers no link read-back' : 'no confirmable prior scheduling link checkpoint exists'), this.client.receipt(call));
    try { parseResourceURI(input.eventTypeUri, 'event_types', 'event type'); }
    catch (error) { return newMutationBranch('defect', output, {}, failure('createSchedulingLink', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    if (resolved.kind !== 'resolved') {
      try { await fence.recordNotSent(); }
      catch { return newMutationRetry(failure('createSchedulingLink', 'AVAILABILITY', 'Calendly credentials and a safe retry checkpoint are unavailable')); }
      return credentialMutation('createSchedulingLink', resolved, output)!;
    }
    let exchange: CalendlyExchange;
    try { exchange = await fence.dispatch(signal => this.client.exchange(call, resolved.credentials, 'POST', '/scheduling_links', undefined, { max_event_count: 1, owner: input.eventTypeUri, owner_type: 'EventType' }, signal, false)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure('createSchedulingLink', 'AVAILABILITY', 'the scheduling link request was not sent before its safe deadline'));
      return newMutationUncertain(output, failure('createSchedulingLink', 'TRANSPORT', "Calendly's scheduling link answer was lost after the request may have been sent"), this.client.receipt(call));
    }
    if (exchange.kind === 'notSent') {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure('createSchedulingLink', 'AVAILABILITY', 'Calendly could not be reached and the retry checkpoint failed')); }
      return newMutationRetry(failure('createSchedulingLink', 'AVAILABILITY', 'Calendly could not be reached before a connection was obtained'));
    }
    if (exchange.kind === 'transport') return newMutationUncertain(output, failure('createSchedulingLink', 'TRANSPORT', "Calendly's scheduling link answer was lost after the request may have been sent"), this.client.receipt(call));
    if (exchange.kind === 'tooLarge') return newMutationUncertain(output, failure('createSchedulingLink', 'RESPONSE_TOO_LARGE', "Calendly's scheduling link response exceeds maxResponseBytes"), this.client.receipt(call));
    if (exchange.kind === 'defect') return newMutationBranch('defect', output, {}, failure('createSchedulingLink', 'LOCAL_DEFECT', 'Calendly request could not be built'));
    const response = (exchange as Extract<CalendlyExchange, { kind: 'response' }>).response;
    if (response.status >= 200 && response.status < 300) {
      try {
        const resource = responseResource(exchange);
        const bookingUrl = stringField(resource, 'booking_url');
        const parsed = new URL(bookingUrl);
        if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password || /%(?![0-9A-Fa-f]{2})/.test(bookingUrl) || (stringField(resource, 'owner') && stringField(resource, 'owner') !== input.eventTypeUri)) throw new TypeError('invalid link');
        try { await fence.recordProviderId(bookingUrl); } catch { /* The accepted provider result remains authoritative. */ }
        return newMutationBranch('created', { bookingUrl, eventTypeUri: input.eventTypeUri }, this.client.receipt(call, bookingUrl));
      } catch { return newMutationUncertain(output, failure('createSchedulingLink', 'PROTOCOL', 'Calendly created a scheduling link but its answer is malformed'), this.client.receipt(call)); }
    }
    const outcome = this.client.failure('createSchedulingLink', response);
    if (response.status === 429 || response.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure('createSchedulingLink', 'AVAILABILITY', 'Calendly declined the request but the retry checkpoint failed')); }
      return newMutationRetry(outcome.failure, outcome.retryAfterMs);
    }
    if (response.status === 409) return newMutationUncertain(output, failure('createSchedulingLink', 'CONFLICT', 'Calendly reported a link conflict without a way to identify the existing link'), this.client.receipt(call));
    if (response.status >= 500) return newMutationUncertain(output, failure('createSchedulingLink', 'AVAILABILITY', "Calendly's response does not prove whether a scheduling link was created"), this.client.receipt(call));
    return newMutationBranch('providerRejected', output, this.client.receipt(call), outcome.failure);
  }
}

interface WebhookRequest { callbackURL: string; scope: string; events: string[]; userURI: string; organizationURI: string; }

function validateWebhookInput(input: CreateWebhookSubscriptionInput): WebhookRequest {
  let callback: URL;
  try { callback = new URL(input.callbackUrl); }
  catch { throw new TypeError('callbackUrl must be an absolute HTTPS URL without user information or a fragment'); }
  if (callback.protocol !== 'https:' || !callback.hostname || callback.username || callback.password || callback.hash || /%(?![0-9A-Fa-f]{2})/.test(input.callbackUrl)) throw new TypeError('callbackUrl must be an absolute HTTPS URL without user information or a fragment');
  const scope = input.scope || 'user';
  if (scope !== 'user' && scope !== 'organization') throw new TypeError('scope must be user, organization, or blank');
  if (scope === 'organization' && input.userUri) throw new TypeError('userUri applies only to a user subscription');
  if (input.userUri) parseResourceURI(input.userUri, 'users', 'user');
  if (input.organizationUri) parseResourceURI(input.organizationUri, 'organizations', 'organization');
  const events = input.events?.length ? input.events : ['invitee.created', 'invitee.canceled'];
  if (new Set(events).size !== events.length || events.some(event => event !== 'invitee.created' && event !== 'invitee.canceled')) throw new TypeError('events must be distinct values from invitee.created and invitee.canceled');
  return { callbackURL: input.callbackUrl, scope, events, userURI: input.userUri ?? '', organizationURI: input.organizationUri ?? '' };
}

function decodeSubscription(value: unknown): WebhookSubscription {
  const record = parseObject(value);
  const uri = stringField(record, 'uri'); parseResourceURI(uri, 'webhook_subscriptions', 'webhook subscription');
  const callbackUrl = stringField(record, 'callback_url'), state = stringField(record, 'state'), scope = stringField(record, 'scope');
  if (!callbackUrl || !state || !scope) throw new TypeError('invalid webhook subscription');
  const events = arrayField(record, 'events').map(value => { if (typeof value !== 'string') throw new TypeError('invalid event'); return value; });
  return { uri, callbackUrl, state, events, scope, organizationUri: stringField(record, 'organization'),
    ...(stringField(record, 'user') ? { userUri: stringField(record, 'user') } : {}), ...(optionalTimestamp(record, 'created_at') ? { createdAt: optionalTimestamp(record, 'created_at') } : {}), wasCreated: false };
}

/** Register or reuse one Calendly webhook subscription. */
export class CreateWebhookSubscriptionOperation implements Mutation<CreateWebhookSubscriptionInput, WebhookSubscription> {
  /** Bind the Calendly client. */ constructor(private readonly client: CalendlyClient) {}
  /** Return the manifest definition. */ definition() { return CreateWebhookSubscriptionDefinition; }
  /** Correlate retries with the Dex Call ID. */ idempotencyKey(callId: string) { return callId; }
  /** List before create, then read back after conflicts or lost answers. */
  async invoke(call: Call, input: CreateWebhookSubscriptionInput): Promise<MutationAttempt<WebhookSubscription>> {
    let request: WebhookRequest;
    try { request = validateWebhookInput(input); }
    catch (error) { return newMutationBranch('defect', zeroSubscription(), {}, failure('createWebhookSubscription', 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call);
    const credentialAttempt = credentialMutation('createWebhookSubscription', resolved, zeroSubscription()); if (credentialAttempt) return credentialAttempt;
    const credentials = (resolved as { credentials: Credentials }).credentials;
    const registerKey = credentials.authMethodId === 'personal-access-token';
    if (registerKey && !credentials.webhookSigningKey?.reveal()) return newMutationBranch('defect', zeroSubscription(), {}, failure('createWebhookSubscription', 'VALIDATION', 'set webhook_signing_key on the Calendly connection before creating a subscription, so deliveries can be verified'));
    if (!request.organizationURI || (request.scope === 'user' && !request.userURI)) {
      const current = await currentUser(this.client, call, credentials);
      const attempted = this.failedExchange(call, current.exchange); if (attempted) return attempted;
      if (!request.organizationURI) request.organizationURI = current.organizationUri;
      if (request.scope === 'user' && !request.userURI) request.userURI = current.userUri;
    }
    const listed = await this.reuseListedSubscription(call, credentials, request);
    if (listed) return listed;
    const payload: Record<string, unknown> = { url: request.callbackURL, events: request.events, organization: request.organizationURI, scope: request.scope };
    if (request.scope === 'user') payload.user = request.userURI;
    if (registerKey) payload.signing_key = credentials.webhookSigningKey!.reveal();
    const exchange = await this.client.exchange(call, credentials, 'POST', '/webhook_subscriptions', undefined, payload);
    if (exchange.kind === 'response' && exchange.response.status === 409) {
      const retryList = await this.reuseListedSubscription(call, credentials, request); if (retryList) return retryList;
      return newMutationBranch('conflictingSubscription', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'CONFLICT', 'Calendly already has a subscription for this callback URL in another scope or for another user; delete it or use another URL'));
    }
    const attempted = this.failedExchange(call, exchange); if (attempted) return attempted;
    try {
      const subscription = decodeSubscription(responseResource(exchange));
      if (subscription.callbackUrl !== request.callbackURL) throw new TypeError('wrong callback');
      return newMutationBranch('subscribed', { ...subscription, wasCreated: true }, this.client.receipt(call, subscription.uri));
    } catch { return newMutationBranch('invalidResponse', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'PROTOCOL', 'Calendly returned a malformed response')); }
  }

  private async reuseListedSubscription(call: Call, credentials: Credentials, request: WebhookRequest): Promise<MutationAttempt<WebhookSubscription> | undefined> {
    const query: Record<string, string> = { organization: request.organizationURI, scope: request.scope, count: '100', sort: 'created_at:asc' };
    if (request.scope === 'user') query.user = request.userURI;
    for (let page = 0; page < 10; page++) {
      const exchange = await this.client.exchange(call, credentials, 'GET', '/webhook_subscriptions', query);
      const attempted = this.failedExchange(call, exchange); if (attempted) return attempted;
      try {
        const response = parseBody(exchange);
        for (const item of arrayField(response, 'collection')) {
          const raw = parseObject(item);
          if (stringField(raw, 'callback_url') !== request.callbackURL) continue;
          const existing = decodeSubscription(raw);
          const covers = existing.state === 'active' && request.events.every(event => existing.events?.includes(event));
          return covers ? newMutationBranch('subscribed', existing, this.client.receipt(call, existing.uri)) : newMutationBranch('conflictingSubscription', existing, this.client.receipt(call, existing.uri), failure('createWebhookSubscription', 'CONFLICT', 'a Calendly subscription for this callback URL is disabled or lacks a requested event; delete it and run again'));
        }
        const nextPageToken = paginationToken(response);
        if (!nextPageToken) return undefined;
        query.page_token = nextPageToken;
      } catch { return newMutationBranch('invalidResponse', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'PROTOCOL', 'Calendly returned a malformed response')); }
    }
    return newMutationBranch('invalidResponse', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'RESPONSE_TOO_LARGE', 'the scope has more than 1000 webhook subscriptions'));
  }

  private failedExchange(call: Call, exchange: CalendlyExchange): MutationAttempt<WebhookSubscription> | undefined {
    if (exchange.kind === 'defect') return newMutationBranch('defect', zeroSubscription(), {}, failure('createWebhookSubscription', 'LOCAL_DEFECT', 'Calendly request could not be built'));
    if (exchange.kind === 'tooLarge') return newMutationBranch('invalidResponse', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'RESPONSE_TOO_LARGE', 'Calendly response exceeds the configured maxResponseBytes'));
    if (exchange.kind === 'malformed') return newMutationBranch('invalidResponse', zeroSubscription(), this.client.receipt(call), failure('createWebhookSubscription', 'PROTOCOL', 'Calendly returned a malformed response'));
    if (exchange.kind !== 'response') return newMutationRetry(failure('createWebhookSubscription', 'TRANSPORT', "Calendly's answer was lost; the retry lists subscriptions before creating one"));
    const response = exchange.response;
    if (response.status >= 200 && response.status < 300) return undefined;
    const outcome = this.client.failure('createWebhookSubscription', response);
    return outcome.isRetry ? newMutationRetry(outcome.failure, outcome.retryAfterMs) : newMutationBranch('providerRejected', zeroSubscription(), this.client.receipt(call), outcome.failure);
  }
}
