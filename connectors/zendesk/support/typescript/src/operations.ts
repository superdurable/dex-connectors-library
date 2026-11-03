// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CreateTicketDefinition, GetTicketDefinition, SearchTicketsDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import type { CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketComment, TicketDetails, TicketUser, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { zendeskFailure, isBareEmailAddress, type ZendeskClient, type ZendeskExchange, header } from './client.js';

const emptyTime = '0001-01-01T00:00:00Z';
const zeroTicket: Ticket = { id: 0, agentUrl: '', status: '', createdAt: emptyTime, updatedAt: emptyTime };
const zeroSearch: SearchTicketsOutput = { tickets: [] };
const zeroDetails: TicketDetails = { ticket: zeroTicket, latestComments: [] };
const zeroCreate: CreateTicketOutput = { ticket: zeroTicket };
const zeroUpdate: UpdateTicketOutput = { ticket: zeroTicket };
const statuses = new Set(['new', 'open', 'pending', 'hold', 'solved', 'closed']);
const priorities = new Set(['low', 'normal', 'high', 'urgent']);
const types = new Set(['problem', 'incident', 'question', 'task']);

/** Search one bounded export page of Zendesk tickets. */
export class SearchTicketsOperation implements Query<SearchTicketsInput, SearchTicketsOutput> {
  /** Bind the authenticated provider client. */ constructor(private readonly client: ZendeskClient) {}
  /** Return the manifest definition. */ definition() { return SearchTicketsDefinition; }
  /** Validate filters and retrieve one page. */
  async invoke(call: Call, input: SearchTicketsInput): Promise<QueryAttempt<SearchTicketsOutput>> {
    let query: string;
    try { query = buildTicketSearchQuery(input); validateSearchPage(input); }
    catch (error) { return queryDefect('searchTickets', zeroSearch, error); }
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return queryDefect('searchTickets', zeroSearch, 'connection credentials are unavailable', 'AUTHENTICATION');
    const parameters = { 'filter[type]': 'ticket', 'page[size]': String(input.pageSize || 25), query, ...(input.cursor ? { 'page[after]': input.cursor } : {}) };
    const result = await this.client.exchange(call, credentials, 'searchTickets', 'GET', '/search/export', parameters);
    const terminal = queryExchange(result, this.client.receipt(call, result.response), zeroSearch);
    if (terminal) return terminal;
    try {
      const body = objectJSON(result.response.body);
      if (!Array.isArray(body.results)) throw new Error('search page has no results array');
      const tickets = body.results.map(raw => { const wire = object(raw); if (wire.result_type !== undefined && wire.result_type !== '' && wire.result_type !== 'ticket') throw new Error('search result is not a ticket'); const decoded = decodeTicket(wire, this.client.agentTicketURL).ticket; delete decoded.description; delete decoded.isDescriptionTruncated; return decoded; });
      const meta = body.meta === undefined ? {} : object(body.meta);
      const hasMore = optionalBoolean(meta.has_more);
      const nextCursor = optionalString(meta.after_cursor);
      if (hasMore && !nextCursor) throw new Error('search page has more results but no cursor');
      return newQueryBranch('searched', { tickets, ...(hasMore ? { nextCursor } : {}) }, this.client.receipt(call, result.response));
    } catch (error) { return queryInvalid('searchTickets', zeroSearch, this.client.receipt(call, result.response), 'Zendesk returned an invalid search page', error); }
  }
}

/** Read a ticket, sideloaded requester, and latest comments. */
export class GetTicketOperation implements Query<GetTicketInput, TicketDetails> {
  /** Bind the authenticated provider client. */ constructor(private readonly client: ZendeskClient) {}
  /** Return the manifest definition. */ definition() { return GetTicketDefinition; }
  /** Read ticket and comment pages. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<TicketDetails>> {
    if (!validID(input.ticketId) || !Number.isInteger(input.latestCommentLimit ?? 0) || (input.latestCommentLimit ?? 0) < 0 || (input.latestCommentLimit ?? 0) > 20) return queryDefect('getTicket', zeroDetails, 'ticketId or latestCommentLimit is invalid');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return queryDefect('getTicket', zeroDetails, 'connection credentials are unavailable', 'AUTHENTICATION');
    const read = await this.client.exchange(call, credentials, 'getTicket', 'GET', `/tickets/${input.ticketId}`, { include: 'users' });
    const readReceipt = this.client.receipt(call, read.response, input.ticketId);
    const terminal = queryExchange(read, readReceipt, zeroDetails);
    if (terminal) return terminal;
    let details: TicketDetails;
    try {
      const envelope = objectJSON(read.response.body);
      const decoded = decodeTicket(object(envelope.ticket), this.client.agentTicketURL).ticket;
      if (decoded.id !== input.ticketId) throw new Error('ticket response is for another ticket');
      const users = envelope.users === undefined || envelope.users === null ? [] : array(envelope.users);
      const requester = users.map(user => decodeUser(object(user))).find(user => user.id === decoded.requesterId && user.id > 0);
      details = { ticket: decoded, latestComments: [], ...(requester ? { requester } : {}) };
    } catch (error) { return queryInvalid('getTicket', zeroDetails, readReceipt, 'Zendesk returned an invalid ticket', error); }
    const commentLimit = input.latestCommentLimit || 5;
    const comments = await this.client.exchange(call, credentials, 'getTicket', 'GET', `/tickets/${input.ticketId}/comments`, { 'page[size]': String(commentLimit), sort: '-created_at' });
    const receipt = this.client.receipt(call, comments.response, input.ticketId);
    const commentTerminal = queryExchange(comments, receipt, zeroDetails);
    if (commentTerminal) return commentTerminal;
    try {
      const page = objectJSON(comments.response.body);
      const rawComments = array(page.comments);
      if (rawComments.length > commentLimit) throw new Error('comment page is larger than requested');
      const latestComments = rawComments.map(raw => decodeComment(object(raw))).sort((left, right) => right.createdAt.localeCompare(left.createdAt) || right.id - left.id);
      const meta = page.meta === undefined ? {} : object(page.meta);
      return newQueryBranch('found', { ...details, latestComments, ...(optionalBoolean(meta.has_more) ? { hasOlderComments: true } : {}) }, receipt);
    } catch (error) { return queryInvalid('getTicket', zeroDetails, receipt, 'Zendesk returned an invalid comment page', error); }
  }
}

/** Create a ticket under Zendesk's stable Idempotency-Key. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the authenticated provider client. */ constructor(private readonly client: ZendeskClient) {}
  /** Return the manifest definition. */ definition() { return CreateTicketDefinition; }
  /** Use the stable connector Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Send a deterministic creation body. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    try { validateCreate(input); } catch (error) { return mutationDefect('createTicket', zeroCreate, error); }
    if (!call.idempotencyKey) return mutationDefect('createTicket', zeroCreate, 'connector call has no idempotency key', 'LOCAL_DEFECT');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return mutationDefect('createTicket', zeroCreate, 'connection credentials are unavailable', 'AUTHENTICATION');
    const ticket = { subject: input.subject, comment: { body: input.comment.body, public: !input.comment.isInternalNote },
      ...(input.requester ? { requester: { email: input.requester.email, ...(input.requester.name ? { name: input.requester.name } : {}) } } : {}),
      ...(input.priority ? { priority: input.priority } : {}), ...(input.type ? { type: input.type } : {}),
      ...(input.tags?.length ? { tags: input.tags } : {}), ...(input.groupId ? { group_id: input.groupId } : {}),
      ...(input.assigneeId ? { assignee_id: input.assigneeId } : {}), ...(input.brandId ? { brand_id: input.brandId } : {}),
      ...(input.externalId ? { external_id: input.externalId } : {}), metadata: { dex_idempotency_key: call.idempotencyKey } };
    const result = await this.client.exchange(call, credentials, 'createTicket', 'POST', '/tickets', undefined, { ticket }, call.idempotencyKey);
    const receipt = this.client.receipt(call, result.response);
    const terminal = mutationExchange(result, receipt, zeroCreate);
    if (terminal) return terminal;
    try {
      const decoded = decodeTicketEnvelope(result.response.body, this.client.agentTicketURL).ticket;
      const lookup = header(result.response.headers, 'x-idempotency-lookup').toLowerCase();
      const completedReceipt = this.client.receipt(call, result.response, decoded.id);
      if (lookup) completedReceipt.metadata = { idempotencyLookup: lookup };
      return newMutationBranch('created', { ticket: decoded, ...(lookup === 'hit' ? { wasIdempotentReplay: true } : {}) }, completedReceipt);
    } catch (error) { return mutationInvalid('createTicket', zeroCreate, receipt, 'Zendesk returned an invalid created ticket', error); }
  }
}

/** Read, reconcile, and safely update one ticket. */
export class UpdateTicketOperation implements Mutation<UpdateTicketInput, UpdateTicketOutput> {
  /** Bind the authenticated provider client. */ constructor(private readonly client: ZendeskClient) {}
  /** Return the manifest definition. */ definition() { return UpdateTicketDefinition; }
  /** Use the stable connector Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, check audit metadata, and write only changed fields. */
  async invoke(call: Call, input: UpdateTicketInput): Promise<MutationAttempt<UpdateTicketOutput>> {
    try { validateUpdate(input); } catch (error) { return mutationDefect('updateTicket', zeroUpdate, error); }
    if (!call.idempotencyKey) return mutationDefect('updateTicket', zeroUpdate, 'connector call has no idempotency key', 'LOCAL_DEFECT');
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return mutationDefect('updateTicket', zeroUpdate, 'connection credentials are unavailable', 'AUTHENTICATION');
    const path = `/tickets/${input.ticketId}`;
    const read = await this.client.exchange(call, credentials, 'updateTicket', 'GET', path);
    let receipt = this.client.receipt(call, read.response, input.ticketId);
    const readTerminal = mutationExchange(read, receipt, zeroUpdate);
    if (readTerminal) return readTerminal;
    let current: { ticket: Ticket; updatedStamp: string };
    try { current = decodeTicketEnvelope(read.response.body, this.client.agentTicketURL); if (current.ticket.id !== input.ticketId) throw new Error('ticket response is for another ticket'); }
    catch (error) { return mutationInvalid('updateTicket', zeroUpdate, receipt, 'Zendesk returned an invalid ticket', error); }
    const currentOutput = { ticket: current.ticket };
    if (input.comment || input.expectedUpdatedAt) {
      const audits = await this.client.exchange(call, credentials, 'updateTicket', 'GET', `${path}/audits`, { sort_order: 'desc' });
      receipt = this.client.receipt(call, audits.response, input.ticketId);
      const auditTerminal = mutationExchange(audits, receipt, currentOutput);
      if (auditTerminal) return auditTerminal;
      try {
        const page = objectJSON(audits.response.body);
        const hasEarlierChange = array(page.audits).some(raw => { const audit = object(raw); const metadata = audit.metadata === undefined ? {} : object(audit.metadata); const custom = metadata.custom === undefined ? {} : object(metadata.custom); return custom.dex_idempotency_key === call.idempotencyKey; });
        if (hasEarlierChange) return newMutationBranch('updated', { ticket: current.ticket, wasAlreadyApplied: true }, receipt);
      } catch (error) { return mutationInvalid('updateTicket', currentOutput, receipt, 'Zendesk returned an invalid audit page', error); }
    }
    if (input.expectedUpdatedAt && parseRFC3339(input.expectedUpdatedAt).getTime() !== parseRFC3339(current.ticket.updatedAt).getTime()) return newMutationBranch('conflict', currentOutput, receipt, zendeskFailure('updateTicket', 'CONFLICT', `ticket was updated at ${current.ticket.updatedAt}, after expectedUpdatedAt`));
    const change: Record<string, unknown> = { metadata: { dex_idempotency_key: call.idempotencyKey }, safe_update: true, updated_stamp: current.updatedStamp };
    if (input.status && input.status !== current.ticket.status) change.status = input.status;
    if (input.priority && input.priority !== current.ticket.priority) change.priority = input.priority;
    if (input.assigneeId && input.assigneeId !== current.ticket.assigneeId) change.assignee_id = input.assigneeId;
    if (input.groupId && input.groupId !== current.ticket.groupId) change.group_id = input.groupId;
    const tags = (current.ticket.tags ?? []).filter(tag => !input.removeTags?.includes(tag));
    for (const tag of input.addTags ?? []) if (!tags.includes(tag)) tags.push(tag);
    if (JSON.stringify(tags) !== JSON.stringify(current.ticket.tags ?? [])) change.tags = tags;
    if (input.comment) change.comment = { body: input.comment.body, public: !input.comment.isInternalNote };
    if (Object.keys(change).length === 3) return newMutationBranch('updated', { ticket: current.ticket, wasAlreadyApplied: true }, receipt);
    const write = await this.client.exchange(call, credentials, 'updateTicket', 'PUT', path, undefined, { ticket: change });
    receipt = this.client.receipt(call, write.response, input.ticketId);
    const writeTerminal = mutationExchange(write, receipt, currentOutput);
    if (writeTerminal) return writeTerminal;
    try { const decoded = decodeTicketEnvelope(write.response.body, this.client.agentTicketURL).ticket; if (decoded.id !== input.ticketId) throw new Error('ticket response is for another ticket'); return newMutationBranch('updated', { ticket: decoded }, receipt); }
    catch (error) { return mutationInvalid('updateTicket', currentOutput, receipt, 'Zendesk returned an invalid updated ticket', error); }
  }
}

/** Build the exact Zendesk export search query from typed filters. */
export function buildTicketSearchQuery(input: SearchTicketsInput): string {
  const terms: string[] = [];
  const seen = new Set<string>();
  for (const status of input.statuses ?? []) { if (!statuses.has(status) || seen.has(status)) throw new Error('statuses contains an invalid or duplicate Zendesk status'); seen.add(status); terms.push(`status:${status}`); }
  if (input.requesterEmail) { if (!isBareEmailAddress(input.requesterEmail)) throw new Error('requesterEmail must be one bare email address'); terms.push(`requester:${input.requesterEmail}`); }
  validateTags('tags', input.tags);
  for (const tag of input.tags ?? []) terms.push(`tags:${tag}`);
  if (input.updatedSince) terms.push(`updated>=${formatGoUTC(input.updatedSince)}`);
  const words = (input.text ?? '').trim().split(/\s+/u).filter(Boolean);
  if (words.length > 32) throw new Error('text has more than 32 keywords');
  for (const word of words) { if (/[:<>="*]/.test(word) || /^[+-]/.test(word)) throw new Error('text keyword contains a search operator'); terms.push(word); }
  if (!terms.length) throw new Error('at least one search filter is required');
  return terms.join(' ');
}

function validateSearchPage(input: SearchTicketsInput): void { if (!Number.isInteger(input.pageSize ?? 0) || (input.pageSize ?? 0) < 0 || (input.pageSize ?? 0) > 100) throw new Error('pageSize is invalid'); if (Buffer.byteLength(input.cursor ?? '', 'utf8') > 2048 || /[^\x21-\x7e]/.test(input.cursor ?? '')) throw new Error('cursor is invalid'); }
function validateCreate(input: CreateTicketInput): void { if (!input.subject?.trim()) throw new Error('subject is required'); validateComment(input.comment); if (input.requester && !isBareEmailAddress(input.requester.email)) throw new Error('requester.email must be one bare email address'); if (input.priority && !priorities.has(input.priority)) throw new Error('priority is invalid'); if (input.type && !types.has(input.type)) throw new Error('type is invalid'); validateTags('tags', input.tags); for (const id of [input.groupId, input.assigneeId, input.brandId]) if (id !== undefined && (!Number.isSafeInteger(id) || id < 0)) throw new Error('ID cannot be negative'); }
function validateUpdate(input: UpdateTicketInput): void { if (!validID(input.ticketId)) throw new Error('ticketId must be positive'); if (input.status && !statuses.has(input.status)) throw new Error('status is invalid'); if (input.priority && !priorities.has(input.priority)) throw new Error('priority is invalid'); for (const id of [input.assigneeId, input.groupId]) if (id !== undefined && (!Number.isSafeInteger(id) || id < 0)) throw new Error('ID cannot be negative'); validateTags('addTags', input.addTags); validateTags('removeTags', input.removeTags); if (input.addTags?.some(tag => input.removeTags?.includes(tag))) throw new Error('tag is in both addTags and removeTags'); if (input.comment) validateComment(input.comment); if (input.expectedUpdatedAt) parseRFC3339(input.expectedUpdatedAt); if (!(input.status || input.priority || input.assigneeId || input.groupId || input.addTags?.length || input.removeTags?.length || input.comment)) throw new Error('at least one change is required'); }
function validateTags(_field: string, tags?: string[]): void { const seen = new Set<string>(); for (const tag of tags ?? []) { if (!/^[a-z0-9][a-z0-9_./-]{0,79}$/.test(tag) || seen.has(tag)) throw new Error('tag is invalid or duplicate'); seen.add(tag); } }
function validateComment(comment: { body: string }): void { if (!comment?.body?.trim()) throw new Error('comment.body is required'); }
function validID(value: number): boolean { return Number.isSafeInteger(value) && value > 0; }
function parseRFC3339(value: string): Date {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts) throw new Error('timestamp must be RFC 3339');
  const year = Number(parts[1]), month = Number(parts[2]), day = Number(parts[3]);
  const hour = Number(parts[4]), minute = Number(parts[5]), second = Number(parts[6]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1]! || hour > 23 || minute > 59 || second > 59) throw new Error('timestamp must be RFC 3339');
  const offset = parts[8]!;
  const offsetHours = offset === 'Z' ? 0 : Number(offset.slice(1, 3));
  const offsetMinutes = offset === 'Z' ? 0 : Number(offset.slice(4, 6));
  if (offsetMinutes > 59) throw new Error('timestamp must be RFC 3339');
  const local = new Date(0);
  local.setUTCFullYear(year, month - 1, day);
  local.setUTCHours(hour, minute, second, Number((parts[7] ?? '').padEnd(3, '0').slice(0, 3)));
  const offsetMilliseconds = (offsetHours * 60 + offsetMinutes) * 60_000 * (offset.startsWith('-') ? -1 : 1);
  return new Date(local.getTime() - offsetMilliseconds);
}
function formatGoUTC(value: string): string {
  const date = parseRFC3339(value);
  const fraction = /[.,](\d+)(?:Z|[+-]\d{2}:\d{2})$/.exec(value)?.[1] ?? '';
  const nanos = fraction.slice(0, 9).padEnd(9, '0').replace(/0+$/, '');
  return `${date.toISOString().slice(0, 19)}${nanos ? `.${nanos}` : ''}Z`;
}
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object'); return value as Record<string, unknown>; }
function objectJSON(body: Uint8Array): Record<string, unknown> { return object(JSON.parse(Buffer.from(body).toString('utf8')) as unknown); }
function array(value: unknown): unknown[] { if (!Array.isArray(value)) throw new Error('expected array'); return value; }
function optionalString(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('expected string'); return value; }
function optionalNumber(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error('expected integer'); return value; }
function optionalBoolean(value: unknown): boolean { if (value === undefined || value === null) return false; if (typeof value !== 'boolean') throw new Error('expected boolean'); return value; }
function optionalStringArray(value: unknown): string[] { if (value === undefined || value === null) return []; return array(value).map(item => { if (typeof item !== 'string') throw new Error('expected string'); return item; }); }
function optionalTime(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error('expected time string'); return value; }
function truncateUTF8(value: string): { value: string; truncated: boolean } { const data = Buffer.from(value, 'utf8'); if (data.length <= 16384) return { value, truncated: false }; return { value: data.subarray(0, 16384).toString('utf8').replace(/\uFFFD$/, ''), truncated: true }; }
function decodeTicket(wire: Record<string, unknown>, agentTicketURL: string): { ticket: Ticket; updatedStamp: string } {
  const id = optionalNumber(wire.id); const status = optionalString(wire.status); if (id < 1 || !status) throw new Error('ticket ID or status is missing');
  const createdStamp = optionalTime(wire.created_at); const updatedStamp = optionalTime(wire.updated_at);
  const createdAt = formatGoUTC(createdStamp); const updatedAt = formatGoUTC(updatedStamp);
  const description = truncateUTF8(optionalString(wire.description));
  const channel = wire.via === undefined || wire.via === null ? '' : (() => { const via = object(wire.via); return via.channel === undefined ? '' : typeof via.channel === 'string' ? via.channel : JSON.stringify(via.channel).trim(); })();
  const ticket: Ticket = { id, agentUrl: `${agentTicketURL}${id}`, status, createdAt, updatedAt,
    ...(optionalString(wire.subject) ? { subject: optionalString(wire.subject) } : {}), ...(description.value ? { description: description.value } : {}),
    ...(description.truncated ? { isDescriptionTruncated: true } : {}),
    ...(optionalNumber(wire.custom_status_id) ? { customStatusId: optionalNumber(wire.custom_status_id) } : {}),
    ...(optionalString(wire.priority) ? { priority: optionalString(wire.priority) } : {}), ...(optionalString(wire.type) ? { type: optionalString(wire.type) } : {}),
    ...(optionalNumber(wire.requester_id) ? { requesterId: optionalNumber(wire.requester_id) } : {}), ...(optionalNumber(wire.submitter_id) ? { submitterId: optionalNumber(wire.submitter_id) } : {}),
    ...(optionalNumber(wire.assignee_id) ? { assigneeId: optionalNumber(wire.assignee_id) } : {}), ...(optionalNumber(wire.group_id) ? { groupId: optionalNumber(wire.group_id) } : {}),
    ...(optionalNumber(wire.organization_id) ? { organizationId: optionalNumber(wire.organization_id) } : {}), ...(optionalNumber(wire.brand_id) ? { brandId: optionalNumber(wire.brand_id) } : {}),
    ...(wire.tags === undefined || wire.tags === null ? {} : { tags: optionalStringArray(wire.tags) }),
    ...(optionalString(wire.external_id) ? { externalId: optionalString(wire.external_id) } : {}), ...(channel ? { channel } : {}),
    ...(optionalTime(wire.due_at) ? { dueAt: formatGoUTC(optionalTime(wire.due_at)) } : {}) };
  return { ticket, updatedStamp };
}
function decodeTicketEnvelope(body: Uint8Array, agentTicketURL: string): { ticket: Ticket; updatedStamp: string } { return decodeTicket(object(objectJSON(body).ticket), agentTicketURL); }
function decodeUser(wire: Record<string, unknown>): TicketUser { return { id: optionalNumber(wire.id), ...(optionalString(wire.name) ? { name: optionalString(wire.name) } : {}), ...(optionalString(wire.email) ? { email: optionalString(wire.email) } : {}), ...(optionalString(wire.role) ? { role: optionalString(wire.role) } : {}) }; }
function decodeComment(wire: Record<string, unknown>): TicketComment { const id = optionalNumber(wire.id); if (id < 1) throw new Error('comment ID is missing'); const body = truncateUTF8(optionalString(wire.plain_body) || optionalString(wire.body)); return { id, isPublic: optionalBoolean(wire.public), body: body.value, createdAt: formatGoUTC(optionalTime(wire.created_at)), ...(optionalNumber(wire.author_id) ? { authorId: optionalNumber(wire.author_id) } : {}), ...(body.truncated ? { isBodyTruncated: true } : {}) }; }
function queryExchange<T>(result: ZendeskExchange, receipt: ReceiptDraft, zero: T): QueryAttempt<T> | undefined { if (result.outcome === 'success') return undefined; if (result.outcome === 'retry') return newQueryRetry(result.failure, result.retryAfterMs); const branch = result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected'; return newQueryBranch(branch, zero, receipt, result.failure); }
function mutationExchange<T>(result: ZendeskExchange, receipt: ReceiptDraft, zero: T): MutationAttempt<T> | undefined { if (result.outcome === 'success') return undefined; if (result.outcome === 'retry') return newMutationRetry(result.failure, result.retryAfterMs); const branch = result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected'; return newMutationBranch(branch, zero, receipt, result.failure); }
function queryDefect<T>(operation: string, zero: T, error: unknown, kind: Failure['kind'] = 'VALIDATION'): QueryAttempt<T> { return newQueryBranch('defect', zero, zeroReceipt(), zendeskFailure(operation, kind, String(error instanceof Error ? error.message : error))); }
function mutationDefect<T>(operation: string, zero: T, error: unknown, kind: Failure['kind'] = 'VALIDATION'): MutationAttempt<T> { return newMutationBranch('defect', zero, zeroReceipt(), zendeskFailure(operation, kind, String(error instanceof Error ? error.message : error))); }
function queryInvalid<T>(operation: string, zero: T, receipt: ReceiptDraft, message: string, error: unknown): QueryAttempt<T> { return newQueryBranch('invalidResponse', zero, receipt, zendeskFailure(operation, 'PROTOCOL', `${message}: ${error instanceof Error ? error.message : String(error)}`)); }
function mutationInvalid<T>(operation: string, zero: T, receipt: ReceiptDraft, message: string, error: unknown): MutationAttempt<T> { return newMutationBranch('invalidResponse', zero, receipt, zendeskFailure(operation, 'PROTOCOL', `${message}: ${error instanceof Error ? error.message : String(error)}`)); }
