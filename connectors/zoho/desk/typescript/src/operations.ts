// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import type { ZohoDeskClient, ExchangeFailure, ExchangeResult } from './client.js';
import { SearchTicketsDefinition, GetTicketDefinition, CreateTicketDefinition, UpdateTicketDefinition, AddCommentDefinition } from './generated/definitions.js';
import type { SearchTicketsInput, SearchTicketsOutput, GetTicketInput, TicketDetails, CreateTicketInput, CreateTicketOutput, UpdateTicketInput, UpdateTicketOutput, AddCommentInput, AddCommentOutput, Ticket, Credentials } from './generated/models.js';
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
      const page = decodeObject(result.response.body, 'search page is not a JSON object with data and count');
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
    const threadLimit = input.threadLimit || 5;
    const threadsResponse = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId) + '/threads', { from: '0', limit: String(threadLimit), sortBy: '-sendDateTime' });
    receipt = exchangeReceipt(this.client, call, threadsResponse, input.ticketId);
    if ('outcome' in threadsResponse) return queryOutcome(threadsResponse.outcome, zeroDetails, receipt, 'providerRejected', 'notFound');
    let threads: TicketDetails['threads'];
    try { threads = decodeList(threadsResponse.response.body, threadsResponse.response.status, threadLimit).map(decodeThread).sort((left, right) => Date.parse(right.createdAt) - Date.parse(left.createdAt)); }
    catch (error) { return newQueryBranch('invalidResponse', zeroDetails, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid thread list: ${errorMessage(error)}`)); }
    const commentLimit = input.commentLimit || 5;
    const commentsResponse = await this.client.exchange(call, operation, session, 'GET', ticketPath(input.ticketId) + '/comments', { from: '0', limit: String(commentLimit), sortBy: '-commentedTime' });
    receipt = exchangeReceipt(this.client, call, commentsResponse, input.ticketId);
    if ('outcome' in commentsResponse) return queryOutcome(commentsResponse.outcome, zeroDetails, receipt, 'providerRejected', 'notFound');
    try {
      const comments = decodeList(commentsResponse.response.body, commentsResponse.response.status, commentLimit).map(decodeComment).sort((left, right) => Date.parse(right.commentedAt) - Date.parse(left.commentedAt));
      const details: TicketDetails = { ticket: decoded.ticket, ...(decoded.contact ? { contact: decoded.contact } : {}), threads, comments,
        ...(decoded.ticket.threadCount && decoded.ticket.threadCount > threads.length ? { hasMoreThreads: true } : {}),
        ...(decoded.ticket.commentCount && decoded.ticket.commentCount > comments.length ? { hasMoreComments: true } : {}) };
      return newQueryBranch('found', details, receipt);
    } catch (error) { return newQueryBranch('invalidResponse', zeroDetails, receipt, failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid comment list: ${errorMessage(error)}`)); }
  }
}

/** Create one ticket under the SDK single-dispatch fence and send cap. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return CreateTicketDefinition; }
  /** Correlate the call without claiming provider deduplication. */ idempotencyKey(callId: string): string { return callId; }
  /** Send the ticket; resend an unknown outcome only while the send cap allows. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    const operation = 'createTicket';
    let body: Record<string, unknown>;
    try { body = buildCreateBody(input); }
    catch (error) { return newMutationBranch('defect', zeroCreated, {}, validation(operation, errorMessage(error))); }
    return invokeFencedWrite(this.client, call, {
      operation, definition: CreateTicketDefinition, zero: zeroCreated, path: '/tickets', body, notFound: 'providerRejected', successBranch: 'created',
      invalidMessage: 'Zoho Desk accepted the ticket but returned an invalid ticket',
      decode: answer => { const ticket = decodeTicket(decodeObject(answer)).ticket; return { output: { ticket }, providerId: ticket.id }; },
      readBackPath: ticketId => ticketPath(ticketId),
      decodeReadBack: (answer, ticketId) => ({ ticket: decodeTicket(decodeObject(answer), ticketId).ticket }),
    });
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
    // The write is absolute and an earlier attempt may have sent it, so an unusable read retries.
    if ('outcome' in read && read.outcome.disposition === 'invalid') return newMutationRetry(read.outcome.failure, read.outcome.retryAfterMs);
    if ('outcome' in read) return mutationOutcome(read.outcome, zeroUpdated, receipt, 'providerRejected', 'notFound');
    let current: Ticket;
    try { current = decodeTicket(decodeObject(read.response.body), input.ticketId).ticket; }
    catch (error) { return newMutationRetry(failure(operation, 'PROTOCOL', `Zoho Desk returned an invalid ticket: ${errorMessage(error)}`)); }
    const change: Record<string, string> = {};
    if (input.status && input.status.toLowerCase() !== current.status.toLowerCase()) change.status = input.status;
    if (input.priority && input.priority.toLowerCase() !== (current.priority ?? '').toLowerCase()) change.priority = input.priority;
    const hasDepartmentChange = !!input.departmentId && input.departmentId !== current.departmentId;
    if (input.assigneeId && (input.assigneeId !== current.assigneeId || hasDepartmentChange)) change.assigneeId = input.assigneeId;
    if (hasDepartmentChange) change.departmentId = input.departmentId!;
    if (!Object.keys(change).length) return newMutationBranch('updated', { ticket: current, wasAlreadyApplied: true }, receipt);
    const write = await this.client.exchange(call, operation, session, 'PATCH', ticketPath(input.ticketId), undefined, change);
    receipt = exchangeReceipt(this.client, call, write, input.ticketId);
    if ('outcome' in write) {
      const { disposition, response } = write.outcome;
      // The PATCH sets final values, so an unknown outcome repeats it.
      if (disposition === 'invalid' || (disposition === 'rejected' && !definiteRefusalStatuses.has(response?.status ?? 0))) return newMutationRetry(write.outcome.failure, write.outcome.retryAfterMs);
      return mutationOutcome(write.outcome, { ticket: current }, receipt, 'providerRejected', 'notFound');
    }
    try { return newMutationBranch('updated', { ticket: decodeTicket(decodeObject(write.response.body), input.ticketId).ticket }, receipt); }
    catch (error) { return newMutationRetry(failure(operation, 'PROTOCOL', `Zoho Desk accepted the change but returned an invalid updated ticket: ${errorMessage(error)}`)); }
  }
}

/** Add a public or private plain-text ticket comment under the SDK single-dispatch fence and send cap. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZohoDeskClient) {}
  /** Return the generated operation definition. */ definition() { return AddCommentDefinition; }
  /** Correlate the call without claiming provider deduplication. */ idempotencyKey(callId: string): string { return callId; }
  /** Send the comment; resend an unknown outcome only while the send cap allows. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const operation = 'addComment';
    try { validateId('ticketId', input.ticketId); validateText('content', input.content, 32_000); }
    catch (error) { return newMutationBranch('defect', zeroAdded, {}, validation(operation, errorMessage(error))); }
    const isPublic = input.isPublic ?? false;
    return invokeFencedWrite(this.client, call, {
      operation, definition: AddCommentDefinition, zero: zeroAdded, path: ticketPath(input.ticketId) + '/comments',
      body: { content: input.content, isPublic, contentType: 'plainText' }, notFound: 'notFound', successBranch: 'added',
      invalidMessage: 'Zoho Desk accepted the comment but returned an invalid comment',
      decode: answer => { const comment = decodeComment(decodeObject(answer)); return { output: { comment }, providerId: comment.id }; },
      // A read-back would return the same stored visibility.
      rejectAccepted: output => output.comment.isPublic !== isPublic ? commentVisibilityError : '',
      readBackPath: commentId => `${ticketPath(input.ticketId)}/comments/${goPathEscape(commentId)}`,
      decodeReadBack: (answer, commentId) => {
        const comment = decodeComment(decodeObject(answer));
        if (comment.id !== commentId) throw new Error('comment response is for another comment');
        if (comment.isPublic !== isPublic) throw new Error(commentVisibilityError);
        return { comment };
      },
    });
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
/** Only these statuses prove Zoho Desk applied nothing; any other non-2xx answer may have been applied. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
const writeExecuteTimeoutMs = 30_000;
const commentVisibilityError = 'comment response has another visibility than requested';

/** A fence that allows this attempt to send, with its send-cap facts. */
interface WritePermit {
  /** The attempt's fence. */ fence: Fence;
  /** No earlier attempt of this call may have sent. */ isFirstSend: boolean;
  /** This attempt's send leaves room under the cap for one more. */ canResendUnknownOutcome: boolean;
}

/** Reads an accepted write back by provider ID; the flag reports that the ID is checkpointed. */
type ReadBack<T> = (providerId: string, isProviderIdSaved: boolean) => Promise<MutationAttempt<T>>;

/** One fenced POST of createTicket or addComment and how its answer decodes. */
interface FencedWrite<T> {
  operation: string;
  definition: MutationDefinition;
  zero: T;
  path: string;
  body: unknown;
  notFound: string;
  successBranch: string;
  invalidMessage: string;
  /** Decode the 2xx body and return the output and its provider ID. */
  decode: (body: Uint8Array) => { output: T; providerId: string };
  /** Explain why a decoded, accepted output is not the requested write, or return an empty string. */
  rejectAccepted?: (output: T) => string;
  /** The GET path that reads the accepted write back by its provider ID. */
  readBackPath: (providerId: string) => string;
  /** Decode the read-back body of the write with this provider ID. */
  decodeReadBack: (body: Uint8Array, providerId: string) => T;
}

/**
 * Send one write under the SDK single-dispatch fence and the manifest send cap. Zoho Desk has no ticket or comment
 * read-back by Call ID, so an unknown outcome resends while the cap allows and selects uncertain at the cap. A 2xx is
 * never resent: a returned or checkpointed provider ID is read back by ID, and a 2xx without one is uncertain.
 */
async function invokeFencedWrite<T>(client: ZohoDeskClient, call: Call, write: FencedWrite<T>): Promise<MutationAttempt<T>> {
  const { operation, zero } = write;
  const operationMaxSends = definitionMaxSends(write.definition);
  let fence: Fence;
  try { fence = newFence(call, writeExecuteTimeoutMs, { maxSends: operationMaxSends }); }
  catch { return newMutationBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'single-dispatch fence could not start')); }
  const decision = fence.decide();
  const savedProviderId = decision.marker?.providerId ?? '';
  // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
  if (decision.kind === 'failClosed') return newMutationRetry(failure(operation, 'AVAILABILITY', 'the dispatch checkpoint could not be read; nothing was sent in this attempt'));
  if (decision.kind === 'readBackRequired' && !savedProviderId) return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'earlier attempts may have sent the request and the send cap is reached, so it is not sent again'), client.receipt(call));
  const priorSends = decision.priorSendUpperBound ?? 0;
  const permit: WritePermit = { fence, isFirstSend: priorSends === 0 && !savedProviderId, canResendUnknownOutcome: priorSends + 1 < effectiveMaxSends(call, operationMaxSends) };
  const session = await client.startSession(call, operation);
  if ('disposition' in session) return sessionWriteAttempt(permit, session, zero);
  const readBack: ReadBack<T> = (providerId, isProviderIdSaved) => readBackAcceptedWrite(client, call, session, write, providerId, isProviderIdSaved);
  // A saved provider ID proves Zoho Desk accepted the write, so it is read back and never resent.
  if (savedProviderId) return readBack(savedProviderId, true);
  let result: ExchangeResult;
  // Dispatch refused before sending, so nothing reached Zoho Desk in this attempt.
  try { result = await fence.dispatch(signal => client.exchange(call, operation, session, 'POST', write.path, undefined, write.body, signal)); }
  catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint; nothing was sent')); }
  const receipt = exchangeReceipt(client, call, result);
  if ('outcome' in result) return writeAttemptForExchange(permit, client, call, session.credentials, result.outcome, receipt, zero, write.notFound);
  let decoded: { output: T; providerId: string };
  try { decoded = write.decode(result.response.body); }
  catch (error) {
    const invalid = failure(operation, 'PROTOCOL', `${write.invalidMessage}: ${errorMessage(error)}`);
    const providerId = acceptedProviderId(result.response.body);
    // A 2xx accepted the write; with no readable ID it is never resent.
    if (!providerId) return newMutationUncertain(zero, invalid, receipt);
    return readBack(providerId, await recordAcceptedWrite(permit, providerId));
  }
  await recordAcceptedWrite(permit, decoded.providerId);
  const acceptedReceipt = client.receipt(call, result.response, decoded.providerId);
  const mismatch = write.rejectAccepted?.(decoded.output) ?? '';
  if (mismatch) return newMutationUncertain(zero, failure(operation, 'PROTOCOL', `${write.invalidMessage}: ${mismatch}`), acceptedReceipt);
  return newMutationBranch(write.successBranch, decoded.output, acceptedReceipt);
}

/**
 * Read an accepted write back by provider ID and never send it. A confirmed write selects success. A transient read
 * failure retries only when the ID is checkpointed, because without it the next attempt could resend; anything else is
 * uncertain.
 */
async function readBackAcceptedWrite<T>(client: ZohoDeskClient, call: Call, session: { credentials: Credentials; baseURL: string }, write: FencedWrite<T>, providerId: string, isProviderIdSaved: boolean): Promise<MutationAttempt<T>> {
  const result = await client.exchange(call, write.operation, session, 'GET', write.readBackPath(providerId));
  const receipt = exchangeReceipt(client, call, result, providerId);
  if ('outcome' in result) {
    const { disposition } = result.outcome;
    if (isProviderIdSaved && (disposition === 'rateLimited' || disposition === 'notSent' || disposition === 'unavailable')) return newMutationRetry(result.outcome.failure, result.outcome.retryAfterMs);
    return newMutationUncertain(write.zero, result.outcome.failure, receipt);
  }
  try { return newMutationBranch(write.successBranch, write.decodeReadBack(result.response.body, providerId), receipt); }
  catch (error) { return newMutationUncertain(write.zero, failure(write.operation, 'PROTOCOL', `Zoho Desk accepted the write, but its read-back was invalid: ${errorMessage(error)}`), receipt); }
}

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Map a session that could not start; nothing was sent in this attempt. */
async function sessionWriteAttempt<T>(permit: WritePermit, session: ExchangeFailure, zero: T): Promise<MutationAttempt<T>> {
  const attempt = sessionFailure(session, zero, true) as MutationAttempt<T>;
  if (!permit.isFirstSend) {
    // notSent would hide the earlier possible send, so nothing is recorded.
    if (session.disposition !== 'rejected' && session.disposition !== 'defect') return attempt;
    return newMutationUncertain(zero, failure(session.failure.operation, session.failure.kind, 'an earlier attempt may have sent the request, and the connection needs reconnection to resend it'), {});
  }
  try { await permit.fence.recordNotSent(); }
  catch { return newMutationRetry(failure(session.failure.operation, 'AVAILABILITY', 'Dex did not record the not-sent checkpoint')); }
  return attempt;
}

/** Map a sent write. Only a definite refusal selects a refusal branch; any other outcome may have been applied. */
async function writeAttemptForExchange<T>(permit: WritePermit, client: ZohoDeskClient, call: Call, credentials: Credentials, outcome: ExchangeFailure, receipt: ReceiptDraft, zero: T, notFound: string): Promise<MutationAttempt<T>> {
  const status = outcome.response?.status ?? 0;
  // A 2xx accepted the write; with no readable ID it is never resent. The client reports 'invalid' only for a 2xx.
  if (outcome.disposition === 'invalid' || (status >= 200 && status < 300)) return newMutationUncertain(zero, outcome.failure, receipt);
  if (outcome.disposition === 'rateLimited' || outcome.disposition === 'notSent' || (outcome.disposition === 'unavailable' && status === 503)) {
    // A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
    try { await permit.fence.recordCanResend(); } catch { /* counted as a possible send */ }
    return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  }
  if (outcome.disposition === 'notFound') return rejectWrite(permit, outcome.failure, receipt, zero, notFound);
  // rejectWrite selects uncertain after an earlier possible send.
  if (outcome.disposition === 'defect') return rejectWrite(permit, outcome.failure, {}, zero, 'defect');
  if (outcome.disposition === 'rejected' && status === 401) {
    // A changed token or a transient refresh failure retries, because the 401 proves this send applied nothing.
    const replacement = await client.replaceRejectedCredentials(call, credentials);
    if (replacement === 'replacement') return retryRejectedCredentialWrite(permit, failure(outcome.failure.operation, 'AUTHENTICATION', 'Zoho Desk rejected the prior access token (HTTP 401); retry with its replacement'), true);
    if (replacement === 'unavailable') return retryRejectedCredentialWrite(permit, failure(outcome.failure.operation, outcome.failure.kind, 'Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable'), permit.isFirstSend);
    return rejectWrite(permit, outcome.failure, receipt, zero, 'providerRejected');
  }
  if (outcome.disposition === 'rejected' && definiteRefusalStatuses.has(status)) return rejectWrite(permit, outcome.failure, receipt, zero, 'providerRejected');
  return resendOrUncertain(permit, outcome.failure, outcome.retryAfterMs, receipt, zero);
}

/** Retry a 401; after an earlier possible send a refresh outage records nothing, so the claim keeps counting. */
async function retryRejectedCredentialWrite<T>(permit: WritePermit, cause: Failure, shouldRecordCanResend: boolean): Promise<MutationAttempt<T>> {
  if (shouldRecordCanResend) {
    try { await permit.fence.recordCanResend(); }
    catch { return newMutationRetry(failure(cause.operation, 'AVAILABILITY', 'Dex did not record the credential resend checkpoint')); }
  }
  return newMutationRetry(cause);
}

/** Select a refusal branch only when no earlier send of this call may have applied. */
async function rejectWrite<T>(permit: WritePermit, cause: Failure, receipt: ReceiptDraft, zero: T, branch: string): Promise<MutationAttempt<T>> {
  if (!permit.isFirstSend) return newMutationUncertain(zero, cause, receipt);
  try { await permit.fence.recordCanResend(); }
  catch { return newMutationRetry(failure(cause.operation, 'AVAILABILITY', 'Dex did not record the refusal checkpoint')); }
  return newMutationBranch(branch, zero, receipt, cause);
}

/** Retry an unknown outcome while the send cap allows another send, else select uncertain. */
function resendOrUncertain<T>(permit: WritePermit, cause: Failure, retryAfterMs: number, receipt: ReceiptDraft, zero: T): MutationAttempt<T> {
  // The claim stays the last marker, so the next attempt counts this send.
  return permit.canResendUnknownOutcome ? newMutationRetry(cause, retryAfterMs) : newMutationUncertain(zero, cause, receipt);
}

/** Checkpoint the accepted provider ID and report whether it was saved. */
async function recordAcceptedWrite(permit: WritePermit, providerId: string): Promise<boolean> {
  try { await permit.fence.recordProviderId(providerId); return true; } catch { /* Acceptance is already known if this extra checkpoint fails. */ return false; }
}

/** Return the valid id of a 2xx object body, or an empty string. */
function acceptedProviderId(body: Uint8Array): string {
  try {
    const value = decodeObject(body).id;
    if (typeof value === 'number' && Number.isSafeInteger(value) && value >= 0) return numericId.test(String(value)) ? String(value) : '';
    return typeof value === 'string' && numericId.test(value) ? value : '';
  } catch { return ''; }
}
