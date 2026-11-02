// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketConversation, TicketDetails, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { AddNoteDefinition, CreateTicketDefinition, GetTicketDefinition, SearchTicketsDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import type { Exchange, FreshdeskClient } from './client.js';

const zeroTicket: Ticket = { id: 0, status: 0, priority: 0, createdAt: '0001-01-01T00:00:00Z', updatedAt: '0001-01-01T00:00:00Z' };
const zeroDetails: TicketDetails = { ticket: zeroTicket, conversations: [] };
const zeroConversation: TicketConversation = { id: 0, source: 0, isPrivate: false, body: '', createdAt: '0001-01-01T00:00:00Z' };
const tagPattern = /^[A-Za-z0-9](?:[A-Za-z0-9 _./+-]{0,62}[A-Za-z0-9_./+-])?$/;
const positive = (value: number): boolean => Number.isSafeInteger(value) && value > 0;
const nonnegative = (value: number): boolean => Number.isSafeInteger(value) && value >= 0;

function validateTags(tags: string[]): void {
  const seen = new Set<string>();
  for (const tag of tags) {
    if (!tagPattern.test(tag) || Buffer.byteLength(tag) > 64 || seen.has(tag.toLowerCase())) throw new TypeError('Freshdesk tag is invalid or duplicated');
    seen.add(tag.toLowerCase());
  }
}
function validateStatus(value: number): void { if (!Number.isSafeInteger(value) || value < 2) throw new TypeError('Freshdesk status is invalid'); }
function validatePriority(value: number): void { if (!Number.isSafeInteger(value) || value < 1 || value > 4) throw new TypeError('Freshdesk priority is invalid'); }
function validateText(value: string): void { if (!value?.trim()) throw new TypeError('Freshdesk text is required'); }
function validateEmail(value: string): void {
  // Matches Go mail.ParseAddress's bare addr-spec restriction for the supported ASCII mailbox shape.
  if (!/^[A-Za-z0-9.!#$%&*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$/.test(value) || value.includes('..') || /["'()<>,;\s]/.test(value)) throw new TypeError('Freshdesk requester email is invalid');
}
function encodeHTML(text: string): string { return text.replaceAll('\r\n', '\n').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&#34;').replaceAll("'", '&#39;').replaceAll('\n', '<br>'); }
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Freshdesk response is not an object'); return value as Record<string, unknown>; }
function parse(body: Uint8Array): unknown { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
function number(value: unknown): number { if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('Freshdesk numeric field is invalid'); return value; }
function text(value: unknown): string { if (typeof value !== 'string') throw new TypeError('Freshdesk text field is invalid'); return value; }
function wireNumber(value: unknown): number { return value == null ? 0 : number(value); }
function wireText(value: unknown): string { return value == null ? '' : text(value); }
function wireBoolean(value: unknown): boolean { if (value == null) return false; if (typeof value !== 'boolean') throw new TypeError('Freshdesk boolean field is invalid'); return value; }
function optionalField<T>(value: T | undefined | null, name: string): Record<string, T> { return value ? { [name]: value } as Record<string, T> : {}; }
function timestamp(value: unknown): string { const raw = text(value); const date = new Date(raw); if (!Number.isFinite(date.getTime()) || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(raw)) throw new TypeError('Freshdesk timestamp is invalid'); return date.toISOString().replace('.000Z', 'Z'); }
function truncated(value: string): { text: string; isTruncated: boolean } {
  const bytes = Buffer.from(value);
  if (bytes.length <= 16_384) return { text: value, isTruncated: false };
  let end = 16_384;
  while (end > 0 && (bytes[end]! & 0xc0) === 0x80) end--;
  return { text: bytes.subarray(0, end).toString('utf8'), isTruncated: true };
}
function ticket(value: unknown, expectedID = 0): Ticket {
  const wire = object(value);
  const id = number(wire.id);
  const status = number(wire.status);
  if (!positive(id) || status < 1 || (expectedID && id !== expectedID)) throw new TypeError('Freshdesk ticket ID or status is invalid');
  const description = truncated(wireText(wire.description_text));
  const tags = wire.tags === undefined || wire.tags === null ? undefined : wire.tags;
  if (tags !== undefined && (!Array.isArray(tags) || tags.some(item => typeof item !== 'string'))) throw new TypeError('Freshdesk ticket tags are invalid');
  const result: Ticket = { id, status, priority: wireNumber(wire.priority), createdAt: timestamp(wire.created_at), updatedAt: timestamp(wire.updated_at),
    ...optionalField(wireText(wire.subject), 'subject'), ...optionalField(description.text, 'description'), ...(description.isTruncated ? { isDescriptionTruncated: true } : {}),
    ...optionalField(wireNumber(wire.source), 'source'), ...optionalField(wireText(wire.type), 'type'), ...optionalField(wireNumber(wire.requester_id), 'requesterId'),
    ...optionalField(wireNumber(wire.responder_id), 'responderId'), ...optionalField(wireNumber(wire.group_id), 'groupId'), ...optionalField(wireNumber(wire.company_id), 'companyId'),
    ...optionalField(wireNumber(wire.product_id), 'productId'), ...(tags?.length ? { tags: tags as string[] } : {}), ...(wireBoolean(wire.is_escalated) ? { isEscalated: true } : {}), ...(wireBoolean(wire.spam) ? { isSpam: true } : {}) };
  if (wire.due_by) result.dueBy = timestamp(wire.due_by);
  if (wire.fr_due_by) result.firstResponseDueBy = timestamp(wire.fr_due_by);
  return result;
}
function conversation(value: unknown): TicketConversation {
  const wire = object(value);
  const id = number(wire.id);
  if (!positive(id)) throw new TypeError('Freshdesk conversation ID is invalid');
  const body = truncated(wireText(wire.body_text));
  return { id, source: wireNumber(wire.source), isPrivate: wireBoolean(wire.private), body: body.text, createdAt: timestamp(wire.created_at),
    ...optionalField(wireNumber(wire.ticket_id), 'ticketId'), ...optionalField(wireNumber(wire.user_id), 'userId'), ...(wireBoolean(wire.incoming) ? { isIncoming: true } : {}), ...(body.isTruncated ? { isBodyTruncated: true } : {}) };
}
function queryFailure<T>(client: FreshdeskClient, call: Call, operation: string, branch: string, value: T, kind: Failure['kind'], message: string, response?: ProviderResponse): QueryAttempt<T> {
  return newQueryBranch(branch, value, client.receipt(call, response), client.failure(operation, kind, message));
}
function mutationFailure<T>(client: FreshdeskClient, call: Call, operation: string, branch: string, value: T, kind: Failure['kind'], message: string, response?: ProviderResponse): MutationAttempt<T> {
  return newMutationBranch(branch, value, client.receipt(call, response), client.failure(operation, kind, message));
}
function queryExchange<T>(client: FreshdeskClient, call: Call, exchange: Exchange, zero: T, notFoundBranch: string): QueryAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'serviceUnavailable', 'unavailable'].includes(exchange.outcome)) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' ? notFoundBranch : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, client.receipt(call, exchange.response), exchange.failure);
}
function updateExchange(client: FreshdeskClient, call: Call, exchange: Exchange, value: UpdateTicketOutput): MutationAttempt<UpdateTicketOutput> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'serviceUnavailable', 'unavailable'].includes(exchange.outcome)) return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, value, client.receipt(call, exchange.response), exchange.failure);
}
async function credential(client: FreshdeskClient, call: Call, operation: string): Promise<{ key: string } | { failure: Failure; transient: boolean }> { return client.resolveCredential(call, operation); }

/** Search one Freshdesk filter page, optionally using a contact lookup. */
export class SearchTicketsOperation implements Query<SearchTicketsInput, SearchTicketsOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return SearchTicketsDefinition; }
  /** Validate filters, then perform at most two reads. */
  async invoke(call: Call, input: SearchTicketsInput): Promise<QueryAttempt<SearchTicketsOutput>> {
    const operation = 'searchTickets';
    const zero: SearchTicketsOutput = { tickets: [], totalMatched: 0 };
    let filter: string;
    try { filter = buildFilter(input); if (!nonnegative(input.page ?? 0) || (input.page ?? 0) > 10) throw new TypeError('page is invalid'); if (input.requesterEmail) validateEmail(input.requesterEmail); }
    catch (error) { return queryFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', String(error)); }
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) return resolved.transient ? newQueryRetry(resolved.failure) : newQueryBranch('defect', zero, {}, resolved.failure);
    let requesterIDs: Set<number> | undefined;
    if (input.requesterEmail) {
      const contact = await this.client.exchange(call, operation, resolved.key, 'GET', '/contacts', new URLSearchParams({ email: input.requesterEmail }));
      const terminal = queryExchange(this.client, call, contact, zero, 'providerRejected'); if (terminal) return terminal;
      try {
        const rows = parse((contact as { response: ProviderResponse }).response.body);
        if (!Array.isArray(rows)) throw new TypeError('contact list is not an array');
        requesterIDs = new Set<number>();
        for (const row of rows) { const record = object(row); const id = number(record.id); if (!positive(id)) throw new TypeError('contact has no ID'); if (wireText(record.email).toLowerCase() === input.requesterEmail.toLowerCase()) requesterIDs.add(id); }
      } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid contact list', contact.response); }
      if (requesterIDs.size === 0) return newQueryBranch('searched', { tickets: [], totalMatched: 0 }, this.client.receipt(call, contact.response));
    }
    const page = input.page || 1;
    const response = await this.client.exchange(call, operation, resolved.key, 'GET', '/search/tickets', new URLSearchParams({ query: `"${filter}"`, page: String(page) }));
    const terminal = queryExchange(this.client, call, response, zero, 'providerRejected'); if (terminal) return terminal;
    try {
      const wire = object(parse((response as { response: ProviderResponse }).response.body));
      const total = number(wire.total);
      if (total < 0 || !Array.isArray(wire.results) || wire.results.length > 30) throw new TypeError('invalid search page');
      const tickets = wire.results.map(item => ticket(item)).filter(item => !requesterIDs || requesterIDs.has(item.requesterId ?? 0)).map(item => { delete item.description; delete item.isDescriptionTruncated; return item; });
      return newQueryBranch('searched', { tickets, totalMatched: total, ...(page < 10 && page * 30 < total ? { nextPage: page + 1 } : {}) }, this.client.receipt(call, response.response));
    } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid search page', response.response); }
  }
}

/** Read one ticket, requester, and bounded conversation page. */
export class GetTicketOperation implements Query<GetTicketInput, TicketDetails> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return GetTicketDefinition; }
  /** Perform the ticket and conversation reads. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<TicketDetails>> {
    const operation = 'getTicket';
    const zero = zeroDetails;
    if (!positive(input.ticketId) || !nonnegative(input.conversationLimit ?? 0) || (input.conversationLimit ?? 0) > 20) return queryFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'ticketId or conversationLimit is invalid');
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) return resolved.transient ? newQueryRetry(resolved.failure) : newQueryBranch('defect', zero, {}, resolved.failure);
    const path = `/tickets/${input.ticketId}`;
    const first = await this.client.exchange(call, operation, resolved.key, 'GET', path, new URLSearchParams({ include: 'requester' }));
    const terminal = queryExchange(this.client, call, first, zero, 'notFound'); if (terminal) return terminal;
    let details: TicketDetails;
    try {
      const wire = object(parse((first as { response: ProviderResponse }).response.body));
      details = { ticket: ticket(wire, input.ticketId), conversations: [] };
      if (wire.requester) { const requester = object(wire.requester); const id = number(requester.id); if (positive(id)) { if (id !== details.ticket.requesterId) throw new TypeError('requester mismatch'); details.requester = { id, ...optionalField(wireText(requester.name), 'name'), ...optionalField(wireText(requester.email), 'email') }; } }
    } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid ticket', first.response); }
    const limit = input.conversationLimit || 10;
    const second = await this.client.exchange(call, operation, resolved.key, 'GET', `${path}/conversations`, new URLSearchParams({ per_page: String(limit), page: '1' }));
    const secondTerminal = queryExchange(this.client, call, second, zero, 'notFound'); if (secondTerminal) return secondTerminal;
    try {
      const rows = parse((second as { response: ProviderResponse }).response.body);
      if (!Array.isArray(rows) || rows.length > limit) throw new TypeError('invalid conversation page');
      details.conversations = rows.map(item => conversation(item)).sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id - right.id);
      if (String(second.response!.headers.link ?? '').includes('rel="next"')) details.hasMoreConversations = true;
      return newQueryBranch('found', details, this.client.receipt(call, second.response, input.ticketId));
    } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid conversation page', second.response); }
  }
}

function buildFilter(input: SearchTicketsInput): string {
  const clauses: string[] = [];
  const any = (values: string[]) => { if (values.length) clauses.push(values.length === 1 ? values[0]! : `(${values.join(' OR ')})`); };
  const statuses = input.statuses ?? [];
  statuses.forEach(validateStatus); if (new Set(statuses).size !== statuses.length) throw new TypeError('duplicate statuses');
  any(statuses.map(value => `status:${value}`));
  const priorities = input.priorities ?? [];
  priorities.forEach(validatePriority); if (new Set(priorities).size !== priorities.length) throw new TypeError('duplicate priorities');
  any(priorities.map(value => `priority:${value}`));
  validateTags(input.tags ?? []);
  any((input.tags ?? []).map(value => `tag:'${value}'`));
  if (input.updatedSinceDate) { const date = new Date(`${input.updatedSinceDate}T00:00:00Z`); if (!/^\d{4}-\d\d-\d\d$/.test(input.updatedSinceDate) || !Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== input.updatedSinceDate) throw new TypeError('updatedSinceDate is invalid'); clauses.push(`updated_at:>'${input.updatedSinceDate}'`); }
  if (!clauses.length) throw new TypeError('at least one Freshdesk filter is required');
  const query = clauses.join(' AND ');
  if (Buffer.byteLength(query) + 2 > 512) throw new TypeError('Freshdesk filter is too long');
  return query;
}

type SingleDispatchMarker = { freshdeskDispatchedCallId: string; freshdeskDispatchedAttempt?: number; freshdeskRetryAttempt?: number };
async function inspectDispatch(call: Call): Promise<'allowed' | 'uncertain'> {
  try {
    if (!call.context.hasLastHeartbeatValue()) return call.context.attempt === 1 ? 'allowed' : 'uncertain';
    const marker = call.context.getLastHeartbeatValue() as SingleDispatchMarker;
    return marker?.freshdeskDispatchedCallId === call.id && marker.freshdeskRetryAttempt === call.context.attempt ? 'allowed' : 'uncertain';
  } catch { return 'uncertain'; }
}
async function recordDispatch(call: Call): Promise<boolean> {
  try { await call.context.recordHeartbeat({ freshdeskDispatchedCallId: call.id, freshdeskDispatchedAttempt: call.context.attempt }); return true; }
  catch { await permitRetry(call); return false; }
}
async function permitRetry(call: Call): Promise<void> {
  try { await call.context.recordHeartbeat({ freshdeskDispatchedCallId: call.id, freshdeskRetryAttempt: call.context.attempt + 1 }); }
  catch { /* A missing permit fails closed on the next attempt. */ }
}
async function singleDispatchExchange<T>(client: FreshdeskClient, call: Call, exchange: Exchange, zero: T, notFoundBranch = 'providerRejected'): Promise<MutationAttempt<T> | undefined> {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'serviceUnavailable'].includes(exchange.outcome)) { await permitRetry(call); return newMutationRetry(exchange.failure, exchange.retryAfterMs); }
  if (exchange.outcome === 'unavailable' || exchange.outcome === 'invalid') return newMutationUncertain(zero, exchange.failure, client.receipt(call, exchange.response));
  const branch = exchange.outcome === 'notFound' ? notFoundBranch : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, zero, client.receipt(call, exchange.response), exchange.failure);
}
async function beforeSingleDispatch<T>(client: FreshdeskClient, call: Call, operation: string, zero: T): Promise<MutationAttempt<T> | undefined> {
  if (await inspectDispatch(call) === 'uncertain') return newMutationUncertain(zero, client.failure(operation, 'TRANSPORT', 'an earlier attempt of this Step may have sent the request, so it is not sent again'), client.receipt(call));
  return undefined;
}

/** Create one ticket with a checkpoint before the unkeyed POST. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return CreateTicketDefinition; }
  /** Correlate the mutation by Call ID; Freshdesk does not receive this key. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, checkpoint, and send at most once per safe attempt. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    const operation = 'createTicket'; const zero: CreateTicketOutput = { ticket: zeroTicket };
    try {
      validateText(input.subject); validateText(input.description); validateEmail(input.requester.email);
      if (input.requester.name && input.requester.name !== input.requester.name.trim()) throw new TypeError('requester name has surrounding spaces');
      if (input.status) validateStatus(input.status); if (input.priority) validatePriority(input.priority);
      if (input.type && (input.type !== input.type.trim() || Buffer.byteLength(input.type) > 255)) throw new TypeError('ticket type is invalid');
      validateTags(input.tags ?? []); if (!nonnegative(input.groupId ?? 0) || !nonnegative(input.responderId ?? 0)) throw new TypeError('assignment ID is invalid');
    } catch { return mutationFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'Freshdesk create input is invalid'); }
    const prior = await beforeSingleDispatch(this.client, call, operation, zero); if (prior) return prior;
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) { if (resolved.transient || call.context.attempt > 1) { await permitRetry(call); return newMutationRetry(resolved.failure); } return newMutationBranch('defect', zero, {}, resolved.failure); }
    if (!(await recordDispatch(call))) return newMutationRetry(this.client.failure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent'));
    const request = { subject: input.subject, description: encodeHTML(input.description), email: input.requester.email,
      ...(input.requester.name ? { name: input.requester.name } : {}), ...(input.status ? { status: input.status } : {}), ...(input.priority ? { priority: input.priority } : {}),
      ...(input.type ? { type: input.type } : {}), ...(input.tags?.length ? { tags: input.tags } : {}), ...(input.groupId ? { group_id: input.groupId } : {}), ...(input.responderId ? { responder_id: input.responderId } : {}) };
    const exchange = await this.client.exchange(call, operation, resolved.key, 'POST', '/tickets', undefined, request);
    const terminal = await singleDispatchExchange(this.client, call, exchange, zero); if (terminal) return terminal;
    try { const created = ticket(parse((exchange as { response: ProviderResponse }).response.body)); return newMutationBranch('created', { ticket: created }, this.client.receipt(call, exchange.response, created.id)); }
    catch { return newMutationUncertain(zero, this.client.failure(operation, 'PROTOCOL', 'Freshdesk accepted the ticket but returned an invalid ticket'), this.client.receipt(call, exchange.response)); }
  }
}

/** Replace changed ticket fields after reading current values. */
export class UpdateTicketOperation implements Mutation<UpdateTicketInput, UpdateTicketOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return UpdateTicketDefinition; }
  /** Correlate the absolute replacement by Call ID. */ idempotencyKey(callID: string): string { return callID; }
  /** Read, diff, and write only when a field differs. */
  async invoke(call: Call, input: UpdateTicketInput): Promise<MutationAttempt<UpdateTicketOutput>> {
    const operation = 'updateTicket'; const zero: UpdateTicketOutput = { ticket: zeroTicket };
    try {
      if (!positive(input.ticketId)) throw new TypeError('ticket ID is invalid');
      if (input.status) validateStatus(input.status); if (input.priority) validatePriority(input.priority);
      if (!nonnegative(input.groupId ?? 0) || !nonnegative(input.responderId ?? 0)) throw new TypeError('assignment ID is invalid');
      validateTags(input.addTags ?? []); validateTags(input.removeTags ?? []);
      if ((input.addTags ?? []).some(tag => (input.removeTags ?? []).some(other => other.toLowerCase() === tag.toLowerCase()))) throw new TypeError('tag appears in both changes');
      if (!input.status && !input.priority && !input.groupId && !input.responderId && !input.addTags?.length && !input.removeTags?.length) throw new TypeError('no ticket change');
    } catch { return mutationFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'Freshdesk update input is invalid'); }
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) return resolved.transient ? newMutationRetry(resolved.failure) : newMutationBranch('defect', zero, {}, resolved.failure);
    const path = `/tickets/${input.ticketId}`;
    const read = await this.client.exchange(call, operation, resolved.key, 'GET', path);
    const readTerminal = updateExchange(this.client, call, read, zero); if (readTerminal) return readTerminal;
    let current: Ticket;
    try { current = ticket(parse((read as { response: ProviderResponse }).response.body), input.ticketId); }
    catch { return mutationFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid ticket', read.response); }
    const change: Record<string, unknown> = {};
    if (input.status && input.status !== current.status) change.status = input.status;
    if (input.priority && input.priority !== current.priority) change.priority = input.priority;
    if (input.groupId && input.groupId !== current.groupId) change.group_id = input.groupId;
    if (input.responderId && input.responderId !== current.responderId) change.responder_id = input.responderId;
    const tags = (current.tags ?? []).filter(tag => !(input.removeTags ?? []).some(other => other.toLowerCase() === tag.toLowerCase()));
    for (const tag of input.addTags ?? []) if (!tags.some(existing => existing.toLowerCase() === tag.toLowerCase())) tags.push(tag);
    if (JSON.stringify(tags) !== JSON.stringify(current.tags ?? [])) change.tags = tags;
    if (!Object.keys(change).length) return newMutationBranch('updated', { ticket: current, wasAlreadyApplied: true }, this.client.receipt(call, read.response, input.ticketId));
    const write = await this.client.exchange(call, operation, resolved.key, 'PUT', path, undefined, change);
    const writeTerminal = updateExchange(this.client, call, write, { ticket: current }); if (writeTerminal) return writeTerminal;
    try { return newMutationBranch('updated', { ticket: ticket(parse((write as { response: ProviderResponse }).response.body), input.ticketId) }, this.client.receipt(call, write.response, input.ticketId)); }
    catch { return mutationFailure(this.client, call, operation, 'invalidResponse', { ticket: current }, 'PROTOCOL', 'Freshdesk returned an invalid updated ticket', write.response); }
  }
}

/** Add one private note or public reply with a checkpoint before POST. */
export class AddNoteOperation implements Mutation<AddNoteInput, AddNoteOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return AddNoteDefinition; }
  /** Correlate the mutation by Call ID; Freshdesk does not receive this key. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, checkpoint, and send one note or reply. */
  async invoke(call: Call, input: AddNoteInput): Promise<MutationAttempt<AddNoteOutput>> {
    const operation = 'addNote'; const zero: AddNoteOutput = { conversation: zeroConversation };
    if (!positive(input.ticketId) || !input.body?.trim()) return mutationFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'Freshdesk note input is invalid');
    const prior = await beforeSingleDispatch(this.client, call, operation, zero); if (prior) return prior;
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) { if (resolved.transient || call.context.attempt > 1) { await permitRetry(call); return newMutationRetry(resolved.failure); } return newMutationBranch('defect', zero, {}, resolved.failure); }
    if (!(await recordDispatch(call))) return newMutationRetry(this.client.failure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent'));
    const path = `/tickets/${input.ticketId}/${input.isPublicReply ? 'reply' : 'notes'}`;
    const payload = input.isPublicReply ? { body: encodeHTML(input.body) } : { body: encodeHTML(input.body), private: true };
    const exchange = await this.client.exchange(call, operation, resolved.key, 'POST', path, undefined, payload);
    const terminal = await singleDispatchExchange(this.client, call, exchange, zero, 'notFound'); if (terminal) return terminal;
    try {
      const wire = object(parse((exchange as { response: ProviderResponse }).response.body));
      const added = conversation(wire);
      if (added.ticketId && added.ticketId !== input.ticketId) throw new TypeError('conversation belongs to another ticket');
      added.ticketId = input.ticketId;
      added.source = input.isPublicReply ? 0 : 2;
      added.isPrivate = input.isPublicReply ? false : wire.private !== false;
      return newMutationBranch('added', { conversation: added }, this.client.receipt(call, exchange.response, added.id));
    } catch { return newMutationUncertain(zero, this.client.failure(operation, 'PROTOCOL', 'Freshdesk accepted the note but returned an invalid conversation'), this.client.receipt(call, exchange.response)); }
  }
}
