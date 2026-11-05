// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { ZohoDeskClient, ExchangeFailure, ExchangeResult } from './client.js';
import { SearchTicketsDefinition, GetTicketDefinition, CreateTicketDefinition, UpdateTicketDefinition, AddCommentDefinition } from './generated/definitions.js';
import type { SearchTicketsInput, SearchTicketsOutput, GetTicketInput, TicketDetails, CreateTicketInput, CreateTicketOutput, UpdateTicketInput, UpdateTicketOutput, AddCommentInput, AddCommentOutput, Ticket } from './generated/models.js';
import { count, decodeComment, decodeList, decodeObject, decodeThread, decodeTicket, timestamp } from './ticket.js';

const zeroTime = '0001-01-01T00:00:00Z';
const zeroTicket: Ticket = { id: '', status: '', createdAt: zeroTime, modifiedAt: zeroTime };
const zeroSearch: SearchTicketsOutput = { tickets: [], totalMatched: 0 };
const zeroDetails: TicketDetails = { ticket: zeroTicket, threads: [], comments: [] };
const zeroCreated: CreateTicketOutput = { ticket: zeroTicket };
const zeroUpdated: UpdateTicketOutput = { ticket: zeroTicket };
const zeroAdded: AddCommentOutput = { comment: { id: '', isPublic: false, content: '', commentedAt: zeroTime } };
const numericId = /^[0-9]{1,20}$/;

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'zoho-desk', operation, message }; }
function validation(operation: string, message: string): Failure { return failure(operation, 'VALIDATION', message); }
function exchangeReceipt(client: ZohoDeskClient, call: Call, result: ExchangeResult, objectId = ''): ReceiptDraft {
  return client.receipt(call, 'response' in result ? result.response : result.outcome.response, objectId);
}
function queryOutcome<T>(result: ExchangeFailure, zero: T, receipt: ReceiptDraft, rejected = 'providerRejected', notFound = rejected): QueryAttempt<T> {
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.disposition)) return newQueryRetry(result.failure, result.retryAfterMs);
  return newQueryBranch(result.disposition === 'notFound' ? notFound : result.disposition === 'invalid' ? 'invalidResponse' : result.disposition === 'defect' ? 'defect' : rejected, zero, receipt, result.failure);
}
function mutationOutcome<T>(result: ExchangeFailure, zero: T, receipt: ReceiptDraft, rejected = 'providerRejected', notFound = rejected): MutationAttempt<T> {
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.disposition)) return newMutationRetry(result.failure, result.retryAfterMs);
  return newMutationBranch(result.disposition === 'notFound' ? notFound : result.disposition === 'invalid' ? 'invalidResponse' : result.disposition === 'defect' ? 'defect' : rejected, zero, receipt, result.failure);
}
function sessionFailure<T>(result: ExchangeFailure, zero: T, isMutation = false): QueryAttempt<T> | MutationAttempt<T> {
  const branch = result.disposition === 'rejected' ? 'providerRejected' : result.disposition === 'defect' ? 'defect' : '';
  if (!branch) return isMutation ? newMutationRetry(result.failure) : newQueryRetry(result.failure);
  return isMutation ? newMutationBranch(branch, zero, {}, result.failure) : newQueryBranch(branch, zero, {}, result.failure);
}
function validateId(name: string, value: string | undefined, optional = false): void {
  if (optional && !value) return;
  if (!value || !numericId.test(value)) throw new Error(`${name} must be a numeric Zoho Desk ID`);
}
function validatePicklist(name: string, value: string): void {
  if (!value || value.trim() !== value || [...value].length > 120 || /[,*${}\p{Cc}\p{Cf}]/u.test(value)) throw new Error(`${name} must be 1 to 120 characters without surrounding spaces or search wildcards`);
}
function validateText(name: string, value: string, limit: number): void {
  if (!value?.trim()) throw new Error(`${name} is required`);
  if ([...value].length > limit) throw new Error(`${name} is longer than Zoho Desk's ${limit}-character limit`);
}
function validateEmail(value: string): boolean {
  // Zoho Desk disallows search metacharacters even when an RFC 5322 parser could accept them.
  if (!value || /[ "'()<>;,*${}]/.test(value)) return false;
  const at = value.lastIndexOf('@');
  if (at <= 0 || at === value.length - 1 || value.indexOf('@') !== at) return false;
  const local = value.slice(0, at), domain = value.slice(at + 1);
  return [...local].every(character => !/\s/u.test(character) && character.codePointAt(0)! > 31 && character.codePointAt(0)! !== 127) && (/^(?:[^.\s]+\.)*[^.\s]+$/u.test(domain) || /^\[[^\]\s]+\]$/.test(domain));
}
export function goPathEscape(value: string): string {
  return encodeURIComponent(value)
    .replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%(24|26|2B|3D|3A|40)/gi, encoded => String.fromCharCode(parseInt(encoded.slice(1), 16)));
}
function ticketPath(id: string): string { return `/tickets/${goPathEscape(id)}`; }

/** Search a single Zoho Desk ticket page. */
export class SearchTicketsOperation implements Query<SearchTicketsInput, SearchTicketsOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return SearchTicketsDefinition; }
  /** Validate filters, query one page, and filter exact contact addresses. */
  async invoke(call: Call, input: SearchTicketsInput): Promise<QueryAttempt<SearchTicketsOutput>> {
    const operation = 'searchTickets';
    let query: Record<string, string>;
    try { query = buildSearchQuery(input, this.client.currentTime()); }
    catch (error) { return newQueryBranch('defect', zeroSearch, {}, validation(operation, errorMessage(error))); }
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) return sessionFailure(session, zeroSearch) as QueryAttempt<SearchTicketsOutput>;
    const result = await this.client.exchange(call, operation, session, 'GET', '/tickets/search', query);
    const receipt = exchangeReceipt(this.client, call, result);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroSearch, receipt);
    try {
      if (result.response.status === 204) return newQueryBranch('searched', zeroSearch, receipt);
      const page = decodeObject(result.response.body);
      if (!Array.isArray(page.data)) throw new Error('search page has no data array');
      const limit = Number(query.limit);
      if (page.data.length > limit) throw new Error('search page holds more tickets than requested');
      const count = parseCount(page.count);
      const tickets = page.data.map(item => {
        if (!item || typeof item !== 'object' || Array.isArray(item)) throw new Error('search ticket is invalid');
        const decoded = decodeTicket(item as Record<string, unknown>);
        const ticket = { ...decoded.ticket };
        delete ticket.descriptionHtml;
        delete ticket.isDescriptionTruncated;
        return { ticket, contactEmail: decoded.contact?.email ?? '' };
      }).filter(item => !input.contactEmail || item.ticket.email?.toLowerCase() === input.contactEmail.toLowerCase() || item.contactEmail.toLowerCase() === input.contactEmail.toLowerCase()).map(item => item.ticket);
      return newQueryBranch('searched', { tickets, totalMatched: count, ...(page.data.length === limit && (input.from ?? 0) + limit < 5000 ? { nextFrom: (input.from ?? 0) + limit } : {}) }, receipt);
    } catch (error) { return newQueryBranch('invalidResponse', zeroSearch, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid search page: ${errorMessage(error)}`)); }
  }
}

/** Read a ticket and its bounded newest conversation items. */
export class GetTicketOperation implements Query<GetTicketInput, TicketDetails> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return GetTicketDefinition; }
  /** Read the ticket, threads, and comments in one attempt. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<TicketDetails>> {
    const operation = 'getTicket';
    try { validateId('ticketId', input.ticketId); if ((input.threadLimit ?? 0) < 0 || (input.threadLimit ?? 0) > 20 || (input.commentLimit ?? 0) < 0 || (input.commentLimit ?? 0) > 20) throw new Error('threadLimit and commentLimit must be 1 to 20 or zero'); }
    catch (error) { return newQueryBranch('defect', zeroDetails, {}, validation(operation, errorMessage(error))); }
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) return sessionFailure(session, zeroDetails) as QueryAttempt<TicketDetails>;
    const first = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId), { include: 'contacts' });
    let receipt = exchangeReceipt(this.client, call, first, input.ticketId);
    if ('outcome' in first) return queryOutcome(first.outcome, zeroDetails, receipt, 'providerRejected', 'notFound');
    let decoded: ReturnType<typeof decodeTicket>;
    try { decoded = decodeTicket(decodeObject(first.response.body), input.ticketId); }
    catch (error) { return newQueryBranch('invalidResponse', zeroDetails, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid ticket: ${errorMessage(error)}`)); }
    const threadLimit = input.threadLimit || 10;
    const threadsResponse = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId) + '/threads', { from: '0', limit: String(threadLimit), sortBy: '-sendDateTime' });
    receipt = exchangeReceipt(this.client, call, threadsResponse, input.ticketId);
    if ('outcome' in threadsResponse) return queryOutcome(threadsResponse.outcome, zeroDetails, receipt, 'providerRejected', 'notFound');
    let threads: TicketDetails['threads'];
    try { threads = decodeList(threadsResponse.response.body, threadsResponse.response.status, threadLimit).map(decodeThread).sort((left, right) => right.createdAt.localeCompare(left.createdAt)); }
    catch (error) { return newQueryBranch('invalidResponse', zeroDetails, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid thread list: ${errorMessage(error)}`)); }
    const commentLimit = input.commentLimit || 5;
    const commentsResponse = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId) + '/comments', { from: '0', limit: String(commentLimit), sortBy: '-commentedTime' });
    receipt = exchangeReceipt(this.client, call, commentsResponse, input.ticketId);
    if ('outcome' in commentsResponse) return queryOutcome(commentsResponse.outcome, zeroDetails, receipt, 'providerRejected', 'notFound');
    try {
      const comments = decodeList(commentsResponse.response.body, commentsResponse.response.status, commentLimit).map(decodeComment).sort((left, right) => right.commentedAt.localeCompare(left.commentedAt));
      const details: TicketDetails = { ticket: decoded.ticket, ...(decoded.contact ? { contact: decoded.contact } : {}), threads, comments,
        ...(decoded.ticket.threadCount && decoded.ticket.threadCount > threads.length ? { hasMoreThreads: true } : {}),
        ...(decoded.ticket.commentCount && decoded.ticket.commentCount > comments.length ? { hasMoreComments: true } : {}) };
      return newQueryBranch('found', details, receipt);
    } catch (error) { return newQueryBranch('invalidResponse', zeroDetails, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid comment list: ${errorMessage(error)}`)); }
  }
}

/** Create one ticket after a durable dispatch checkpoint. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return CreateTicketDefinition; }
  /** Correlate the call without claiming provider deduplication. */ idempotencyKey(callId: string): string { return callId; }
  /** Send no more than one ticket creation per Step execution. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    const operation = 'createTicket';
    let body: Record<string, unknown>;
    try { body = buildCreateBody(input); }
    catch (error) { return newMutationBranch('defect', zeroCreated, {}, validation(operation, errorMessage(error))); }
    if (hasEarlierDispatch(call)) return previousDispatch(operation, zeroCreated, this.client.receipt(call));
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) return sessionFailure(session, zeroCreated, true) as MutationAttempt<CreateTicketOutput>;
    const checkpoint = await recordDispatch(call, operation);
    if (checkpoint) return checkpoint as MutationAttempt<CreateTicketOutput>;
    const result = await this.client.exchange(call, operation, session, 'POST', '/tickets', undefined, body);
    const receipt = exchangeReceipt(this.client, call, result);
    if ('outcome' in result) return singleDispatchOutcome(call, result.outcome, zeroCreated, receipt);
    try { const ticket = decodeTicket(decodeObject(result.response.body)).ticket; return newMutationBranch('created', { ticket }, this.client.receipt(call, result.response, ticket.id)); }
    catch (error) { return newMutationUncertain(zeroCreated, failure(operation, 'PROTOCOL', `Zoho Desk accepted the ticket but returned an invalid ticket: ${errorMessage(error)}`), receipt); }
  }
}

/** Set absolute ticket values after reading current values. */
export class UpdateTicketOperation implements Mutation<UpdateTicketInput, UpdateTicketOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return UpdateTicketDefinition; }
  /** Correlate the call; absolute values make repetition safe. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, compare, and conditionally patch one ticket. */
  async invoke(call: Call, input: UpdateTicketInput): Promise<MutationAttempt<UpdateTicketOutput>> {
    const operation = 'updateTicket';
    try { validateId('ticketId', input.ticketId); for (const value of [input.status, input.priority]) if (value) validatePicklist('status or priority', value); validateId('assigneeId', input.assigneeId, true); validateId('departmentId', input.departmentId, true); if (!input.status && !input.priority && !input.assigneeId && !input.departmentId) throw new Error('at least one of status, priority, assigneeId, or departmentId is required'); }
    catch (error) { return newMutationBranch('defect', zeroUpdated, {}, validation(operation, errorMessage(error))); }
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) return sessionFailure(session, zeroUpdated, true) as MutationAttempt<UpdateTicketOutput>;
    const read = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId));
    let receipt = exchangeReceipt(this.client, call, read, input.ticketId);
    if ('outcome' in read) return mutationOutcome(read.outcome, zeroUpdated, receipt, 'providerRejected', 'notFound');
    let current: Ticket;
    try { current = decodeTicket(decodeObject(read.response.body), input.ticketId).ticket; }
    catch (error) { return newMutationBranch('invalidResponse', zeroUpdated, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid ticket: ${errorMessage(error)}`)); }
    const change: Record<string, string> = {};
    if (input.status && input.status.toLowerCase() !== current.status.toLowerCase()) change.status = input.status;
    if (input.priority && input.priority.toLowerCase() !== (current.priority ?? '').toLowerCase()) change.priority = input.priority;
    if (input.departmentId && input.departmentId !== current.departmentId) change.departmentId = input.departmentId;
    if (input.assigneeId && (input.assigneeId !== current.assigneeId || change.departmentId)) change.assigneeId = input.assigneeId;
    if (!Object.keys(change).length) return newMutationBranch('updated', { ticket: current, wasAlreadyApplied: true }, receipt);
    const write = await this.client.exchange(call, operation, session, 'PATCH', ticketPath(input.ticketId), undefined, change);
    receipt = exchangeReceipt(this.client, call, write, input.ticketId);
    if ('outcome' in write) return mutationOutcome(write.outcome, { ticket: current }, receipt, 'providerRejected', 'notFound');
    try { return newMutationBranch('updated', { ticket: decodeTicket(decodeObject(write.response.body), input.ticketId).ticket }, receipt); }
    catch (error) { return newMutationBranch('invalidResponse', { ticket: current }, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid updated ticket: ${errorMessage(error)}`)); }
  }
}

/** Add a public or private plain-text ticket comment once. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return AddCommentDefinition; }
  /** Correlate the call without claiming provider deduplication. */ idempotencyKey(callId: string): string { return callId; }
  /** Checkpoint dispatch before sending the non-idempotent comment. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const operation = 'addComment';
    try { validateId('ticketId', input.ticketId); validateText('content', input.content, 32_000); }
    catch (error) { return newMutationBranch('defect', zeroAdded, {}, validation(operation, errorMessage(error))); }
    if (hasEarlierDispatch(call)) return previousDispatch(operation, zeroAdded, this.client.receipt(call));
    const session = await this.client.startSession(call, operation);
    if ('disposition' in session) return sessionFailure(session, zeroAdded, true) as MutationAttempt<AddCommentOutput>;
    const checkpoint = await recordDispatch(call, operation);
    if (checkpoint) return checkpoint as MutationAttempt<AddCommentOutput>;
    const result = await this.client.exchange(call, operation, session, 'POST', ticketPath(input.ticketId) + '/comments', undefined, { content: input.content, isPublic: input.isPublic ?? false, contentType: 'plainText' });
    const receipt = exchangeReceipt(this.client, call, result);
    if ('outcome' in result) return singleDispatchOutcome(call, result.outcome, zeroAdded, receipt, 'notFound');
    try {
      const comment = decodeComment(decodeObject(result.response.body));
      if (comment.isPublic !== (input.isPublic ?? false)) throw new Error('comment response has another visibility than requested');
      return newMutationBranch('added', { comment }, this.client.receipt(call, result.response, comment.id));
    } catch (error) { return newMutationUncertain(zeroAdded, failure(operation, 'PROTOCOL', `Zoho Desk accepted the comment but returned an invalid comment: ${errorMessage(error)}`), receipt); }
  }
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid value'; }
function parseCount(value: unknown): number {
  return count(value);
}
function buildSearchQuery(input: SearchTicketsInput, now: Date): Record<string, string> {
  if (input.statuses?.length && input.statusTypes?.length) throw new Error('set statuses or statusTypes, not both');
  const query: Record<string, string> = {};
  const terms = input.statuses?.length ? input.statuses : input.statusTypes?.map(type => {
    const token = ({ Open: '${OPEN}', 'On Hold': '${ONHOLD}', Closed: '${CLOSED}' } as Record<string, string>)[type];
    if (!token) throw new Error('statusTypes entry must be Open, On Hold, or Closed');
    return token;
  });
  if (terms?.length) { if (input.statuses?.length) terms.forEach(value => validatePicklist('statuses entry', value)); distinct(terms, 'statuses'); query.status = terms.join(','); }
  if (input.priorities?.length) { input.priorities.forEach(value => validatePicklist('priorities entry', value)); distinct(input.priorities, 'priorities'); query.priority = input.priorities.join(','); }
  if (input.contactEmail) { if (!validateEmail(input.contactEmail)) throw new Error('contactEmail must be one bare email address such as jane@example.com'); query.email = input.contactEmail; }
  if (input.departmentId) { validateId('departmentId', input.departmentId); query.departmentId = input.departmentId; }
  if (input.modifiedSince) {
    const since = timestamp('modifiedSince', input.modifiedSince);
    if (Date.parse(since) > now.getTime()) throw new Error('modifiedSince cannot be in the future');
    query.modifiedTimeRange = `${formatZohoTime(new Date(since))},${formatZohoTime(now)}`;
  }
  if (!Object.keys(query).length) throw new Error('at least one of statuses, statusTypes, priorities, contactEmail, departmentId, or modifiedSince is required');
  const from = input.from ?? 0, limit = input.limit ?? 0;
  if (!Number.isSafeInteger(from) || from < 0 || from >= 5000 || !Number.isSafeInteger(limit) || limit < 0 || limit > 100) throw new Error('from must be 0 to 4999, and limit 1 to 100 or zero for 25');
  query.from = String(from); query.limit = String(Math.min(limit || 25, 5000 - from)); query.sortBy = '-modifiedTime';
  return query;
}
function distinct(values: readonly string[], name: string): void { const seen = new Set<string>(); for (const value of values) { const folded = value.toLowerCase(); if (seen.has(folded)) throw new Error(`${name} lists ${JSON.stringify(value)} twice`); seen.add(folded); } }
function formatZohoTime(value: Date): string { return value.toISOString().slice(0, 23) + 'Z'; }
function buildCreateBody(input: CreateTicketInput): Record<string, unknown> {
  validateText('subject', input.subject, 255); validateText('description', input.description, 65_535); validateId('departmentId', input.departmentId);
  const contact = input.contact ?? {};
  if (!!input.contactId === !!contact.email) throw new Error('set exactly one of contactId and contact.email');
  if (input.contactId) { validateId('contactId', input.contactId); if (contact.firstName || contact.lastName) throw new Error('contact names apply only with contact.email'); }
  if (contact.email && !validateEmail(contact.email)) throw new Error('contact.email must be one bare email address such as jane@example.com');
  for (const name of [contact.firstName ?? '', contact.lastName ?? '']) if (name.trim() !== name || [...name].length > 80) throw new Error('contact name must be at most 80 characters without surrounding spaces');
  if (input.status) validatePicklist('status', input.status);
  if (input.priority) validatePicklist('priority', input.priority);
  validateId('assigneeId', input.assigneeId, true);
  const html = input.description.replaceAll('\r\n', '\n').replaceAll('&', '&amp;').replaceAll('"', '&#34;').replaceAll("'", '&#39;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('\n', '<br>');
  if ([...html].length > 65_535) throw new Error("description is longer than Zoho Desk's 65,535-character limit once escaped as HTML");
  return { subject: input.subject, description: html, departmentId: input.departmentId,
    ...(input.contactId ? { contactId: input.contactId } : {}),
    ...(contact.email ? { contact: { email: contact.email, ...(contact.firstName ? { firstName: contact.firstName } : {}), ...(contact.lastName ? { lastName: contact.lastName } : {}) }, email: contact.email } : {}),
    ...(input.status ? { status: input.status } : {}), ...(input.priority ? { priority: input.priority } : {}), ...(input.assigneeId ? { assigneeId: input.assigneeId } : {}) };
}
function hasEarlierDispatch(call: Call): boolean {
  try { return call.context.hasLastHeartbeatValue(); } catch { return true; }
}
function previousDispatch<T>(operation: string, zero: T, receipt: ReceiptDraft): MutationAttempt<T> {
  return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'an earlier attempt of this Step may have sent the request, so it is not sent again'), receipt);
}
async function recordDispatch(call: Call, operation: string): Promise<MutationAttempt<unknown> | undefined> {
  try { await call.context.recordHeartbeat({ zohoDeskDispatchedCallId: call.id }); return undefined; }
  catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
}
async function clearDispatch(call: Call): Promise<void> {
  try { await call.context.recordHeartbeat(undefined); } catch { /* A failed clear preserves uncertainty on the next attempt. */ }
}
async function singleDispatchOutcome<T>(call: Call, outcome: ExchangeFailure, zero: T, receipt: ReceiptDraft, notFound = 'providerRejected'): Promise<MutationAttempt<T>> {
  if (outcome.disposition === 'rateLimited' || outcome.disposition === 'notSent' || outcome.response?.status === 503) { await clearDispatch(call); return newMutationRetry(outcome.failure, outcome.retryAfterMs); }
  if (outcome.disposition === 'unavailable' || outcome.disposition === 'invalid') return newMutationUncertain(zero, outcome.failure, receipt);
  if (outcome.disposition === 'defect') { await clearDispatch(call); return newMutationBranch('defect', zero, receipt, outcome.failure); }
  return newMutationBranch(outcome.disposition === 'notFound' ? notFound : 'providerRejected', zero, receipt, outcome.failure);
}
