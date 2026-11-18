// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import type { AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketConversation, TicketDetails, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { AddNoteDefinition, CreateTicketDefinition, GetTicketDefinition, SearchTicketsDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import { acceptedProviderID, type Exchange, type FreshdeskClient } from './client.js';

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
  if (!/^[A-Za-z0-9.!#$%&*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$/.test(value) || value.includes('..') || value.startsWith('.') || value.slice(0, value.indexOf('@')).endsWith('.') || /["'()<>,;\s]/.test(value)) throw new TypeError('Freshdesk requester email is invalid');
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
/** Decode one Freshdesk ticket object at parity with the Go connector; throws TypeError when invalid. */
export function decodeTicketWire(value: unknown, expectedID = 0): Ticket { return ticket(value, expectedID); }
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
function queryFailure<T>(client: FreshdeskClient, call: Call, operation: string, branch: string, value: T, kind: Failure['kind'], message: string, response?: ProviderResponse, objectID = 0): QueryAttempt<T> {
  return newQueryBranch(branch, value, client.receipt(call, response, objectID), client.failure(operation, kind, message));
}
function mutationFailure<T>(client: FreshdeskClient, call: Call, operation: string, branch: string, value: T, kind: Failure['kind'], message: string, response?: ProviderResponse, objectID = 0): MutationAttempt<T> {
  return newMutationBranch(branch, value, client.receipt(call, response, objectID), client.failure(operation, kind, message));
}
function queryExchange<T>(client: FreshdeskClient, call: Call, exchange: Exchange, zero: T, notFoundBranch: string, objectID = 0): QueryAttempt<T> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'serviceUnavailable', 'unavailable'].includes(exchange.outcome)) return newQueryRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' ? notFoundBranch : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, client.receipt(call, exchange.response, objectID), exchange.failure);
}
function updateExchange(client: FreshdeskClient, call: Call, exchange: Exchange, value: UpdateTicketOutput, objectID: number): MutationAttempt<UpdateTicketOutput> | undefined {
  if (exchange.outcome === 'succeeded') return undefined;
  if (['rateLimited', 'notSent', 'serviceUnavailable', 'unavailable'].includes(exchange.outcome)) return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, value, client.receipt(call, exchange.response, objectID), exchange.failure);
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
    const terminal = queryExchange(this.client, call, first, zero, 'notFound', input.ticketId); if (terminal) return terminal;
    let details: TicketDetails;
    try {
      const wire = object(parse((first as { response: ProviderResponse }).response.body));
      details = { ticket: ticket(wire, input.ticketId), conversations: [] };
      if (wire.requester) { const requester = object(wire.requester); const id = number(requester.id); if (positive(id)) { if (id !== details.ticket.requesterId) throw new TypeError('requester mismatch'); details.requester = { id, ...optionalField(wireText(requester.name), 'name'), ...optionalField(wireText(requester.email), 'email') }; } }
    } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid ticket', first.response, input.ticketId); }
    const limit = input.conversationLimit || 10;
    const second = await this.client.exchange(call, operation, resolved.key, 'GET', `${path}/conversations`, new URLSearchParams({ per_page: String(limit), page: '1' }));
    const secondTerminal = queryExchange(this.client, call, second, zero, 'notFound', input.ticketId); if (secondTerminal) return secondTerminal;
    try {
      const rows = parse((second as { response: ProviderResponse }).response.body);
      if (!Array.isArray(rows) || rows.length > limit) throw new TypeError('invalid conversation page');
      details.conversations = rows.map(item => conversation(item)).sort((left, right) => left.createdAt.localeCompare(right.createdAt) || left.id - right.id);
      if (String(second.response!.headers.link ?? '').includes('rel="next"')) details.hasMoreConversations = true;
      return newQueryBranch('found', details, this.client.receipt(call, second.response, input.ticketId));
    } catch { return queryFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid conversation page', second.response, input.ticketId); }
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

// Only these statuses prove Freshdesk applied nothing; any other non-2xx answer may have been applied.
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

// A read with one of these outcomes may answer differently on a later attempt.
const transientOutcomes = new Set<Exchange['outcome']>(['rateLimited', 'notSent', 'unavailable', 'serviceUnavailable']);
// Freshdesk's largest conversation page, and the pages a note read-back searches in one attempt.
const acceptedConversationPageSize = 100;
const maximumAcceptedConversationPages = 10;

/** A fence that allows this attempt to send or read back, with its send-cap facts. */
interface WritePermit {
  fence: ReturnType<typeof newFence>; isFirstSend: boolean; canResendUnknownOutcome: boolean;
  /** An earlier attempt checkpointed this ID; nonzero means read back and never send. */ savedProviderID: number;
}

/** The result of reading an accepted write back by provider ID; exchange explains an unconfirmed read. */
type AcceptedWriteReadBack<T> = { isConfirmed: true; output: T; exchange: Exchange } | { isConfirmed: false; exchange: Exclude<Exchange, { outcome: 'succeeded' }> };

/** Decodes one operation's accepted answer and reads the write back by provider ID. */
interface AcceptedWriteReader<T> {
  /** Return the output and provider ID of a 2xx answer; throws TypeError when invalid. */ decodeAcceptedWrite(body: Uint8Array): { output: T; providerID: number };
  /** Read the accepted write back by the provider ID Freshdesk returned. */ readAcceptedWrite(call: Call, key: string, providerID: number): Promise<AcceptedWriteReadBack<T>>;
}

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Decide whether this attempt may send; Freshdesk has no read-back by Call ID, so only a checkpointed provider ID is read back, and any other earlier possible send is resent while the cap allows. */
function beginWrite<T>(client: FreshdeskClient, call: Call, definition: MutationDefinition, zero: T): WritePermit | MutationAttempt<T> {
  const operation = definition.operation.operationId;
  const operationMaxSends = definitionMaxSends(definition);
  let fence: ReturnType<typeof newFence>;
  try { fence = newFence(call, definition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: operationMaxSends }); }
  catch { return newMutationBranch('defect', zero, {}, client.failure(operation, 'LOCAL_DEFECT', 'single-dispatch fence could not start')); }
  const decision = fence.decide();
  // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
  if (decision.kind === 'failClosed') return newMutationRetry(client.failure(operation, 'AVAILABILITY', 'the single-dispatch checkpoint could not be read; nothing was sent in this attempt'));
  if (decision.marker?.providerId) {
    // A saved provider ID proves Freshdesk accepted the write, so it is read back and never resent.
    const savedProviderID = markerProviderID(decision.marker.providerId);
    if (!savedProviderID) return newMutationUncertain(zero, client.failure(operation, 'PROTOCOL', "Freshdesk accepted an earlier attempt's write, and its checkpointed ID is not a Freshdesk ID"), client.receipt(call));
    return { fence, isFirstSend: false, canResendUnknownOutcome: false, savedProviderID };
  }
  if (decision.kind === 'readBackRequired') return newMutationUncertain(zero, client.failure(operation, 'TRANSPORT', 'earlier attempts may have sent the request and the send cap is reached, so it is not sent again'), client.receipt(call));
  const priorSends = decision.priorSendUpperBound ?? 0;
  return { fence, isFirstSend: priorSends === 0, canResendUnknownOutcome: priorSends + 1 < effectiveMaxSends(call, operationMaxSends), savedProviderID: 0 };
}

/** Map a credential failure before any send in this attempt. */
async function credentialAttempt<T>(client: FreshdeskClient, permit: WritePermit, failure: Failure, transient: boolean, zero: T): Promise<MutationAttempt<T>> {
  // notSent would hide the earlier possible send, so nothing is recorded.
  if (!permit.isFirstSend) return transient ? newMutationRetry(failure) : newMutationUncertain(zero, failure, {});
  try { await permit.fence.recordNotSent(); } catch { return newMutationRetry(client.failure(failure.operation, 'AVAILABILITY', 'Dex did not record the not-sent checkpoint')); }
  return transient ? newMutationRetry(failure) : newMutationBranch('defect', zero, {}, failure);
}

/** Dispatch one POST under the fence; a refused dispatch returns its Retry attempt. */
async function sendWrite<T>(client: FreshdeskClient, call: Call, permit: WritePermit, operation: string, key: string, path: string, payload: unknown): Promise<Exchange | MutationAttempt<T>> {
  try { return await permit.fence.dispatch(signal => client.exchange(call, operation, key, 'POST', path, undefined, payload, signal)); }
  // Dispatch refused before sending, so nothing reached Freshdesk in this attempt.
  catch { return newMutationRetry(client.failure(operation, 'AVAILABILITY', 'Freshdesk write was not dispatched')); }
}

/** Map a sent write: any 2xx, or an answer carrying a created ID, was accepted and is never resent. */
async function finishWrite<T>(client: FreshdeskClient, call: Call, permit: WritePermit, operation: string, key: string, exchange: Exchange, successBranch: string, zero: T, reader: AcceptedWriteReader<T>, notFoundBranch = 'providerRejected'): Promise<MutationAttempt<T>> {
  const status = exchange.response?.status ?? 0;
  if (exchange.outcome === 'succeeded' || (status >= 200 && status < 300)) {
    let failure: Failure;
    if (exchange.outcome === 'succeeded') {
      try {
        const accepted = reader.decodeAcceptedWrite(exchange.response.body);
        await recordAcceptedWrite(permit, accepted.providerID);
        return newMutationBranch(successBranch, accepted.output, client.receipt(call, exchange.response, accepted.providerID));
      } catch (error) { failure = client.failure(operation, 'PROTOCOL', `Freshdesk accepted the write but returned an invalid answer: ${error instanceof Error ? error.message : 'invalid'}`); }
    } else failure = exchange.failure;
    return completeUnreadableAcceptedWrite(client, call, permit, key, exchange, acceptedProviderID(exchange.response?.body ?? new Uint8Array()), failure, successBranch, zero, reader);
  }
  if (exchange.bodyProviderID) return completeUnreadableAcceptedWrite(client, call, permit, key, exchange, exchange.bodyProviderID, exchange.failure, successBranch, zero, reader);
  return writeExchangeAttempt(client, call, permit, exchange, zero, notFoundBranch);
}

/** Read back an accepted write whose answer was unusable; Freshdesk has no read-back without the ID, so none selects uncertain. */
async function completeUnreadableAcceptedWrite<T>(client: FreshdeskClient, call: Call, permit: WritePermit, key: string, exchange: Exchange, providerID: number, failure: Failure, successBranch: string, zero: T, reader: AcceptedWriteReader<T>): Promise<MutationAttempt<T>> {
  if (!providerID) return newMutationUncertain(zero, failure, client.receipt(call, exchange.response));
  // Without a saved ID a later attempt could resend, so a failed checkpoint forbids Retry.
  let isProviderIDRecorded = true;
  try { await permit.fence.recordProviderId(String(providerID)); } catch { isProviderIDRecorded = false; }
  return confirmAcceptedWrite(client, call, key, providerID, isProviderIDRecorded, successBranch, zero, reader);
}

/** Read an accepted write back: confirmed succeeds, a checkpointed transient failure retries, and anything else selects uncertain. */
async function confirmAcceptedWrite<T>(client: FreshdeskClient, call: Call, key: string, providerID: number, isProviderIDRecorded: boolean, successBranch: string, zero: T, reader: AcceptedWriteReader<T>): Promise<MutationAttempt<T>> {
  const readBack = await reader.readAcceptedWrite(call, key, providerID);
  const receipt = client.receipt(call, readBack.exchange.response, providerID);
  if (readBack.isConfirmed) return newMutationBranch(successBranch, readBack.output, receipt);
  if (isProviderIDRecorded && transientOutcomes.has(readBack.exchange.outcome)) return newMutationRetry(readBack.exchange.failure, readBack.exchange.retryAfterMs);
  return newMutationUncertain(zero, readBack.exchange.failure, receipt);
}

/** An unconfirmed read-back that carries a protocol failure for its last read. */
function unconfirmedReadBack<T>(client: FreshdeskClient, exchange: Exchange, operation: string, message: string): AcceptedWriteReadBack<T> {
  return { isConfirmed: false, exchange: { outcome: 'invalid', ...(exchange.response ? { response: exchange.response } : {}), failure: client.failure(operation, 'PROTOCOL', message), retryAfterMs: 0 } };
}

/** Map a sent write Freshdesk did not accept; only a complete definite refusal selects a refusal branch, and any other outcome resends while the cap allows. */
async function writeExchangeAttempt<T>(client: FreshdeskClient, call: Call, permit: WritePermit, exchange: Exclude<Exchange, { outcome: 'succeeded' }>, zero: T, notFoundBranch: string): Promise<MutationAttempt<T>> {
  if (exchange.outcome === 'rateLimited' || exchange.outcome === 'notSent' || exchange.outcome === 'serviceUnavailable') {
    // A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
    try { await permit.fence.recordCanResend(); } catch { /* counted as a possible send */ }
    return newMutationRetry(exchange.failure, exchange.retryAfterMs);
  }
  const receipt = client.receipt(call, exchange.response);
  if (exchange.outcome === 'notFound' && !exchange.isBodyIncomplete) return rejectWrite(client, permit, exchange.failure, receipt, notFoundBranch, zero);
  if (exchange.outcome === 'defect') {
    // defect would hide an earlier possible send, so only a first send selects it.
    if (permit.isFirstSend) return rejectWrite(client, permit, exchange.failure, {}, 'defect', zero);
    // The request could not be built, so a resend would fail the same way.
    return newMutationUncertain(zero, exchange.failure, receipt);
  }
  if (exchange.outcome === 'rejected' && definiteRefusalStatuses.has(exchange.response?.status ?? 0) && !exchange.isBodyIncomplete) return rejectWrite(client, permit, exchange.failure, receipt, 'providerRejected', zero);
  return resendOrUncertain(permit, zero, exchange.failure, exchange.retryAfterMs, receipt);
}

/** Select a refusal branch only when no earlier send of this call may have applied. */
async function rejectWrite<T>(client: FreshdeskClient, permit: WritePermit, failure: Failure, receipt: ReceiptDraft, branch: string, zero: T): Promise<MutationAttempt<T>> {
  if (!permit.isFirstSend) return newMutationUncertain(zero, failure, receipt);
  try { await permit.fence.recordCanResend(); } catch { return newMutationRetry(client.failure(failure.operation, 'AVAILABILITY', 'Dex did not record the refusal checkpoint')); }
  return newMutationBranch(branch, zero, receipt, failure);
}

/** Retry an unknown outcome while the send cap allows another send, else select uncertain. */
function resendOrUncertain<T>(permit: WritePermit, zero: T, failure: Failure, retryAfterMs: number, receipt: ReceiptDraft): MutationAttempt<T> {
  // The claim stays the last marker, so the next attempt counts this send.
  return permit.canResendUnknownOutcome ? newMutationRetry(failure, retryAfterMs) : newMutationUncertain(zero, failure, receipt);
}

async function recordAcceptedWrite(permit: WritePermit, providerID: number): Promise<void> {
  try { await permit.fence.recordProviderId(String(providerID)); } catch { /* The accepted response already proves success. */ }
}

/** Return a checkpointed Freshdesk ID for the Receipt, or 0 when none is valid. */
function markerProviderID(providerID: string | undefined): number {
  const parsed = /^[0-9]+$/.test(providerID ?? '') ? Number(providerID) : 0;
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}

/** Create one ticket under the single-dispatch fence and its send cap. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return CreateTicketDefinition; }
  /** Correlate the mutation by Call ID; Freshdesk does not receive this key. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate and send the ticket, resending an unknown outcome while the send cap allows. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    const operation = 'createTicket'; const zero: CreateTicketOutput = { ticket: zeroTicket };
    try {
      validateText(input.subject); validateText(input.description); validateEmail(input.requester.email);
      if (input.requester.name && input.requester.name !== input.requester.name.trim()) throw new TypeError('requester name has surrounding spaces');
      if (input.status) validateStatus(input.status); if (input.priority) validatePriority(input.priority);
      if (input.type && (input.type !== input.type.trim() || Buffer.byteLength(input.type) > 255)) throw new TypeError('ticket type is invalid');
      validateTags(input.tags ?? []); if (!nonnegative(input.groupId ?? 0) || !nonnegative(input.responderId ?? 0)) throw new TypeError('assignment ID is invalid');
    } catch { return mutationFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'Freshdesk create input is invalid'); }
    const permit = beginWrite(this.client, call, CreateTicketDefinition, zero); if (!('fence' in permit)) return permit;
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) return credentialAttempt(this.client, permit, resolved.failure, resolved.transient, zero);
    const reader = new CreatedTicketReader(this.client);
    if (permit.savedProviderID) return confirmAcceptedWrite(this.client, call, resolved.key, permit.savedProviderID, true, 'created', zero, reader);
    const request = { subject: input.subject, description: encodeHTML(input.description), email: input.requester.email,
      ...(input.requester.name ? { name: input.requester.name } : {}), ...(input.status ? { status: input.status } : {}), ...(input.priority ? { priority: input.priority } : {}),
      ...(input.type ? { type: input.type } : {}), ...(input.tags?.length ? { tags: input.tags } : {}), ...(input.groupId ? { group_id: input.groupId } : {}), ...(input.responderId ? { responder_id: input.responderId } : {}) };
    const exchange = await sendWrite<CreateTicketOutput>(this.client, call, permit, operation, resolved.key, '/tickets', request); if (!('outcome' in exchange)) return exchange;
    return finishWrite(this.client, call, permit, operation, resolved.key, exchange, 'created', zero, reader);
  }
}

/** Decodes a created ticket and reads it back by ID. */
class CreatedTicketReader implements AcceptedWriteReader<CreateTicketOutput> {
  constructor(private readonly client: FreshdeskClient) {}
  decodeAcceptedWrite(body: Uint8Array): { output: CreateTicketOutput; providerID: number } { const created = ticket(parse(body)); return { output: { ticket: created }, providerID: created.id }; }
  /** Read GET /tickets/{id}; a missing or invalid ticket is unconfirmed. */
  async readAcceptedWrite(call: Call, key: string, providerID: number): Promise<AcceptedWriteReadBack<CreateTicketOutput>> {
    const exchange = await this.client.exchange(call, 'createTicket', key, 'GET', `/tickets/${providerID}`);
    if (exchange.outcome !== 'succeeded') return { isConfirmed: false, exchange };
    try { return { isConfirmed: true, output: { ticket: ticket(parse(exchange.response.body), providerID) }, exchange }; }
    catch (error) { return unconfirmedReadBack(this.client, exchange, 'createTicket', `Freshdesk accepted the ticket, and its read-back returned an invalid ticket: ${error instanceof Error ? error.message : 'invalid'}`); }
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
    const readTerminal = updateExchange(this.client, call, read, zero, input.ticketId); if (readTerminal) return readTerminal;
    let current: Ticket;
    try { current = ticket(parse((read as { response: ProviderResponse }).response.body), input.ticketId); }
    catch { return mutationFailure(this.client, call, operation, 'invalidResponse', zero, 'PROTOCOL', 'Freshdesk returned an invalid ticket', read.response, input.ticketId); }
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
    // The PUT sets final values, so an unknown outcome repeats it.
    if (write.outcome !== 'succeeded' && (write.isBodyIncomplete || (write.outcome !== 'notFound' && write.outcome !== 'defect' && !(write.outcome === 'rejected' && definiteRefusalStatuses.has(write.response?.status ?? 0))))) return newMutationRetry(write.failure, write.retryAfterMs);
    const writeTerminal = updateExchange(this.client, call, write, { ticket: current }, input.ticketId); if (writeTerminal) return writeTerminal;
    try { return newMutationBranch('updated', { ticket: ticket(parse((write as { response: ProviderResponse }).response.body), input.ticketId) }, this.client.receipt(call, write.response, input.ticketId)); }
    catch { return newMutationRetry(this.client.failure(operation, 'PROTOCOL', 'Freshdesk accepted the change but returned an invalid updated ticket')); }
  }
}

/** Add one private note or public reply under the single-dispatch fence and its send cap. */
export class AddNoteOperation implements Mutation<AddNoteInput, AddNoteOutput> {
  /** Bind the API client. */ constructor(private readonly client: FreshdeskClient) {}
  /** Return stable manifest metadata. */ definition() { return AddNoteDefinition; }
  /** Correlate the mutation by Call ID; Freshdesk does not receive this key. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate and send one note or reply, resending an unknown outcome while the send cap allows. */
  async invoke(call: Call, input: AddNoteInput): Promise<MutationAttempt<AddNoteOutput>> {
    const operation = 'addNote'; const zero: AddNoteOutput = { conversation: zeroConversation };
    if (!positive(input.ticketId) || !input.body?.trim()) return mutationFailure(this.client, call, operation, 'defect', zero, 'VALIDATION', 'Freshdesk note input is invalid');
    const permit = beginWrite(this.client, call, AddNoteDefinition, zero); if (!('fence' in permit)) return permit;
    const resolved = await credential(this.client, call, operation);
    if ('failure' in resolved) return credentialAttempt(this.client, permit, resolved.failure, resolved.transient, zero);
    const reader = new AddedConversationReader(this.client, input);
    if (permit.savedProviderID) return confirmAcceptedWrite(this.client, call, resolved.key, permit.savedProviderID, true, 'added', zero, reader);
    const path = `/tickets/${input.ticketId}/${input.isPublicReply ? 'reply' : 'notes'}`;
    const payload = input.isPublicReply ? { body: encodeHTML(input.body) } : { body: encodeHTML(input.body), private: true };
    const exchange = await sendWrite<AddNoteOutput>(this.client, call, permit, operation, resolved.key, path, payload); if (!('outcome' in exchange)) return exchange;
    return finishWrite(this.client, call, permit, operation, resolved.key, exchange, 'added', zero, reader, 'notFound');
  }
}

/** Decode an added conversation, filling its kind from the request because a reply answer lacks it. */
function addedConversation(value: unknown, input: AddNoteInput): TicketConversation {
  const wire = object(value);
  const added = conversation(wire);
  if (added.ticketId && added.ticketId !== input.ticketId) throw new TypeError('conversation belongs to another ticket');
  added.ticketId = input.ticketId;
  added.source = input.isPublicReply ? 0 : 2;
  added.isPrivate = input.isPublicReply ? false : wire.private !== false;
  return added;
}

/** Decodes an added note or reply and finds it in the ticket's conversation list. */
class AddedConversationReader implements AcceptedWriteReader<AddNoteOutput> {
  constructor(private readonly client: FreshdeskClient, private readonly input: AddNoteInput) {}
  decodeAcceptedWrite(body: Uint8Array): { output: AddNoteOutput; providerID: number } { const added = addedConversation(parse(body), this.input); return { output: { conversation: added }, providerID: added.id }; }
  /** Page GET /tickets/{id}/conversations, Freshdesk's only conversation read, until it finds the ID. */
  async readAcceptedWrite(call: Call, key: string, providerID: number): Promise<AcceptedWriteReadBack<AddNoteOutput>> {
    for (let page = 1; page <= maximumAcceptedConversationPages; page++) {
      const exchange = await this.client.exchange(call, 'addNote', key, 'GET', `/tickets/${this.input.ticketId}/conversations`, new URLSearchParams({ per_page: String(acceptedConversationPageSize), page: String(page) }));
      if (exchange.outcome !== 'succeeded') return { isConfirmed: false, exchange };
      let rows: unknown;
      try { rows = parse(exchange.response.body); } catch { rows = undefined; }
      if (!Array.isArray(rows)) return unconfirmedReadBack(this.client, exchange, 'addNote', 'Freshdesk accepted the note, and its read-back returned an invalid conversation page');
      const row: unknown = rows.find(item => item && typeof item === 'object' && !Array.isArray(item) && (item as Record<string, unknown>).id === providerID);
      if (row !== undefined) {
        try { return { isConfirmed: true, output: { conversation: addedConversation(row, this.input) }, exchange }; }
        catch (error) { return unconfirmedReadBack(this.client, exchange, 'addNote', `Freshdesk accepted the note, and its read-back returned an invalid conversation: ${error instanceof Error ? error.message : 'invalid'}`); }
      }
      if (!String(exchange.response.headers.link ?? '').includes('rel="next"')) return unconfirmedReadBack(this.client, exchange, 'addNote', "Freshdesk accepted the note, and the ticket's conversations do not list it");
    }
    return unconfirmedReadBack(this.client, { outcome: 'invalid', failure: this.client.failure('addNote', 'PROTOCOL', 'invalid'), retryAfterMs: 0 }, 'addNote', "Freshdesk accepted the note, and it is not within the ticket's first conversation pages");
  }
}
