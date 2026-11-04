// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type Receipt } from '@superdurable/connectors-sdk';
import { gorgiasFailure, isBareEmail, type Exchange, type GorgiasClient } from './client.js';
import { AddNoteDefinition, CreateTicketDefinition, FindCustomerByEmailDefinition, GetTicketDefinition, SearchTicketsDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import { emptyAddNoteOutput, emptyCreateTicketOutput, emptyCustomerMatches, emptySearchTicketsOutput, emptyTicketDetails, emptyUpdateTicketOutput } from './generated/mock.js';
import type { AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, Credentials, CustomerMatches, FindCustomerByEmailInput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketDetails, TicketMessage, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { decodeCustomerMatches, decodeDetails, decodeList, decodeMessage, decodeTicket, decodeTicketPage, objectBody } from './wire.js';

function queryFailure<T>(client: GorgiasClient, call: Call, result: Exchange, output: T, notFoundBranch = 'providerRejected', objectID = 0): QueryAttempt<T> {
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.outcome)) return newQueryRetry(result.failure!, result.retryAfterMs);
  const branch = result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : result.outcome === 'notFound' ? notFoundBranch : 'providerRejected';
  return newQueryBranch(branch, output, client.receipt(call, result, objectID), result.failure);
}
function mutationFailure<T>(client: GorgiasClient, call: Call, result: Exchange, output: T, notFoundBranch = 'providerRejected', objectID = 0, invalidBranch = 'invalidResponse'): MutationAttempt<T> {
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.outcome)) return newMutationRetry(result.failure!, result.retryAfterMs);
  const branch = result.outcome === 'invalid' ? invalidBranch : result.outcome === 'defect' ? 'defect' : result.outcome === 'notFound' ? notFoundBranch : 'providerRejected';
  return newMutationBranch(branch, output, client.receipt(call, result, objectID), result.failure);
}
function unsentRequestAttempt<T>(client: GorgiasClient, call: Call, result: Exchange, output: T, notFoundBranch = 'providerRejected', objectID = 0): MutationAttempt<T> {
  if (result.outcome === 'invalid') return newMutationRetry(result.failure!, result.retryAfterMs);
  return mutationFailure(client, call, result, output, notFoundBranch, objectID);
}
function defect<T>(operation: string, output: T, error: unknown, isMutation: false): QueryAttempt<T>;
function defect<T>(operation: string, output: T, error: unknown, isMutation: true): MutationAttempt<T>;
function defect<T>(operation: string, output: T, error: unknown, isMutation: boolean): QueryAttempt<T> | MutationAttempt<T> {
  const failure = gorgiasFailure('VALIDATION', operation, error instanceof Error ? error.message : 'invalid input');
  const receipt = {} as Receipt;
  return isMutation ? newMutationBranch('defect', output, receipt, failure) : newQueryBranch('defect', output, receipt, failure);
}
function protocol<T>(client: GorgiasClient, call: Call, operation: string, output: T, result: Exchange, prefix: string, error: unknown, isMutation: false, branch?: string): QueryAttempt<T>;
function protocol<T>(client: GorgiasClient, call: Call, operation: string, output: T, result: Exchange, prefix: string, error: unknown, isMutation: true, branch?: string): MutationAttempt<T>;
function protocol<T>(client: GorgiasClient, call: Call, operation: string, output: T, result: Exchange, prefix: string, error: unknown, isMutation: boolean, branch = 'invalidResponse'): QueryAttempt<T> | MutationAttempt<T> {
  const failure = gorgiasFailure('PROTOCOL', operation, `${prefix}: ${error instanceof Error ? error.message : 'invalid response'}`);
  const receipt = client.receipt(call, result);
  return isMutation ? newMutationBranch(branch, output, receipt, failure) : newQueryBranch(branch, output, receipt, failure);
}
function requireID(value: number | undefined, name: string): void { if (!Number.isSafeInteger(value ?? 0) || (value ?? 0) < 1) throw new TypeError(`${name} must be a positive Gorgias ID`); }
function optionalID(value: number | undefined, name: string): void { if (!Number.isSafeInteger(value ?? 0) || (value ?? 0) < 0) throw new TypeError(`${name} cannot be negative`); }
function validateTags(tags: string[] | undefined, field: string): void {
  if (!tags) return;
  if (tags.length > 20) throw new TypeError(`${field} lists more than 20 tags`);
  const seen = new Set<string>();
  for (const tag of tags) {
    if (!tag || tag !== tag.trim() || [...tag].length > 256 || /\p{Cc}/u.test(tag) || seen.has(tag)) throw new TypeError(`${field} has an invalid or duplicate tag`);
    seen.add(tag);
  }
}
function validateStatus(status: string): void { if (status !== 'open' && status !== 'closed') throw new TypeError('status is not a Gorgias status'); }
function validatePriority(priority: string): void { if (!['low', 'normal', 'high', 'critical'].includes(priority)) throw new TypeError('priority is not a Gorgias priority'); }
function requireText(text: string, name: string): void { if (!text?.trim()) throw new TypeError(`${name} is required`); }
function queryString(values: Record<string, string | number | undefined>): URLSearchParams {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value !== undefined && value !== '') query.set(key, String(value));
  return query;
}

/** Query one filtered Gorgias ticket page. */
export class SearchTicketsOperation implements Query<SearchTicketsInput, SearchTicketsOutput> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return SearchTicketsDefinition; }
  /** Invoke one search attempt. */
  async invoke(call: Call, input: SearchTicketsInput): Promise<QueryAttempt<SearchTicketsOutput>> {
    try {
      if (input.requesterEmail && !isBareEmail(input.requesterEmail)) throw new TypeError('requesterEmail must be one bare email address');
      if (input.requesterEmail && input.requesterId) throw new TypeError('set requesterEmail or requesterId, not both');
      optionalID(input.requesterId, 'requesterId'); optionalID(input.viewId, 'viewId'); validateTags(input.tags, 'tags');
      for (const status of input.statuses ?? []) validateStatus(status);
      for (const priority of input.priorities ?? []) validatePriority(priority);
      if (new Set(input.statuses ?? []).size !== (input.statuses?.length ?? 0) || new Set(input.priorities ?? []).size !== (input.priorities?.length ?? 0)) throw new TypeError('duplicate filter');
      if (!Number.isInteger(input.pageSize ?? 0) || (input.pageSize ?? 0) < 0 || (input.pageSize ?? 0) > 100) throw new TypeError('pageSize must be 1 to 100 or zero');
      if (input.cursor && (input.cursor.length > 2048 || !/^[A-Za-z0-9+/=_.:-]+$/.test(input.cursor))) throw new TypeError('invalid cursor');
      if (input.updatedSince && (!/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(input.updatedSince) || Number.isNaN(Date.parse(input.updatedSince)))) throw new TypeError('invalid updatedSince');
    } catch (error) { return defect('searchTickets', emptySearchTicketsOutput, error, false); }
    const resolved = await this.client.resolveCredentials(call, 'searchTickets');
    if (!resolved.credentials) return newQueryBranch('defect', emptySearchTicketsOutput, {} as Receipt, resolved.failure);
    let requesterID = input.requesterId ?? 0;
    if (input.requesterEmail) {
      const lookup = await this.client.exchange(call, resolved.credentials, 'searchTickets', 'GET', '/customers', queryString({ email: input.requesterEmail, limit: 10 }));
      if (lookup.outcome !== 'succeeded') return queryFailure(this.client, call, lookup, emptySearchTicketsOutput);
      try {
        const matches = decodeCustomerMatches(lookup.response!.body, input.requesterEmail);
        if ((matches.customers ?? []).length > 1) throw new TypeError('more than one customer has this primary email address');
        if (!(matches.customers ?? []).length) return newQueryBranch('searched', { tickets: [], listedCount: 0 }, this.client.receipt(call, lookup));
        requesterID = matches.customers![0]!.id;
      } catch (error) { return protocol(this.client, call, 'searchTickets', emptySearchTicketsOutput, lookup, 'Gorgias returned an invalid customer page', error, false); }
    }
    const query = queryString({ order_by: 'updated_datetime:desc', limit: input.pageSize || 30, trashed: 'false', customer_id: requesterID || undefined, view_id: input.viewId || undefined, cursor: input.cursor });
    const result = await this.client.exchange(call, resolved.credentials, 'searchTickets', 'GET', '/tickets', query);
    if (result.outcome !== 'succeeded') return queryFailure(this.client, call, result, emptySearchTicketsOutput);
    try { return newQueryBranch('searched', decodeTicketPage(result.response!.body, input), this.client.receipt(call, result)); }
    catch (error) { return protocol(this.client, call, 'searchTickets', emptySearchTicketsOutput, result, 'Gorgias returned an invalid ticket page', error, false); }
  }
}

/** Query one ticket with recent messages. */
export class GetTicketOperation implements Query<GetTicketInput, TicketDetails> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return GetTicketDefinition; }
  /** Invoke one ticket read. */
  async invoke(call: Call, input: GetTicketInput): Promise<QueryAttempt<TicketDetails>> {
    try { requireID(input.ticketId, 'ticketId'); if (!Number.isInteger(input.latestMessageLimit ?? 0) || (input.latestMessageLimit ?? 0) < 0 || (input.latestMessageLimit ?? 0) > 20) throw new TypeError('latestMessageLimit must be 1 to 20 or zero'); }
    catch (error) { return defect('getTicket', emptyTicketDetails, error, false); }
    const resolved = await this.client.resolveCredentials(call, 'getTicket');
    if (!resolved.credentials) return newQueryBranch('defect', emptyTicketDetails, {} as Receipt, resolved.failure);
    const result = await this.client.exchange(call, resolved.credentials, 'getTicket', 'GET', `/tickets/${input.ticketId}`);
    if (result.outcome !== 'succeeded') return queryFailure(this.client, call, result, emptyTicketDetails, 'notFound', input.ticketId);
    try { return newQueryBranch('found', decodeDetails(result.response!.body, input), this.client.receipt(call, result, input.ticketId)); }
    catch (error) { return protocol(this.client, call, 'getTicket', emptyTicketDetails, result, 'Gorgias returned an invalid ticket', error, false); }
  }
}

/** Query customers by primary email address. */
export class FindCustomerByEmailOperation implements Query<FindCustomerByEmailInput, CustomerMatches> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return FindCustomerByEmailDefinition; }
  /** Invoke one customer lookup. */
  async invoke(call: Call, input: FindCustomerByEmailInput): Promise<QueryAttempt<CustomerMatches>> {
    if (!isBareEmail(input.email)) return defect('findCustomerByEmail', emptyCustomerMatches, new TypeError('email must be one bare email address'), false);
    const resolved = await this.client.resolveCredentials(call, 'findCustomerByEmail');
    if (!resolved.credentials) return newQueryBranch('defect', emptyCustomerMatches, {} as Receipt, resolved.failure);
    const result = await this.client.exchange(call, resolved.credentials, 'findCustomerByEmail', 'GET', '/customers', queryString({ email: input.email, limit: 10 }));
    if (result.outcome !== 'succeeded') return queryFailure(this.client, call, result, emptyCustomerMatches);
    try {
      const matches = decodeCustomerMatches(result.response!.body, input.email);
      return newQueryBranch((matches.customers ?? []).length ? 'found' : 'notFound', matches, this.client.receipt(call, result, (matches.customers ?? []).length === 1 ? matches.customers![0]!.id : 0));
    } catch (error) { return protocol(this.client, call, 'findCustomerByEmail', emptyCustomerMatches, result, 'Gorgias returned an invalid customer page', error, false); }
  }
}

function html(text: string): string { return text.replace(/\r\n/g, '\n').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&#34;').replaceAll("'", '&#39;').replaceAll('\n', '<br>'); }
function hasEarlierDispatch(call: Call): boolean {
  try { return call.context.hasLastHeartbeatValue() && call.context.getLastHeartbeatValue() !== null; }
  catch { return true; }
}
async function recordDispatch(call: Call): Promise<boolean> {
  try { await call.context.recordHeartbeat({ gorgiasDispatchedCallId: call.id }); return true; }
  catch { return false; }
}
async function releaseDispatch(call: Call): Promise<void> { try { await call.context.recordHeartbeat(null); } catch { /* A retained marker safely forces read-back. */ } }
function reconcileDelay(delay: number, minimum: number): number { return Math.min(60_000, Math.max(delay, minimum)); }
async function reconcile<T>(call: Call, failure: Failure, output: T, receipt: Receipt, delay = 0, uncertainFailure = failure): Promise<MutationAttempt<T>> {
  if (call.context.attempt < 4 && await recordDispatch(call)) return newMutationRetry(failure, reconcileDelay(delay, 15_000));
  return newMutationUncertain(output, uncertainFailure, receipt);
}
async function reconcileCredentialFailure<T>(call: Call, failure: Failure, output: T, operation: string): Promise<MutationAttempt<T>> {
  const uncertainFailure = gorgiasFailure(failure.kind, operation, 'an earlier attempt of this Step sent the request and the connection credentials are unavailable to confirm it, so it is not sent again');
  return reconcile(call, failure, output, {} as Receipt, 0, uncertainFailure);
}
async function reconcileLookupFailure<T>(call: Call, result: Exchange, output: T, receipt: Receipt, operation: string): Promise<MutationAttempt<T>> {
  const failure = result.failure!;
  const uncertainFailure = gorgiasFailure(failure.kind, operation, `an earlier attempt of this Step sent the request and Gorgias could not confirm it, so it is not sent again: ${failure.message}`);
  if (['rateLimited', 'notSent', 'unavailable', 'invalid'].includes(result.outcome)) return reconcile(call, failure, output, receipt, result.retryAfterMs, uncertainFailure);
  return newMutationUncertain(output, uncertainFailure, receipt);
}
async function sendOutcome<T>(call: Call, result: Exchange, client: GorgiasClient, output: T, notFoundBranch = 'providerRejected'): Promise<MutationAttempt<T> | undefined> {
  if (result.outcome === 'succeeded') return undefined;
  if (result.outcome === 'rateLimited' || result.outcome === 'notSent') { await releaseDispatch(call); return newMutationRetry(result.failure!, result.retryAfterMs); }
  if (result.outcome === 'unavailable' || result.outcome === 'invalid') return newMutationRetry(result.failure!, reconcileDelay(result.retryAfterMs, 2_000));
  if (result.outcome === 'defect') await releaseDispatch(call);
  return mutationFailure(client, call, result, output, notFoundBranch);
}

/** Create one ticket and reconcile uncertain sends by external ID. */
export class CreateTicketOperation implements Mutation<CreateTicketInput, CreateTicketOutput> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return CreateTicketDefinition; }
  /** Derive the external ticket ID. */ idempotencyKey(callId: string): string { return `dex-${callId}`; }
  /** Invoke one create or reconciliation attempt. */
  async invoke(call: Call, input: CreateTicketInput): Promise<MutationAttempt<CreateTicketOutput>> {
    try {
      requireText(input.subject, 'subject'); requireText(input.description, 'description');
      if ([...input.subject].length > 998) throw new TypeError('subject is too long');
      if (!isBareEmail(input.requester.email)) throw new TypeError('requester.email must be one bare email address');
      if (input.requester.name && input.requester.name !== input.requester.name.trim()) throw new TypeError('requester.name cannot have surrounding spaces');
      if (input.status) validateStatus(input.status); if (input.priority) validatePriority(input.priority);
      validateTags(input.tags, 'tags'); optionalID(input.assigneeUserId, 'assigneeUserId'); optionalID(input.assigneeTeamId, 'assigneeTeamId');
    } catch (error) { return defect('createTicket', emptyCreateTicketOutput, error, true); }
    const earlier = hasEarlierDispatch(call);
    const resolved = await this.client.resolveCredentials(call, 'createTicket');
    if (!resolved.credentials) return earlier ? reconcileCredentialFailure(call, resolved.failure!, emptyCreateTicketOutput, 'createTicket') : newMutationBranch('defect', emptyCreateTicketOutput, {} as Receipt, resolved.failure);
    if (earlier || call.context.attempt > 1) {
      const lookup = await this.lookup(call, resolved.credentials);
      if (lookup.result.outcome !== 'succeeded') return earlier ? reconcileLookupFailure(call, lookup.result, emptyCreateTicketOutput, lookup.receipt, 'createTicket') : unsentRequestAttempt(this.client, call, lookup.result, emptyCreateTicketOutput);
      if (lookup.ticket) return newMutationBranch('created', { ticket: lookup.ticket, wasCreatedByEarlierAttempt: true }, lookup.receipt);
      if (earlier) return reconcile(call, gorgiasFailure('TRANSPORT', 'createTicket', 'an earlier attempt of this Step sent the request and Gorgias shows no ticket with its external ID, so it is not sent again'), emptyCreateTicketOutput, lookup.receipt);
    }
    if (!await recordDispatch(call)) return newMutationRetry(gorgiasFailure('AVAILABILITY', 'createTicket', 'Dex did not record the dispatch checkpoint; nothing was sent'), 0);
    const requester = { email: input.requester.email, ...(input.requester.name ? { name: input.requester.name } : {}) };
    const payload = {
      customer: requester, subject: input.subject, channel: 'api', via: 'api', from_agent: false,
      ...(input.status ? { status: input.status } : {}), ...(input.priority ? { priority: input.priority } : {}),
      ...(input.tags?.length ? { tags: input.tags.map(name => ({ name })) } : {}),
      ...(input.assigneeUserId ? { assignee_user: { id: input.assigneeUserId } } : {}),
      ...(input.assigneeTeamId ? { assignee_team: { id: input.assigneeTeamId } } : {}),
      external_id: call.idempotencyKey,
      messages: [{ channel: 'api', via: 'api', from_agent: false, sender: requester, subject: input.subject, body_text: input.description, body_html: html(input.description), stripped_text: input.description }],
    };
    const result = await this.client.exchange(call, resolved.credentials, 'createTicket', 'POST', '/tickets', undefined, payload);
    const terminal = await sendOutcome(call, result, this.client, emptyCreateTicketOutput);
    if (terminal) return terminal;
    try { return newMutationBranch('created', { ticket: decodeTicket(objectBody(result.response!.body)) }, this.client.receipt(call, result)); }
    catch (error) { return newMutationRetry(gorgiasFailure('PROTOCOL', 'createTicket', `Gorgias accepted the ticket but returned an invalid ticket: ${String(error)}`), 2_000); }
  }
  private async lookup(call: Call, credentials: Credentials): Promise<{ result: Exchange; receipt: Receipt; ticket?: Ticket }> {
    const result = await this.client.exchange(call, credentials, 'createTicket', 'GET', '/tickets', queryString({ external_id: call.idempotencyKey, limit: 5 }));
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return { result, receipt };
    try {
      const page = decodeList(result.response!.body);
      for (const wire of page.data) {
        try {
          const ticket = decodeTicket(wire);
          if (ticket.externalId === call.idempotencyKey) return { result, receipt: this.client.receipt(call, result, ticket.id), ticket };
        } catch { /* A malformed ticket does not invalidate the rest of the page. */ }
      }
      return { result, receipt };
    } catch (error) { return { result: { outcome: 'invalid', response: result.response!, failure: gorgiasFailure('PROTOCOL', 'createTicket', `Gorgias returned an invalid ticket page: ${String(error)}`), retryAfterMs: 0 }, receipt }; }
  }
}

/** Apply absolute ticket field and tag changes. */
export class UpdateTicketOperation implements Mutation<UpdateTicketInput, UpdateTicketOutput> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return UpdateTicketDefinition; }
  /** Correlate repeat safe updates. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, compare, and apply one update attempt. */
  async invoke(call: Call, input: UpdateTicketInput): Promise<MutationAttempt<UpdateTicketOutput>> {
    try {
      requireID(input.ticketId, 'ticketId'); if (input.status) validateStatus(input.status); if (input.priority) validatePriority(input.priority);
      optionalID(input.assigneeUserId, 'assigneeUserId'); optionalID(input.assigneeTeamId, 'assigneeTeamId');
      validateTags(input.addTags, 'addTags'); validateTags(input.removeTags, 'removeTags');
      if (input.addTags?.some(tag => input.removeTags?.includes(tag))) throw new TypeError('tag is in both addTags and removeTags');
      if (!input.status && !input.priority && !input.assigneeUserId && !input.assigneeTeamId && !input.addTags?.length && !input.removeTags?.length) throw new TypeError('at least one change is required');
    } catch (error) { return defect('updateTicket', emptyUpdateTicketOutput, error, true); }
    const resolved = await this.client.resolveCredentials(call, 'updateTicket');
    if (!resolved.credentials) return newMutationBranch('defect', emptyUpdateTicketOutput, {} as Receipt, resolved.failure);
    const ticketID = input.ticketId;
    const read = await this.client.exchange(call, resolved.credentials, 'updateTicket', 'GET', `/tickets/${ticketID}`);
    if (read.outcome !== 'succeeded') return mutationFailure(this.client, call, read, emptyUpdateTicketOutput, 'notFound', ticketID);
    let current: Ticket;
    try { current = decodeTicket(objectBody(read.response!.body)); if (current.id !== ticketID) throw new TypeError('ticket response is for another ticket'); }
    catch (error) { return protocol(this.client, call, 'updateTicket', emptyUpdateTicketOutput, read, 'Gorgias returned an invalid ticket', error, true); }
    const fields: Record<string, unknown> = {};
    if (input.status && input.status !== current.status) fields.status = input.status;
    if (input.priority && input.priority !== current.priority) fields.priority = input.priority;
    if (input.assigneeUserId && input.assigneeUserId !== current.assigneeUserId) fields.assignee_user = { id: input.assigneeUserId };
    if (input.assigneeTeamId && input.assigneeTeamId !== current.assigneeTeamId) fields.assignee_team = { id: input.assigneeTeamId };
    const addTags = (input.addTags ?? []).filter(tag => !current.tags?.includes(tag));
    const removeTags = (input.removeTags ?? []).filter(tag => current.tags?.includes(tag));
    if (!Object.keys(fields).length && !addTags.length && !removeTags.length) return newMutationBranch('updated', { ticket: current, wasAlreadyApplied: true }, this.client.receipt(call, undefined, ticketID));
    const writes: Array<{ method: string; path: string; payload: unknown }> = [];
    if (addTags.length) writes.push({ method: 'POST', path: `/tickets/${ticketID}/tags`, payload: { names: addTags } });
    if (removeTags.length) writes.push({ method: 'DELETE', path: `/tickets/${ticketID}/tags`, payload: { names: removeTags } });
    if (Object.keys(fields).length) writes.push({ method: 'PUT', path: `/tickets/${ticketID}`, payload: fields });
    let hasAppliedWrite = false;
    for (const write of writes) {
      const result = await this.client.exchange(call, resolved.credentials, 'updateTicket', write.method, write.path, undefined, write.payload);
      if (result.outcome !== 'succeeded') {
        let output: UpdateTicketOutput = { ticket: current };
        if (hasAppliedWrite && !['rateLimited', 'notSent', 'unavailable'].includes(result.outcome)) {
          const reread = await this.client.exchange(call, resolved.credentials, 'updateTicket', 'GET', `/tickets/${ticketID}`);
          if (reread.outcome === 'succeeded') { try { output = { ticket: decodeTicket(objectBody(reread.response!.body)) }; } catch { /* The earlier valid ticket remains safe to return. */ } }
        }
        return mutationFailure(this.client, call, result, output, 'notFound', ticketID);
      }
      hasAppliedWrite = true;
      if (write.method === 'PUT') {
        try {
          const ticket = decodeTicket(objectBody(result.response!.body));
          if (ticket.id !== ticketID) throw new TypeError('updated ticket ID differs');
          return newMutationBranch('updated', { ticket }, this.client.receipt(call, result, ticketID));
        } catch (error) { return protocol(this.client, call, 'updateTicket', { ticket: current }, result, 'Gorgias returned an invalid updated ticket', error, true); }
      }
    }
    const reread = await this.client.exchange(call, resolved.credentials, 'updateTicket', 'GET', `/tickets/${ticketID}`);
    if (reread.outcome !== 'succeeded') return mutationFailure(this.client, call, reread, { ticket: current }, 'notFound', ticketID);
    try { return newMutationBranch('updated', { ticket: decodeTicket(objectBody(reread.response!.body)) }, this.client.receipt(call, undefined, ticketID)); }
    catch (error) { return protocol(this.client, call, 'updateTicket', { ticket: current }, reread, 'Gorgias returned an invalid ticket', error, true); }
  }
}

type MessageWire = Record<string, unknown>;
function messageList(wire: Record<string, unknown>): MessageWire[] { return Array.isArray(wire.messages) ? wire.messages.filter(value => value && typeof value === 'object') as MessageWire[] : []; }
function address(value: unknown): string { return value && typeof value === 'object' && 'address' in value && typeof value.address === 'string' ? value.address : ''; }
function messageSource(wire: MessageWire): Record<string, unknown> | undefined { return wire.source && typeof wire.source === 'object' ? wire.source as Record<string, unknown> : undefined; }
function recipientAddresses(message: MessageWire): string[] {
  const source = messageSource(message);
  if (!source) return [];
  const sender = address(source.from);
  const recipients = [...(Array.isArray(source.to) ? source.to : []), ...(Array.isArray(source.cc) ? source.cc : [])].map(address);
  return [...new Map(recipients.filter(item => isBareEmail(item) && item.toLowerCase() !== sender.toLowerCase()).map(item => [item.toLowerCase(), item])).values()];
}
function newestMessage(left: MessageWire, right: MessageWire): number {
  const leftTime = typeof left.created_datetime === 'string' ? Date.parse(left.created_datetime) : NaN;
  const rightTime = typeof right.created_datetime === 'string' ? Date.parse(right.created_datetime) : NaN;
  return Number.isNaN(leftTime) || Number.isNaN(rightTime) || leftTime === rightTime ? Number(right.id) - Number(left.id) : rightTime - leftTime;
}
function replyRoute(ticketWire: Record<string, unknown>): { supportAddress: string; customerAddress: string; subject: string; integrationID: number } {
  const messages = messageList(ticketWire);
  const newestCustomer = messages.filter(message => !message.from_agent && message.channel === 'email' && isBareEmail(address(messageSource(message)?.from)) && recipientAddresses(message).length).sort(newestMessage)[0];
  if (!newestCustomer) throw new TypeError('the ticket has no email from the customer to reply to; add an internal note instead');
  const integrationID = typeof newestCustomer.integration_id === 'number' ? newestCustomer.integration_id : 0;
  const newestAgent = typeof newestCustomer.integration_id === 'number'
    ? messages.filter(message => message.from_agent === true && message.channel === 'email' && typeof message.integration_id === 'number' && message.integration_id === integrationID && isBareEmail(address(messageSource(message)?.from))).sort(newestMessage)[0]
    : undefined;
  const recipients = recipientAddresses(newestCustomer);
  const supportAddress = newestAgent ? address(messageSource(newestAgent)?.from) : recipients.length === 1 ? recipients[0] : '';
  if (!supportAddress) throw new TypeError("the customer's newest email reached several addresses and none is known to be the helpdesk's");
  const rawSubject = typeof newestCustomer.subject === 'string' && newestCustomer.subject ? newestCustomer.subject : String(ticketWire.subject ?? '');
  return { supportAddress, customerAddress: address(messageSource(newestCustomer)?.from), subject: rawSubject.toLowerCase().startsWith('re:') ? rawSubject : `Re: ${rawSubject}`, integrationID };
}

/** Add one internal note or routed email reply with read-back recovery. */
export class AddNoteOperation implements Mutation<AddNoteInput, AddNoteOutput> {
  /** Bind a client. */ constructor(private readonly client: GorgiasClient) {}
  /** Return manifest identity. */ definition() { return AddNoteDefinition; }
  /** Derive the message external ID. */ idempotencyKey(callId: string): string { return `dex-${callId}`; }
  /** Invoke one note send or reconciliation attempt. */
  async invoke(call: Call, input: AddNoteInput): Promise<MutationAttempt<AddNoteOutput>> {
    try { requireID(input.ticketId, 'ticketId'); requireText(input.body, 'body'); }
    catch (error) { return defect('addNote', emptyAddNoteOutput, error, true); }
    const earlier = hasEarlierDispatch(call);
    const resolved = await this.client.resolveCredentials(call, 'addNote');
    if (!resolved.credentials) return earlier ? reconcileCredentialFailure(call, resolved.failure!, emptyAddNoteOutput, 'addNote') : newMutationBranch('defect', emptyAddNoteOutput, {} as Receipt, resolved.failure);
    if (earlier || call.context.attempt > 1) {
      const lookup = await this.lookup(call, resolved.credentials, input.ticketId);
      if (lookup.result.outcome !== 'succeeded') return earlier ? reconcileLookupFailure(call, lookup.result, emptyAddNoteOutput, lookup.receipt, 'addNote') : unsentRequestAttempt(this.client, call, lookup.result, emptyAddNoteOutput, 'notFound', input.ticketId);
      if (lookup.message) return newMutationBranch('added', { message: lookup.message, wasCreatedByEarlierAttempt: true }, lookup.receipt);
      if (earlier) return reconcile(call, gorgiasFailure('TRANSPORT', 'addNote', 'an earlier attempt of this Step sent the request and Gorgias shows no message with its external ID, so it is not sent again'), emptyAddNoteOutput, lookup.receipt);
    }
    const payload: Record<string, unknown> = { channel: 'internal-note', via: 'api', from_agent: true, public: false, sender: { email: resolved.credentials.email }, body_text: input.body, body_html: html(input.body), external_id: call.idempotencyKey };
    if (input.isPublicReply) {
      const read = await this.client.exchange(call, resolved.credentials, 'addNote', 'GET', `/tickets/${input.ticketId}`);
      if (read.outcome !== 'succeeded') return mutationFailure(this.client, call, read, emptyAddNoteOutput, 'notFound', input.ticketId, 'providerRejected');
      let ticketWire: Record<string, unknown>;
      try {
        ticketWire = objectBody(read.response!.body);
        const ticket = decodeTicket(ticketWire);
        if (ticket.id !== input.ticketId) throw new TypeError('ticket response is for another ticket');
      } catch (error) { return protocol(this.client, call, 'addNote', emptyAddNoteOutput, read, 'Gorgias returned an invalid ticket, so the reply could not be routed', error, true, 'providerRejected'); }
      try {
        const route = replyRoute(ticketWire);
        payload.channel = 'email'; payload.public = true; payload.subject = route.subject;
        if (route.integrationID) payload.integration_id = route.integrationID;
        payload.receiver = { email: route.customerAddress };
        payload.source = { type: 'email', from: { address: route.supportAddress }, to: [{ address: route.customerAddress }] };
      } catch (error) { return newMutationBranch('providerRejected', emptyAddNoteOutput, this.client.receipt(call, read, input.ticketId), gorgiasFailure('VALIDATION', 'addNote', error instanceof Error ? error.message : 'Gorgias could not route the reply')); }
    }
    if (!await recordDispatch(call)) return newMutationRetry(gorgiasFailure('AVAILABILITY', 'addNote', 'Dex did not record the dispatch checkpoint; nothing was sent'), 0);
    const result = await this.client.exchange(call, resolved.credentials, 'addNote', 'POST', `/tickets/${input.ticketId}/messages`, undefined, payload);
    const terminal = await sendOutcome(call, result, this.client, emptyAddNoteOutput, 'notFound');
    if (terminal) return terminal;
    try {
      const message = decodeMessage(objectBody(result.response!.body));
      if (message.ticketId && message.ticketId !== input.ticketId) throw new TypeError('message response is for another ticket');
      if (message.externalId && message.externalId !== call.idempotencyKey) throw new TypeError('message response is another message');
      message.ticketId = input.ticketId;
      return newMutationBranch('added', { message }, this.client.receipt(call, result, message.id));
    } catch (error) { return newMutationRetry(gorgiasFailure('PROTOCOL', 'addNote', `Gorgias accepted the message but returned an invalid message: ${String(error)}`), 2_000); }
  }
  private async lookup(call: Call, credentials: Credentials, ticketID: number): Promise<{ result: Exchange; receipt: Receipt; message?: TicketMessage }> {
    const result = await this.client.exchange(call, credentials, 'addNote', 'GET', `/tickets/${ticketID}`);
    const receipt = this.client.receipt(call, result, ticketID);
    if (result.outcome !== 'succeeded') return { result, receipt };
    try {
      const ticketWire = objectBody(result.response!.body);
      if (decodeTicket(ticketWire).id !== ticketID) throw new TypeError('ticket response is for another ticket');
      for (const wire of messageList(ticketWire)) {
        if (wire.external_id !== call.idempotencyKey) continue;
        try { const message = decodeMessage(wire); message.ticketId = ticketID; return { result, receipt: this.client.receipt(call, result, message.id), message }; }
        catch { /* Keep looking for a valid message with this external ID. */ }
      }
      return { result, receipt };
    } catch (error) { return { result: { outcome: 'invalid', response: result.response!, failure: gorgiasFailure('PROTOCOL', 'addNote', `Gorgias returned an invalid ticket: ${String(error)}`), retryAfterMs: 0 }, receipt }; }
  }
}
