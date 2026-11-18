// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type Receipt } from '@superdurable/connectors-sdk';
import { CheckpointError, DispatchNotAllowedError, InsufficientTimeError, newFence, type Decision, type Fence } from '@superdurable/connectors-sdk/singledispatch';
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
/** Only these statuses prove Gorgias applied nothing; any other non-2xx answer may follow an applied write. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
const retryOutcomes = new Set<Exchange['outcome']>(['rateLimited', 'notSent', 'unavailable']);

function reconcileDelay(delay: number, minimum: number): number { return Math.min(60_000, Math.max(delay, minimum)); }
function isConclusiveWriteFailure(result: Exchange): boolean {
  return result.outcome === 'defect' || ((result.outcome === 'notFound' || result.outcome === 'rejected') && isDefiniteRefusal(result));
}
/** A refusal status whose body was read, which proves Gorgias applied nothing. */
function isDefiniteRefusal(result: Exchange): boolean { return definiteRefusalStatuses.has(result.response?.status ?? 0) && !result.bodyTooLarge; }
/** A 2xx, or any answer naming a created object: Gorgias applied the write. */
function isAcceptedWrite(result: Exchange): boolean {
  const status = result.response?.status ?? 0;
  return (status >= 200 && status < 300) || (result.object?.id ?? 0) > 0;
}
/** The ID an accepted answer names for this Step's write; zero when it names none or another object. */
function acceptedObjectID(result: Exchange, externalID: string, ticketID = 0): number {
  const object = result.object ?? { id: 0, externalId: '', ticketId: 0 };
  const isOtherExternalID = object.externalId !== '' && object.externalId !== externalID;
  const isOtherTicket = ticketID !== 0 && object.ticketId !== 0 && object.ticketId !== ticketID;
  return object.id < 1 || isOtherExternalID || isOtherTicket ? 0 : object.id;
}

/** A fence decision that lets this attempt read back and send, with its send-cap facts. */
class WritePermit {
  /** Bind the fence and its decision. */ constructor(readonly fence: Fence, readonly decision: Decision) {}
  /** The Gorgias ID an earlier attempt checkpointed after Gorgias accepted the write, or empty. */ get savedProviderId(): string { return this.decision.marker?.providerId ?? ''; }
  /** An earlier attempt may have sent, so Gorgias is searched first and nothing is refused. */
  get hasPossibleEarlierSend(): boolean { return this.decision.kind !== 'sendAllowed' || (this.decision.priorSendUpperBound ?? 0) > 0 || this.savedProviderId !== ''; }
  /** Room under the cap and no saved ID proving Gorgias accepted a send. */ get canSendAfterUnconfirmedReadBack(): boolean { return this.decision.kind !== 'readBackRequired' && !this.savedProviderId; }
  /** The saved provider ID as a number, or zero when it is unreadable. */
  savedProviderNumber(): number { return /^[0-9]+$/.test(this.savedProviderId) && Number.isSafeInteger(Number(this.savedProviderId)) ? Number(this.savedProviderId) : 0; }
}

/** Start the SDK fence under the manifest send cap; an attempt ends this attempt before any request. */
function beginWrite<T>(call: Call, operation: string, definition: MutationDefinition, output: T): WritePermit | MutationAttempt<T> {
  let fence: Fence;
  try { fence = newFence(call, definition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: definitionMaxSends(definition) }); }
  catch { return newMutationBranch('defect', output, {} as Receipt, gorgiasFailure('LOCAL_DEFECT', operation, 'single-dispatch fence could not start')); }
  const decision = fence.decide();
  // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
  if (decision.kind === 'failClosed') return newMutationRetry(gorgiasFailure('AVAILABILITY', operation, 'the dispatch checkpoint could not be read; nothing was sent in this attempt'));
  return new WritePermit(fence, decision);
}
/** Map unusable credentials before any send; after a possible earlier send nothing is recorded, a transient failure retries, and a terminal one selects uncertain. */
async function credentialFailureAttempt<T>(permit: WritePermit, operation: string, failure: Failure, isTerminal: boolean, output: T): Promise<MutationAttempt<T>> {
  if (permit.hasPossibleEarlierSend) {
    // notSent would hide the earlier possible send, so nothing is recorded.
    if (!isTerminal) return newMutationRetry(gorgiasFailure('AVAILABILITY', operation, 'connection credentials could not be read; nothing was sent in this attempt'));
    return newMutationUncertain(output, gorgiasFailure(failure.kind, operation, 'an earlier attempt of this Step may have sent the request and the connection credentials are unusable to read it back; reconnect the connection'), {} as Receipt);
  }
  try { await permit.fence.recordNotSent(); } catch { return newMutationRetry(gorgiasFailure('AVAILABILITY', operation, 'Dex did not record the not-sent checkpoint')); }
  if (!isTerminal) return newMutationRetry(failure);
  return newMutationBranch('defect', output, {} as Receipt, failure);
}
/** Map an external-ID lookup; undefined means this attempt may send. Only a readable answer permits a resend: a refused lookup, such as a missing ticket or an auth error, selects uncertain. */
function readBackAttempt<T>(permit: WritePermit, result: Exchange, found: MutationAttempt<T> | undefined, receipt: Receipt, operation: string, resource: string, output: T): MutationAttempt<T> | undefined {
  if (result.outcome === 'succeeded' && found) return found;
  if (retryOutcomes.has(result.outcome)) return newMutationRetry(result.failure!, reconcileDelay(result.retryAfterMs, 15_000));
  if ((result.outcome === 'succeeded' || result.outcome === 'invalid') && permit.canSendAfterUnconfirmedReadBack) return undefined;
  const failure = result.outcome === 'succeeded'
    ? gorgiasFailure('TRANSPORT', operation, `earlier attempts of this Step may have sent the request, the send cap is reached, and Gorgias shows no ${resource} with its external ID`)
    : gorgiasFailure(result.failure!.kind, operation, `earlier attempts of this Step may have sent the request, and Gorgias could not confirm it: ${result.failure!.message}`);
  return newMutationUncertain(output, failure, receipt);
}
/** Map the read-back of a write Gorgias accepted; it never permits a resend. A transient failure retries only when the provider ID is checkpointed, so the next attempt reads back by it. */
function acceptedWriteAttempt<T>(isProviderIDSaved: boolean, result: Exchange, found: MutationAttempt<T> | undefined, receipt: Receipt, operation: string, resource: string, output: T): MutationAttempt<T> {
  if (result.outcome === 'succeeded' && found) return found;
  if (isProviderIDSaved && retryOutcomes.has(result.outcome)) return newMutationRetry(result.failure!, reconcileDelay(result.retryAfterMs, 15_000));
  const failure = result.outcome === 'succeeded'
    ? gorgiasFailure('PROTOCOL', operation, `Gorgias accepted the request, but reading the ${resource} back did not confirm it`)
    : gorgiasFailure(result.failure!.kind, operation, `Gorgias accepted the request, but the ${resource} could not be read back: ${result.failure!.message}`);
  return newMutationUncertain(output, failure, receipt);
}
async function preSendRetry<T>(permit: WritePermit, failure: Failure, retryAfterMs: number): Promise<MutationAttempt<T>> {
  // A lost notSent leaves no marker, so the next attempt reads back before it sends.
  if (!permit.hasPossibleEarlierSend) { try { await permit.fence.recordNotSent(); } catch { /* counted as a possible send */ } }
  return newMutationRetry(failure, retryAfterMs);
}
/** Dispatch one POST under the fence; a refused dispatch returns its Retry attempt. */
async function sendWrite<T>(permit: WritePermit, client: GorgiasClient, call: Call, credentials: Credentials, operation: string, path: string, payload: unknown): Promise<Exchange | MutationAttempt<T>> {
  try { return await permit.fence.dispatch(signal => client.exchange(call, credentials, operation, 'POST', path, undefined, payload, signal)); }
  catch (error) {
    // Dispatch refused before sending, so nothing reached Gorgias in this attempt.
    if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof DispatchNotAllowedError || error instanceof AggregateError) return newMutationRetry(gorgiasFailure('AVAILABILITY', operation, 'Dex did not record the dispatch checkpoint or too little attempt time remained; nothing was sent'));
    // The claim stays, so the next attempt counts this send and reads back first.
    return newMutationRetry(gorgiasFailure('TRANSPORT', operation, 'Gorgias request failed before a response arrived'), 2_000);
  }
}
/** Map a sent write; undefined means Gorgias accepted it, even with an unusable answer. Any outcome but a definite refusal retries, and the next attempt reads back. */
async function writeAttempt<T>(permit: WritePermit, result: Exchange, receipt: Receipt, output: T, notFoundBranch: string): Promise<MutationAttempt<T> | undefined> {
  if (isAcceptedWrite(result)) return undefined;
  const failure = result.failure!;
  if (result.outcome === 'rateLimited' || result.outcome === 'notSent' || (result.outcome === 'unavailable' && result.response?.status === 503)) {
    // A lost checkpoint leaves the claim, so the next attempt counts this send and reads back.
    try { await permit.fence.recordCanResend(); } catch { /* counted as a possible send */ }
    return newMutationRetry(failure, result.retryAfterMs);
  }
  let branch = '';
  if (result.outcome === 'notFound' && isDefiniteRefusal(result)) branch = notFoundBranch;
  // The request was never built; after a possible earlier send this selects uncertain.
  else if (result.outcome === 'defect') { branch = 'defect'; receipt = {} as Receipt; }
  else if (result.outcome === 'rejected' && isDefiniteRefusal(result)) branch = 'providerRejected';
  // A 3xx, 408, 409, other 5xx, unlisted status, or unread refusal body may follow an applied write.
  if (!branch) return newMutationRetry(failure, reconcileDelay(result.retryAfterMs, 2_000));
  if (permit.hasPossibleEarlierSend) return newMutationUncertain(output, failure, receipt);
  try { await permit.fence.recordCanResend(); } catch { return newMutationRetry(gorgiasFailure('AVAILABILITY', failure.operation, 'Dex did not record the refusal checkpoint')); }
  return newMutationBranch(branch, output, receipt, failure);
}
function isExchange<T>(value: Exchange | MutationAttempt<T>): value is Exchange { return 'outcome' in value; }
/** Checkpoint the accepted provider ID and report whether Dex saved it. */
async function recordAcceptedWrite(permit: WritePermit, providerID: number): Promise<boolean> {
  try { await permit.fence.recordProviderId(String(providerID)); return true; } catch { return false; }
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
    const permit = beginWrite(call, 'createTicket', CreateTicketDefinition, emptyCreateTicketOutput);
    if (!(permit instanceof WritePermit)) return permit;
    const resolved = await this.client.resolveWriteCredentials(call, 'createTicket');
    if (!resolved.credentials) return credentialFailureAttempt(permit, 'createTicket', resolved.failure!, resolved.isTerminal ?? true, emptyCreateTicketOutput);
    // Gorgias accepted an earlier attempt's ticket, so it is read back by ID and never sent again.
    if (permit.savedProviderId) return this.confirmTicket(call, resolved.credentials, permit.savedProviderNumber(), true, true);
    if (permit.hasPossibleEarlierSend) {
      const lookup = await this.lookup(call, resolved.credentials);
      const found = lookup.ticket ? newMutationBranch('created', { ticket: lookup.ticket, wasCreatedByEarlierAttempt: true }, lookup.receipt) : undefined;
      const readBack = readBackAttempt(permit, lookup.result, found, lookup.receipt, 'createTicket', 'ticket', emptyCreateTicketOutput);
      if (readBack) return readBack;
      // Gorgias shows no ticket with the external ID, so this attempt resends below the cap.
    }
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
    const result = await sendWrite<CreateTicketOutput>(permit, this.client, call, resolved.credentials, 'createTicket', '/tickets', payload);
    if (!isExchange(result)) return result;
    const terminal = await writeAttempt(permit, result, this.client.receipt(call, result), emptyCreateTicketOutput, 'providerRejected');
    if (terminal) return terminal;
    let ticket: Ticket | undefined;
    if (result.outcome === 'succeeded') { try { ticket = decodeTicket(objectBody(result.response!.body)); } catch { ticket = undefined; } }
    if (!ticket) return this.confirmAcceptedTicket(call, resolved.credentials, permit, result);
    // The answer already proves acceptance, so a lost checkpoint changes nothing.
    await recordAcceptedWrite(permit, ticket.id);
    return newMutationBranch('created', { ticket }, this.client.receipt(call, result));
  }
  /** Read back a ticket Gorgias accepted with an unusable answer, by ID when the answer names this Step's ticket, otherwise once by external ID; it never resends. */
  private async confirmAcceptedTicket(call: Call, credentials: Credentials, permit: WritePermit, result: Exchange): Promise<MutationAttempt<CreateTicketOutput>> {
    const ticketID = acceptedObjectID(result, call.idempotencyKey ?? '');
    if (ticketID) return this.confirmTicket(call, credentials, ticketID, await recordAcceptedWrite(permit, ticketID), false);
    const lookup = await this.lookup(call, credentials);
    const found = lookup.ticket ? newMutationBranch('created', { ticket: lookup.ticket }, lookup.receipt) : undefined;
    return acceptedWriteAttempt(false, lookup.result, found, lookup.receipt, 'createTicket', 'ticket', emptyCreateTicketOutput);
  }
  /** Read GET /api/tickets/{id} and confirm it is this Step's ticket by its external ID. */
  private async confirmTicket(call: Call, credentials: Credentials, ticketID: number, isProviderIDSaved: boolean, wasCreatedByEarlierAttempt: boolean): Promise<MutationAttempt<CreateTicketOutput>> {
    if (ticketID < 1) return newMutationUncertain(emptyCreateTicketOutput, gorgiasFailure('PROTOCOL', 'createTicket', 'Gorgias accepted the ticket, but the checkpointed ticket ID is unreadable'), {} as Receipt);
    const result = await this.client.exchange(call, credentials, 'createTicket', 'GET', `/tickets/${ticketID}`);
    const receipt = this.client.receipt(call, result, ticketID);
    let found: MutationAttempt<CreateTicketOutput> | undefined;
    if (result.outcome === 'succeeded') {
      try {
        const ticket = decodeTicket(objectBody(result.response!.body));
        if (ticket.id === ticketID && ticket.externalId === call.idempotencyKey) found = newMutationBranch('created', { ticket, ...(wasCreatedByEarlierAttempt ? { wasCreatedByEarlierAttempt } : {}) }, receipt);
      } catch { /* An unreadable ticket does not confirm the write. */ }
    }
    return acceptedWriteAttempt(isProviderIDSaved, result, found, receipt, 'createTicket', 'ticket', emptyCreateTicketOutput);
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
    const resolved = await this.client.resolveWriteCredentials(call, 'updateTicket');
    // defect would hide a change an earlier attempt may have sent; repeating it is safe.
    if (!resolved.credentials && (resolved.isTerminal === false || call.context.attempt > 1)) return newMutationRetry(resolved.failure!);
    if (!resolved.credentials) return newMutationBranch('defect', emptyUpdateTicketOutput, {} as Receipt, resolved.failure);
    const ticketID = input.ticketId;
    const read = await this.client.exchange(call, resolved.credentials, 'updateTicket', 'GET', `/tickets/${ticketID}`);
    // An earlier attempt's write may have applied, so only a definite refusal selects a branch.
    if (read.outcome !== 'succeeded') return isConclusiveWriteFailure(read) ? mutationFailure(this.client, call, read, emptyUpdateTicketOutput, 'notFound', ticketID) : newMutationRetry(read.failure!, read.retryAfterMs);
    let current: Ticket;
    try { current = decodeTicket(objectBody(read.response!.body)); if (current.id !== ticketID) throw new TypeError('ticket response is for another ticket'); }
    catch (error) { return newMutationRetry(gorgiasFailure('PROTOCOL', 'updateTicket', `Gorgias returned an invalid ticket: ${error instanceof Error ? error.message : 'invalid response'}`)); }
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
        // After a possible write, only a definite refusal selects a branch; anything else retries the change.
        if (!isConclusiveWriteFailure(result)) return newMutationRetry(result.failure!, result.retryAfterMs);
        let output: UpdateTicketOutput = { ticket: current };
        if (hasAppliedWrite) {
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
        } catch (error) {
          // The PUT sets final values, so an unusable answer repeats it.
          return newMutationRetry(gorgiasFailure('PROTOCOL', 'updateTicket', `Gorgias accepted the change but returned an invalid updated ticket: ${error instanceof Error ? error.message : 'invalid response'}`));
        }
      }
    }
    const reread = await this.client.exchange(call, resolved.credentials, 'updateTicket', 'GET', `/tickets/${ticketID}`);
    if (reread.outcome !== 'succeeded') return isConclusiveWriteFailure(reread) ? mutationFailure(this.client, call, reread, { ticket: current }, 'notFound', ticketID) : newMutationRetry(reread.failure!, reread.retryAfterMs);
    try { return newMutationBranch('updated', { ticket: decodeTicket(objectBody(reread.response!.body)) }, this.client.receipt(call, undefined, ticketID)); }
    catch (error) { return newMutationRetry(gorgiasFailure('PROTOCOL', 'updateTicket', `Gorgias returned an invalid ticket after the change: ${error instanceof Error ? error.message : 'invalid response'}`)); }
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

/** Select branch on a first send; after a possible earlier send, select uncertain. */
function replyRouteFailure(permit: WritePermit, branch: string, failure: Failure, receipt: Receipt): MutationAttempt<AddNoteOutput> {
  if (permit.hasPossibleEarlierSend) return newMutationUncertain(emptyAddNoteOutput, gorgiasFailure(failure.kind, 'addNote', `an earlier attempt of this Step may have sent the reply, and the ticket could not be read to route it again: ${failure.message}`), receipt);
  return newMutationBranch(branch, emptyAddNoteOutput, receipt, failure);
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
    const permit = beginWrite(call, 'addNote', AddNoteDefinition, emptyAddNoteOutput);
    if (!(permit instanceof WritePermit)) return permit;
    const resolved = await this.client.resolveWriteCredentials(call, 'addNote');
    if (!resolved.credentials) return credentialFailureAttempt(permit, 'addNote', resolved.failure!, resolved.isTerminal ?? true, emptyAddNoteOutput);
    // Gorgias accepted an earlier attempt's message, so it is read back by ID and never sent again.
    if (permit.savedProviderId) return this.confirmMessage(call, resolved.credentials, input.ticketId, permit.savedProviderNumber(), true, true);
    if (permit.hasPossibleEarlierSend) {
      const lookup = await this.lookup(call, resolved.credentials, input.ticketId);
      const found = lookup.message ? newMutationBranch('added', { message: lookup.message, wasCreatedByEarlierAttempt: true }, lookup.receipt) : undefined;
      const readBack = readBackAttempt(permit, lookup.result, found, lookup.receipt, 'addNote', 'message', emptyAddNoteOutput);
      if (readBack) return readBack;
      // The ticket shows no message with the external ID, so this attempt resends below the cap.
    }
    const payload: Record<string, unknown> = { channel: 'internal-note', via: 'api', from_agent: true, public: false, sender: { email: resolved.credentials.email }, body_text: input.body, body_html: html(input.body), external_id: call.idempotencyKey };
    if (input.isPublicReply) {
      const read = await this.client.exchange(call, resolved.credentials, 'addNote', 'GET', `/tickets/${input.ticketId}`);
      const readReceipt = this.client.receipt(call, read, input.ticketId);
      if (retryOutcomes.has(read.outcome)) return preSendRetry(permit, read.failure!, read.retryAfterMs);
      if (read.outcome !== 'succeeded') return replyRouteFailure(permit, read.outcome === 'notFound' ? 'notFound' : read.outcome === 'defect' ? 'defect' : 'providerRejected', read.failure!, readReceipt);
      let ticketWire: Record<string, unknown>;
      try {
        ticketWire = objectBody(read.response!.body);
        const ticket = decodeTicket(ticketWire);
        if (ticket.id !== input.ticketId) throw new TypeError('ticket response is for another ticket');
      } catch (error) { return replyRouteFailure(permit, 'providerRejected', gorgiasFailure('PROTOCOL', 'addNote', `Gorgias returned an invalid ticket, so the reply could not be routed: ${error instanceof Error ? error.message : 'invalid response'}`), readReceipt); }
      try {
        const route = replyRoute(ticketWire);
        payload.channel = 'email'; payload.public = true; payload.subject = route.subject;
        if (route.integrationID) payload.integration_id = route.integrationID;
        payload.receiver = { email: route.customerAddress };
        payload.source = { type: 'email', from: { address: route.supportAddress }, to: [{ address: route.customerAddress }] };
      } catch (error) { return replyRouteFailure(permit, 'providerRejected', gorgiasFailure('VALIDATION', 'addNote', error instanceof Error ? error.message : 'Gorgias could not route the reply'), readReceipt); }
    }
    const result = await sendWrite<AddNoteOutput>(permit, this.client, call, resolved.credentials, 'addNote', `/tickets/${input.ticketId}/messages`, payload);
    if (!isExchange(result)) return result;
    const terminal = await writeAttempt(permit, result, this.client.receipt(call, result), emptyAddNoteOutput, 'notFound');
    if (terminal) return terminal;
    let message: TicketMessage | undefined;
    if (result.outcome === 'succeeded') {
      try {
        message = decodeMessage(objectBody(result.response!.body));
        if (message.ticketId && message.ticketId !== input.ticketId) throw new TypeError('message response is for another ticket');
        if (message.externalId && message.externalId !== call.idempotencyKey) throw new TypeError('message response is another message');
        message.ticketId = input.ticketId;
      } catch { message = undefined; }
    }
    if (!message) return this.confirmAcceptedMessage(call, resolved.credentials, input.ticketId, permit, result);
    // The answer already proves acceptance, so a lost checkpoint changes nothing.
    await recordAcceptedWrite(permit, message.id);
    return newMutationBranch('added', { message }, this.client.receipt(call, result, message.id));
  }
  /** Read back a message Gorgias accepted with an unusable answer, by ID when the answer names this Step's message, otherwise once by external ID; it never resends. */
  private async confirmAcceptedMessage(call: Call, credentials: Credentials, ticketID: number, permit: WritePermit, result: Exchange): Promise<MutationAttempt<AddNoteOutput>> {
    const messageID = acceptedObjectID(result, call.idempotencyKey ?? '', ticketID);
    if (messageID) return this.confirmMessage(call, credentials, ticketID, messageID, await recordAcceptedWrite(permit, messageID), false);
    const lookup = await this.lookup(call, credentials, ticketID);
    const found = lookup.message ? newMutationBranch('added', { message: lookup.message }, lookup.receipt) : undefined;
    return acceptedWriteAttempt(false, lookup.result, found, lookup.receipt, 'addNote', 'message', emptyAddNoteOutput);
  }
  /** Read the ticket and confirm the message with messageID carries this Step's external ID. */
  private async confirmMessage(call: Call, credentials: Credentials, ticketID: number, messageID: number, isProviderIDSaved: boolean, wasCreatedByEarlierAttempt: boolean): Promise<MutationAttempt<AddNoteOutput>> {
    if (messageID < 1) return newMutationUncertain(emptyAddNoteOutput, gorgiasFailure('PROTOCOL', 'addNote', 'Gorgias accepted the message, but the checkpointed message ID is unreadable'), {} as Receipt);
    const lookup = await this.lookup(call, credentials, ticketID, messageID);
    const found = lookup.message ? newMutationBranch('added', { message: lookup.message, ...(wasCreatedByEarlierAttempt ? { wasCreatedByEarlierAttempt } : {}) }, lookup.receipt) : undefined;
    return acceptedWriteAttempt(isProviderIDSaved, lookup.result, found, lookup.receipt, 'addNote', 'message', emptyAddNoteOutput);
  }
  /** Find this Step's message among the ticket's messages by external ID and, when messageID is positive, by ID. */
  private async lookup(call: Call, credentials: Credentials, ticketID: number, messageID = 0): Promise<{ result: Exchange; receipt: Receipt; message?: TicketMessage }> {
    const result = await this.client.exchange(call, credentials, 'addNote', 'GET', `/tickets/${ticketID}`);
    const receipt = this.client.receipt(call, result, ticketID);
    if (result.outcome !== 'succeeded') return { result, receipt };
    try {
      const ticketWire = objectBody(result.response!.body);
      if (decodeTicket(ticketWire).id !== ticketID) throw new TypeError('ticket response is for another ticket');
      for (const wire of messageList(ticketWire)) {
        if (wire.external_id !== call.idempotencyKey || (messageID > 0 && wire.id !== messageID)) continue;
        try { const message = decodeMessage(wire); message.ticketId = ticketID; return { result, receipt: this.client.receipt(call, result, message.id), message }; }
        catch { /* Keep looking for a valid message with this external ID. */ }
      }
      return { result, receipt };
    } catch (error) { return { result: { outcome: 'invalid', response: result.response!, failure: gorgiasFailure('PROTOCOL', 'addNote', `Gorgias returned an invalid ticket: ${String(error)}`), retryAfterMs: 0 }, receipt }; }
  }
}
