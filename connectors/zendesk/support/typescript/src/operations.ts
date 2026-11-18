// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence, type Decision, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { CreateTicketDefinition, GetTicketDefinition, SearchTicketsDefinition, UpdateTicketDefinition } from './generated/definitions.js';
import type { CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, Ticket, TicketComment, TicketDetails, TicketUser, UpdateTicketInput, UpdateTicketOutput } from './generated/models.js';
import { zendeskFailure, isBareEmailAddress, type ZendeskClient, type ZendeskExchange, type ZendeskWriteCredentials, header } from './client.js';

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
    const resolved = await this.client.resolveWriteCredentials(call, 'searchTickets');
    if (resolved.kind !== 'resolved') return queryCredentialFailure(zeroSearch, resolved);
    const credentials = resolved.credentials;
    const parameters = { 'filter[type]': 'ticket', 'page[size]': String(input.pageSize || 25), query, ...(input.cursor ? { 'page[after]': input.cursor } : {}) };
    const result = await this.client.exchange(call, credentials, 'searchTickets', 'GET', '/search/export', parameters);
    const terminal = queryExchange(result, this.client.receipt(call, result.response), zeroSearch);
    if (terminal) return terminal;
    try {
      const body = objectJSON(result.response.body);
      if (!Array.isArray(body.results)) throw new Error('search page has no results array');
      const tickets = body.results.map(raw => { const wire = object(raw); if (wire.result_type !== undefined && wire.result_type !== '' && wire.result_type !== 'ticket') throw new Error('search result is not a ticket'); const decoded = decodeTicket(wire, this.client.agentTicketURL).ticket; delete decoded.description; delete decoded.isDescriptionTruncated; return decoded; });
      const meta = body.meta == null ? {} : object(body.meta);
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
    const resolved = await this.client.resolveWriteCredentials(call, 'getTicket');
    if (resolved.kind !== 'resolved') return queryCredentialFailure(zeroDetails, resolved);
    const credentials = resolved.credentials;
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
      const latestComments = rawComments.map(raw => decodeComment(object(raw))).sort((left, right) => compareCommentTimes(right.createdAt, left.createdAt) || right.id - left.id);
      const meta = page.meta == null ? {} : object(page.meta);
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
    const resolved = await this.client.resolveWriteCredentials(call, 'createTicket');
    if (resolved.kind !== 'resolved') {
      // defect would hide a ticket an earlier attempt may have created; createTicket declares no uncertain branch.
      if (call.context.attempt > 1 || resolved.kind === 'transient') return newMutationRetry(resolved.failure);
      return newMutationBranch('defect', zeroCreate, zeroReceipt(), resolved.failure);
    }
    const credentials = resolved.credentials;
    const ticket = { subject: input.subject, comment: { body: input.comment.body, public: !input.comment.isInternalNote },
      ...(input.requester ? { requester: { email: input.requester.email, ...(input.requester.name ? { name: input.requester.name } : {}) } } : {}),
      ...(input.priority ? { priority: input.priority } : {}), ...(input.type ? { type: input.type } : {}),
      ...(input.tags?.length ? { tags: input.tags } : {}), ...(input.groupId ? { group_id: input.groupId } : {}),
      ...(input.assigneeId ? { assignee_id: input.assigneeId } : {}), ...(input.brandId ? { brand_id: input.brandId } : {}),
      ...(input.externalId ? { external_id: input.externalId } : {}), metadata: { dex_idempotency_key: call.idempotencyKey } };
    const result = await this.client.exchange(call, credentials, 'createTicket', 'POST', '/tickets', undefined, { ticket }, call.idempotencyKey);
    const receipt = this.client.receipt(call, result.response);
    if (result.outcome !== 'success') {
      if (result.outcome === 'defect') return newMutationBranch('defect', zeroCreate, receipt, result.failure);
      if (isDefiniteRefusal(result)) return newMutationBranch('providerRejected', zeroCreate, receipt, result.failure);
      // The keyed request is safe to repeat, and its outcome is unknown.
      return newMutationRetry(result.failure, result.retryAfterMs);
    }
    try {
      const decoded = decodeTicketEnvelope(result.response.body, this.client.agentTicketURL).ticket;
      const lookup = header(result.response.headers, 'x-idempotency-lookup').toLowerCase();
      const completedReceipt = this.client.receipt(call, result.response, decoded.id);
      if (lookup) completedReceipt.metadata = { idempotencyLookup: lookup };
      return newMutationBranch('created', { ticket: decoded, ...(lookup === 'hit' ? { wasIdempotentReplay: true } : {}) }, completedReceipt);
    } catch (error) { return newMutationRetry(zendeskFailure('createTicket', 'PROTOCOL', `Zendesk returned an invalid created ticket: ${error instanceof Error ? error.message : String(error)}`)); }
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
    let fence: Fence;
    try { fence = newFence(call, 30_000, { maxSends: definitionMaxSends(UpdateTicketDefinition) }); }
    catch { return mutationDefect('updateTicket', zeroUpdate, 'single-dispatch fence could not start', 'LOCAL_DEFECT'); }
    const decision = fence.decide();
    // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
    if (decision.kind === 'failClosed') return newMutationRetry(zendeskFailure('updateTicket', 'AVAILABILITY', 'the single-dispatch checkpoint could not be read; nothing was sent in this attempt'));
    const permit = new TicketWritePermit(fence, decision);
    const resolved = await this.client.resolveWriteCredentials(call, 'updateTicket');
    if (resolved.kind !== 'resolved') return permit.credentialFailureAttempt(resolved.failure, resolved.kind === 'terminal');
    const credentials = resolved.credentials;
    // Zendesk accepted an earlier attempt's PUT, so this attempt only reads it back.
    if (permit.hasAcceptedWrite) return new AcceptedTicketWrite(this.client, call, credentials, input, true, true).readBack();
    const path = `/tickets/${input.ticketId}`;
    const read = await this.client.exchange(call, credentials, 'updateTicket', 'GET', path);
    let receipt = this.client.receipt(call, read.response, input.ticketId);
    const readTerminal = await permit.readAttempt(read, receipt, zeroUpdate);
    if (readTerminal) return readTerminal;
    let current: { ticket: Ticket; updatedStamp: string };
    try { current = decodeTicketEnvelope(read.response.body, this.client.agentTicketURL); if (current.ticket.id !== input.ticketId) throw new Error('ticket response is for another ticket'); }
    catch (error) {
      if (!permit.isFirstSend) return permit.unconfirmedRead(zeroUpdate, invalidFailure('updateTicket', 'Zendesk returned an invalid ticket', error), receipt);
      return mutationInvalid('updateTicket', zeroUpdate, receipt, 'Zendesk returned an invalid ticket', error);
    }
    const currentOutput = { ticket: current.ticket };
    if (input.comment || input.expectedUpdatedAt) {
      const audits = await this.client.exchange(call, credentials, 'updateTicket', 'GET', `${path}/audits`, { sort_order: 'desc' });
      receipt = this.client.receipt(call, audits.response, input.ticketId);
      const auditTerminal = await permit.readAttempt(audits, receipt, currentOutput);
      if (auditTerminal) return auditTerminal;
      try {
        if (hasAuditWithIdempotencyKey(audits.response.body, call.idempotencyKey)) return newMutationBranch('updated', { ticket: current.ticket, wasAlreadyApplied: true }, receipt);
      } catch (error) {
        if (!permit.isFirstSend) return permit.unconfirmedRead(currentOutput, invalidFailure('updateTicket', 'Zendesk returned an invalid audit page', error), receipt);
        return mutationInvalid('updateTicket', currentOutput, receipt, 'Zendesk returned an invalid audit page', error);
      }
    }
    if (input.expectedUpdatedAt && parseRFC3339(input.expectedUpdatedAt).getTime() !== parseRFC3339(current.ticket.updatedAt).getTime()) {
      const conflict = zendeskFailure('updateTicket', 'CONFLICT', `ticket was updated at ${current.ticket.updatedAt}, after expectedUpdatedAt`);
      // An earlier send of this Step may have moved updated_at without an audit on the latest page.
      if (!permit.isFirstSend) return permit.unconfirmedRead(currentOutput, conflict, receipt);
      return newMutationBranch('conflict', currentOutput, receipt, conflict);
    }
    const change = planTicketChange(input, current, call.idempotencyKey);
    if (Object.keys(change).length === 3) return newMutationBranch('updated', { ticket: current.ticket, wasAlreadyApplied: true }, receipt);
    if (permit.isAtSendCap) return newMutationUncertain(currentOutput, zendeskFailure('updateTicket', 'TRANSPORT', 'earlier attempts may have written the change and the send cap is reached, and the latest audit page shows no change by this Step'), receipt);
    let write: ZendeskExchange;
    try { write = await fence.dispatch(signal => this.client.exchange(call, credentials, 'updateTicket', 'PUT', path, undefined, { ticket: change }, undefined, { signal, isFencedWrite: true })); }
    // Dispatch refused before sending, so nothing reached Zendesk in this attempt.
    catch { return newMutationRetry(zendeskFailure('updateTicket', 'AVAILABILITY', 'Zendesk write was not sent before the attempt deadline or checkpoint')); }
    receipt = this.client.receipt(call, write.response, input.ticketId);
    if (write.response.status < 200 || write.response.status >= 300) return permit.writeAttempt(write, receipt, currentOutput);
    const updated = write.outcome === 'success' ? decodeTicketFor(write.response.body, this.client.agentTicketURL, input.ticketId) : undefined;
    if (updated) return newMutationBranch('updated', { ticket: updated }, receipt);
    // Zendesk accepted the PUT, so it is never resent; the saved ticket ID makes later attempts only read back.
    let isTicketIdRecorded = true;
    try { await fence.recordProviderId(String(input.ticketId)); } catch { isTicketIdRecorded = false; }
    return new AcceptedTicketWrite(this.client, call, credentials, input, isTicketIdRecorded, false).readBack();
  }
}

/** Confirms a PUT that Zendesk answered with an unusable 2xx, or that an earlier attempt saved; it never sends. */
class AcceptedTicketWrite {
  /**
   * Bind one read-back. canRetryTransientRead means the ticket ID is checkpointed, so a Retry can only read back
   * again; isFromEarlierAttempt reports a confirmed change as already applied.
   */
  constructor(private readonly client: ZendeskClient, private readonly call: Call, private readonly credentials: { email: string; apiToken: string },
    private readonly input: UpdateTicketInput, private readonly canRetryTransientRead: boolean, private readonly isFromEarlierAttempt: boolean) {}

  /** Confirm the change by this Step's audit, or by no pending field change without a comment. */
  async readBack(): Promise<MutationAttempt<UpdateTicketOutput>> {
    const path = `/tickets/${this.input.ticketId}`;
    const read = await this.client.exchange(this.call, this.credentials, 'updateTicket', 'GET', path);
    let receipt = this.client.receipt(this.call, read.response, this.input.ticketId);
    const readTerminal = this.readAttempt(read, receipt, zeroUpdate);
    if (readTerminal) return readTerminal;
    let current: { ticket: Ticket; updatedStamp: string };
    try { current = decodeTicketEnvelope(read.response.body, this.client.agentTicketURL); if (current.ticket.id !== this.input.ticketId) throw new Error('ticket response is for another ticket'); }
    catch (error) { return this.unconfirmed(zeroUpdate, 'PROTOCOL', `Zendesk returned an invalid ticket: ${errorMessage(error)}`, receipt); }
    const currentOutput = { ticket: current.ticket };
    if (this.input.comment || this.input.expectedUpdatedAt) {
      const audits = await this.client.exchange(this.call, this.credentials, 'updateTicket', 'GET', `${path}/audits`, { sort_order: 'desc' });
      receipt = this.client.receipt(this.call, audits.response, this.input.ticketId);
      const auditTerminal = this.readAttempt(audits, receipt, currentOutput);
      if (auditTerminal) return auditTerminal;
      try { if (hasAuditWithIdempotencyKey(audits.response.body, this.call.idempotencyKey)) return this.confirmed(current.ticket, receipt); }
      catch (error) { return this.unconfirmed(currentOutput, 'PROTOCOL', `Zendesk returned an invalid audit page: ${errorMessage(error)}`, receipt); }
    }
    if (!this.input.comment && Object.keys(planTicketChange(this.input, current, this.call.idempotencyKey)).length === 3) return this.confirmed(current.ticket, receipt);
    return this.unconfirmed(currentOutput, 'TRANSPORT', 'the ticket and its latest audit page show no change by this Step', receipt);
  }

  /** Retry a transient read only when that cannot resend; any other failed read is uncertain. */
  private readAttempt(result: ZendeskExchange, receipt: ReceiptDraft, value: UpdateTicketOutput): MutationAttempt<UpdateTicketOutput> | undefined {
    if (result.outcome === 'success') return undefined;
    if ((result.outcome === 'retry' || result.outcome === 'notSent') && this.canRetryTransientRead) return newMutationRetry(result.failure, result.retryAfterMs);
    return this.unconfirmed(value, result.failure.kind, result.failure.message, receipt);
  }

  private confirmed(ticket: Ticket, receipt: ReceiptDraft): MutationAttempt<UpdateTicketOutput> {
    return newMutationBranch('updated', { ticket, ...(this.isFromEarlierAttempt ? { wasAlreadyApplied: true } : {}) }, receipt);
  }

  private unconfirmed(value: UpdateTicketOutput, kind: Failure['kind'], reason: string, receipt: ReceiptDraft): MutationAttempt<UpdateTicketOutput> {
    return newMutationUncertain(value, zendeskFailure('updateTicket', kind, `Zendesk accepted an update by this Step that could not be confirmed: ${reason}`), receipt);
  }
}

/** Return the safe update with only fields that differ; three entries mean nothing to write. */
function planTicketChange(input: UpdateTicketInput, current: { ticket: Ticket; updatedStamp: string }, idempotencyKey: string | undefined): Record<string, unknown> {
  const change: Record<string, unknown> = { metadata: { dex_idempotency_key: idempotencyKey }, safe_update: true, updated_stamp: current.updatedStamp };
  if (input.status && input.status !== current.ticket.status) change.status = input.status;
  if (input.priority && input.priority !== current.ticket.priority) change.priority = input.priority;
  if (input.assigneeId && input.assigneeId !== current.ticket.assigneeId) change.assignee_id = input.assigneeId;
  if (input.groupId && input.groupId !== current.ticket.groupId) change.group_id = input.groupId;
  const tags = (current.ticket.tags ?? []).filter(tag => !input.removeTags?.includes(tag));
  for (const tag of input.addTags ?? []) if (!tags.includes(tag)) tags.push(tag);
  if (JSON.stringify(tags) !== JSON.stringify(current.ticket.tags ?? [])) change.tags = tags;
  if (input.comment) change.comment = { body: input.comment.body, public: !input.comment.isInternalNote };
  return change;
}

/** Scan one page of the newest audits for this call's metadata entry. */
function hasAuditWithIdempotencyKey(body: Uint8Array, idempotencyKey: string | undefined): boolean {
  const page = objectJSON(body);
  return array(page.audits).some(raw => { const audit = object(raw); const metadata = audit.metadata == null ? {} : object(audit.metadata); const custom = metadata.custom == null ? {} : object(metadata.custom); return custom.dex_idempotency_key === idempotencyKey; });
}

/** Return the ticket of a response for ticketId, or undefined when it is unusable. */
function decodeTicketFor(body: Uint8Array, agentTicketURL: string, ticketId: number): Ticket | undefined {
  try { const decoded = decodeTicketEnvelope(body, agentTicketURL).ticket; return decoded.id === ticketId ? decoded : undefined; }
  catch { return undefined; }
}

/** Report a refusal status whose readable body carries no ticket and no API token, proving Zendesk applied nothing. */
function isDefiniteRefusal(result: ZendeskExchange): boolean {
  return (result.outcome === 'rejected' || result.outcome === 'notFound') && definiteRefusalStatuses.has(result.response.status)
    && !result.response.hasOversizedBody && !result.response.hasTicketIdentity && !result.response.hasCredentialEcho;
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }

/** Only these statuses prove Zendesk applied nothing; any other non-2xx answer may have been applied. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Maps updateTicket reads and its fenced PUT under the fence decision's send-cap facts. */
class TicketWritePermit {
  /** No earlier attempt of this call may have sent. */ readonly isFirstSend: boolean;
  /** Earlier sends reached the cap, so this attempt only reads back. */ readonly isAtSendCap: boolean;
  /** An earlier attempt saved the ticket ID after Zendesk accepted its PUT. */ readonly hasAcceptedWrite: boolean;

  constructor(private readonly fence: Fence, decision: Decision) {
    const priorSends = decision.priorSendUpperBound ?? 0;
    this.hasAcceptedWrite = Boolean(decision.marker?.providerId);
    // sendAllowed after a canResend marker can still carry earlier possible sends.
    this.isFirstSend = decision.kind === 'sendAllowed' && priorSends === 0 && !this.hasAcceptedWrite;
    this.isAtSendCap = decision.kind === 'readBackRequired';
  }

  /** Map unusable credentials before any request of this attempt. */
  async credentialFailureAttempt(cause: Failure, isTerminal: boolean): Promise<MutationAttempt<UpdateTicketOutput>> {
    if (!this.isFirstSend) {
      // notSent would hide the earlier possible send, so nothing is recorded.
      if (isTerminal) return newMutationUncertain(zeroUpdate, zendeskFailure('updateTicket', cause.kind, 'an earlier attempt may have written the change, and the connection credentials are unusable; reconnect the connection'), zeroReceipt());
      return newMutationRetry(cause);
    }
    try { await this.fence.recordNotSent(); } catch { return newMutationRetry(zendeskFailure('updateTicket', 'AVAILABILITY', 'Dex did not record the not-sent checkpoint')); }
    return isTerminal ? newMutationBranch('defect', zeroUpdate, zeroReceipt(), cause) : newMutationRetry(cause);
  }

  /** Record notSent only on a first send; otherwise the claim stays counted. */
  async retryBeforeDispatch<T>(cause: Failure, retryAfterMs?: number): Promise<MutationAttempt<T>> {
    if (this.isFirstSend) {
      try { await this.fence.recordNotSent(); } catch { return newMutationRetry(zendeskFailure('updateTicket', 'AVAILABILITY', 'Dex did not record the not-sent checkpoint'), retryAfterMs); }
    }
    return newMutationRetry(cause, retryAfterMs);
  }

  /** Retry an inconclusive read after a possible earlier send, or select uncertain at the cap. */
  unconfirmedRead<T>(value: T, cause: Failure, receipt: ReceiptDraft): MutationAttempt<T> {
    return this.isAtSendCap ? newMutationUncertain(value, cause, receipt) : newMutationRetry(cause);
  }

  /** Map a read before the write; a definite answer after a possible send is uncertain. */
  async readAttempt<T>(result: ZendeskExchange, receipt: ReceiptDraft, value: T): Promise<MutationAttempt<T> | undefined> {
    if (result.outcome === 'success') return undefined;
    if (result.outcome === 'retry' || result.outcome === 'notSent') return this.retryBeforeDispatch(result.failure, result.retryAfterMs);
    if (!this.isFirstSend) return result.outcome === 'invalid' || result.outcome === 'defect' ? this.unconfirmedRead(value, result.failure, receipt) : newMutationUncertain(value, result.failure, receipt);
    return mutationExchange(result, receipt, value);
  }

  /** Map a PUT that Zendesk did not answer with 2xx; only a definite refusal of a first send selects a refusal branch. */
  async writeAttempt<T>(result: ZendeskExchange, receipt: ReceiptDraft, value: T): Promise<MutationAttempt<T>> {
    if (result.outcome === 'success') throw new TypeError('a 2xx write is mapped as accepted');
    const status = result.response.status;
    if (result.outcome === 'notSent' || status === 429 || status === 503) {
      // A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
      try { await this.fence.recordCanResend(); } catch { /* counted as a possible send */ }
      return newMutationRetry(result.failure, result.retryAfterMs);
    }
    // The PUT could not be built, and an earlier send may have applied.
    if (result.outcome === 'defect' && !this.isFirstSend) return newMutationUncertain(value, result.failure, receipt);
    if (result.outcome === 'defect' || isDefiniteRefusal(result)) {
      if (!this.isFirstSend) return newMutationUncertain(value, result.failure, receipt);
      try { await this.fence.recordCanResend(); } catch { return newMutationRetry(zendeskFailure('updateTicket', 'AVAILABILITY', 'Dex did not record the refusal checkpoint')); }
      return mutationExchange(result, receipt, value) ?? newMutationRetry(result.failure);
    }
    // A 3xx, 408, 409, 410, other 5xx, unlisted status, lost answer, oversized error body, or error body
    // carrying a ticket may follow an applied write.
    return retryUnknownWriteOutcome(result.failure, result.retryAfterMs ?? 0);
  }
}

/**
 * Retry an unknown write outcome, also after the last allowed send. The claim stays the last marker, so the next
 * attempt reads the audit back; it resends only below the cap and selects uncertain at the cap.
 */
function retryUnknownWriteOutcome<T>(cause: Failure, retryAfterMs: number): MutationAttempt<T> {
  return newMutationRetry(cause, retryAfterMs);
}

/** Build the exact Zendesk export search query from typed filters. */
export function buildTicketSearchQuery(input: SearchTicketsInput): string {
  const terms: string[] = [];
  const seen = new Set<string>();
  for (const status of input.statuses ?? []) { if (!statuses.has(status) || seen.has(status)) throw new Error('statuses contains an invalid or duplicate Zendesk status'); seen.add(status); terms.push(`status:${status}`); }
  if (input.requesterEmail) { if (!isBareEmailAddress(input.requesterEmail)) throw new Error('requesterEmail must be one bare email address'); terms.push(`requester:${input.requesterEmail}`); }
  validateTags('tags', input.tags);
  for (const tag of input.tags ?? []) terms.push(`tags:${tag}`);
  if (input.updatedSince) terms.push(`updated>=${parseRFC3339(input.updatedSince).toISOString().slice(0, 19)}Z`);
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
function compareCommentTimes(left: string, right: string): number {
  const milliseconds = parseRFC3339(left).getTime() - parseRFC3339(right).getTime();
  if (milliseconds) return milliseconds;
  const fractionalNanoseconds = (value: string) => Number((/\.(\d+)Z$/.exec(value)?.[1] ?? '').padEnd(9, '0').slice(3, 9));
  return fractionalNanoseconds(left) - fractionalNanoseconds(right);
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
/** Decode one Zendesk ticket object at parity with the Go connector; throws when it is invalid. */
export function decodeTicketWire(value: unknown, agentTicketURL: string): Ticket { return decodeTicket(object(value), agentTicketURL).ticket; }
function decodeTicket(wire: Record<string, unknown>, agentTicketURL: string): { ticket: Ticket; updatedStamp: string } {
  const id = optionalNumber(wire.id); const status = optionalString(wire.status); if (id < 1 || !status) throw new Error('ticket ID or status is missing');
  const createdStamp = optionalTime(wire.created_at); const updatedStamp = optionalTime(wire.updated_at);
  const createdAt = formatGoUTC(createdStamp); const updatedAt = formatGoUTC(updatedStamp);
  const description = truncateUTF8(optionalString(wire.description));
  const tags = optionalStringArray(wire.tags);
  const channel = wire.via === undefined || wire.via === null ? '' : (() => { const via = object(wire.via); return via.channel === undefined ? '' : typeof via.channel === 'string' ? via.channel : JSON.stringify(via.channel).trim(); })();
  const ticket: Ticket = { id, agentUrl: `${agentTicketURL}${id}`, status, createdAt, updatedAt,
    ...(optionalString(wire.subject) ? { subject: optionalString(wire.subject) } : {}), ...(description.value ? { description: description.value } : {}),
    ...(description.truncated ? { isDescriptionTruncated: true } : {}),
    ...(optionalNumber(wire.custom_status_id) ? { customStatusId: optionalNumber(wire.custom_status_id) } : {}),
    ...(optionalString(wire.priority) ? { priority: optionalString(wire.priority) } : {}), ...(optionalString(wire.type) ? { type: optionalString(wire.type) } : {}),
    ...(optionalNumber(wire.requester_id) ? { requesterId: optionalNumber(wire.requester_id) } : {}), ...(optionalNumber(wire.submitter_id) ? { submitterId: optionalNumber(wire.submitter_id) } : {}),
    ...(optionalNumber(wire.assignee_id) ? { assigneeId: optionalNumber(wire.assignee_id) } : {}), ...(optionalNumber(wire.group_id) ? { groupId: optionalNumber(wire.group_id) } : {}),
    ...(optionalNumber(wire.organization_id) ? { organizationId: optionalNumber(wire.organization_id) } : {}), ...(optionalNumber(wire.brand_id) ? { brandId: optionalNumber(wire.brand_id) } : {}),
    ...(tags.length ? { tags } : {}),
    ...(optionalString(wire.external_id) ? { externalId: optionalString(wire.external_id) } : {}), ...(channel ? { channel } : {}),
    ...(optionalTime(wire.due_at) ? { dueAt: formatGoUTC(optionalTime(wire.due_at)) } : {}) };
  return { ticket, updatedStamp };
}
function decodeTicketEnvelope(body: Uint8Array, agentTicketURL: string): { ticket: Ticket; updatedStamp: string } { return decodeTicket(object(objectJSON(body).ticket), agentTicketURL); }
function decodeUser(wire: Record<string, unknown>): TicketUser { return { id: optionalNumber(wire.id), ...(optionalString(wire.name) ? { name: optionalString(wire.name) } : {}), ...(optionalString(wire.email) ? { email: optionalString(wire.email) } : {}), ...(optionalString(wire.role) ? { role: optionalString(wire.role) } : {}) }; }
function decodeComment(wire: Record<string, unknown>): TicketComment { const id = optionalNumber(wire.id); if (id < 1) throw new Error('comment ID is missing'); const body = truncateUTF8(optionalString(wire.plain_body) || optionalString(wire.body)); return { id, isPublic: optionalBoolean(wire.public), body: body.value, createdAt: formatGoUTC(optionalTime(wire.created_at)), ...(optionalNumber(wire.author_id) ? { authorId: optionalNumber(wire.author_id) } : {}), ...(body.truncated ? { isBodyTruncated: true } : {}) }; }
function queryExchange<T>(result: ZendeskExchange, receipt: ReceiptDraft, zero: T): QueryAttempt<T> | undefined { if (result.outcome === 'success') return undefined; if (result.outcome === 'retry') return newQueryRetry(result.failure, result.retryAfterMs); const branch = result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected'; return newQueryBranch(branch, zero, receipt, result.failure); }
function mutationExchange<T>(result: ZendeskExchange, receipt: ReceiptDraft, zero: T): MutationAttempt<T> | undefined { if (result.outcome === 'success') return undefined; if (result.outcome === 'retry') return newMutationRetry(result.failure, result.retryAfterMs); const branch = result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected'; return newMutationBranch(branch, zero, receipt, result.failure); }
/** Select defect for a terminal credential failure and retry a transient one. */
function queryCredentialFailure<T>(zero: T, resolved: Exclude<ZendeskWriteCredentials, { kind: 'resolved' }>): QueryAttempt<T> {
  return resolved.kind === 'terminal' ? newQueryBranch('defect', zero, zeroReceipt(), resolved.failure) : newQueryRetry(resolved.failure);
}
function queryDefect<T>(operation: string, zero: T, error: unknown, kind: Failure['kind'] = 'VALIDATION'): QueryAttempt<T> { return newQueryBranch('defect', zero, zeroReceipt(), zendeskFailure(operation, kind, String(error instanceof Error ? error.message : error))); }
function mutationDefect<T>(operation: string, zero: T, error: unknown, kind: Failure['kind'] = 'VALIDATION'): MutationAttempt<T> { return newMutationBranch('defect', zero, zeroReceipt(), zendeskFailure(operation, kind, String(error instanceof Error ? error.message : error))); }
function queryInvalid<T>(operation: string, zero: T, receipt: ReceiptDraft, message: string, error: unknown): QueryAttempt<T> { return newQueryBranch('invalidResponse', zero, receipt, zendeskFailure(operation, 'PROTOCOL', `${message}: ${error instanceof Error ? error.message : String(error)}`)); }
function mutationInvalid<T>(operation: string, zero: T, receipt: ReceiptDraft, message: string, error: unknown): MutationAttempt<T> { return newMutationBranch('invalidResponse', zero, receipt, invalidFailure(operation, message, error)); }
function invalidFailure(operation: string, message: string, error: unknown): Failure { return zendeskFailure(operation, 'PROTOCOL', `${message}: ${error instanceof Error ? error.message : String(error)}`); }
