// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { ZoomClient, zoomFailure, type ZoomExchange } from './client.js';
import { CreateMeetingDefinition, GetMeetingDefinition, ListMeetingsDefinition, ListPastMeetingParticipantsDefinition, UpdateMeetingDefinition } from './generated/definitions.js';
import type { CreateMeetingInput, GetMeetingInput, ListMeetingsInput, Meeting, MeetingPage, ListPastMeetingParticipantsInput, ParticipantPage, UpdateMeetingInput, UpdatedMeeting } from './generated/models.js';
import { createRequest, decodeMeeting, emptyMeeting, emptyMeetingPage, integer, meetingID, meetingSummary, object, pageSize, pageToken, participantPage, responseObject, updateRequest } from './meeting.js';

const emptyParticipants: ParticipantPage = { meetingId: 0, participants: [], totalRecords: 0 };
const emptyUpdate: UpdatedMeeting = { meetingId: 0 };
const successful = (status: number) => status >= 200 && status < 300;
// Only these statuses prove Zoom refused a write; other 4xx statuses leave the outcome unknown.
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Return a positive top-level meeting id from a response body, or zero. */
function createdMeetingID(body: Uint8Array): number {
  try {
    const id = object(JSON.parse(Buffer.from(body).toString('utf8'))).id;
    return Number.isSafeInteger(id) && (id as number) > 0 ? id as number : 0;
  } catch { return 0; }
}

/** One classified Zoom exchange, as the Go connector's classifyReadResponse returns it. */
export type Classified = { outcome: 'usable' | 'retry' | 'notFound' | 'rejected' | 'invalid'; failure?: Failure; errorCode: string; retryAfter: number };
function code(body: Uint8Array): string {
  try { const value = integer(responseObject(body).code); return value >= 0 && value <= 4_294_967_295 ? String(value) : ''; }
  catch { return ''; }
}
function statusMessage(message: string, status: number, errorCode: string): string { return `${message} with HTTP ${status}${errorCode ? ` (code ${errorCode})` : ''}`; }
/** Classify one exchange into Go's status outcomes, failure, numeric error code, and Retry-After. */
export function classify(client: ZoomClient, operation: string, exchange: ZoomExchange): Classified {
  if ('error' in exchange) return { outcome: 'retry', failure: zoomFailure(operation, 'TRANSPORT', 'Zoom could not be reached'), errorCode: '', retryAfter: 0 };
  const { response, isTooLarge, reflectsCredential } = exchange, status = response.status;
  const errorCode = code(response.body), retryAfter = client.retryAfter(response.headers);
  if (successful(status)) {
    if (isTooLarge) return { outcome: 'invalid', failure: zoomFailure(operation, 'RESPONSE_TOO_LARGE', 'Zoom response exceeds the configured size limit'), errorCode: '', retryAfter: 0 };
    if (reflectsCredential) return { outcome: 'invalid', failure: zoomFailure(operation, 'PROTOCOL', 'Zoom response contained the connection credential'), errorCode: '', retryAfter: 0 };
    return { outcome: 'usable', errorCode: '', retryAfter: 0 };
  }
  if (status >= 300 && status < 400) return { outcome: 'invalid', failure: zoomFailure(operation, 'PROTOCOL', statusMessage('Zoom redirected the request', status, '')), errorCode: '', retryAfter: 0 };
  if (status === 404) return { outcome: 'notFound', failure: zoomFailure(operation, 'NOT_FOUND', statusMessage('Zoom reported the resource missing', status, errorCode)), errorCode, retryAfter: 0 };
  if (status === 429) {
    let daily = false;
    try { const message = object(JSON.parse(Buffer.from(response.body).toString())).message; daily = typeof message === 'string' && message.trim().toLowerCase().startsWith('you have reached the maximum daily rate limit for this api.'); } catch { /* An invalid error body is an ordinary rate limit. */ }
    return { outcome: daily ? 'rejected' : 'retry', failure: zoomFailure(operation, 'RATE_LIMIT', statusMessage(daily ? 'Zoom daily API rate limit reached' : 'Zoom rate limited the request', status, errorCode)), errorCode, retryAfter: daily ? 0 : retryAfter };
  }
  if (status === 409) return { outcome: 'retry', failure: zoomFailure(operation, 'CONFLICT', statusMessage('Zoom reported a conflict', status, errorCode)), errorCode, retryAfter: 0 };
  if (status === 408 || status >= 500) return { outcome: 'retry', failure: zoomFailure(operation, 'AVAILABILITY', statusMessage('Zoom is temporarily unavailable', status, errorCode)), errorCode, retryAfter };
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 412 ? 'CONFLICT' : 'PROVIDER_REJECTION';
  return { outcome: 'rejected', failure: zoomFailure(operation, kind, statusMessage('Zoom rejected the request', status, errorCode)), errorCode, retryAfter: 0 };
}
function defect(operation: string, error: unknown): Failure { return zoomFailure(operation, 'VALIDATION', error instanceof Error ? error.message : String(error)); }
function queryCredential<Output>(resolved: { failure: Failure; retry: boolean }, empty: Output): QueryAttempt<Output> {
  return resolved.retry ? newQueryRetry(resolved.failure, 0) : newQueryBranch('defect', empty, zeroReceipt(), resolved.failure);
}
function mutationCredential<Output>(resolved: { failure: Failure; retry: boolean }, empty: Output): MutationAttempt<Output> {
  return resolved.retry ? newMutationRetry(resolved.failure, 0) : newMutationBranch('defect', empty, zeroReceipt(), resolved.failure);
}

/** List one page of the authorized user's meetings. */
export class ListMeetingsOperation implements Query<ListMeetingsInput, MeetingPage> {
  /** Bind one Zoom client. */ constructor(private readonly client: ZoomClient) {}
  /** Return the manifest definition. */ definition() { return ListMeetingsDefinition; }
  /** Invoke one bounded meeting-list request. */
  async invoke(call: Call, input: ListMeetingsInput): Promise<QueryAttempt<MeetingPage>> {
    let query: URLSearchParams;
    try {
      const type = input.type || 'upcoming'; if (!['upcoming', 'scheduled', 'live', 'previous_meetings'].includes(type)) throw new TypeError('type must be upcoming, scheduled, live, or previous_meetings');
      pageToken(input.pageToken); query = new URLSearchParams({ type, page_size: String(pageSize(input.pageSize)) });
      if (input.pageToken) query.set('next_page_token', input.pageToken);
    } catch (error) { return newQueryBranch('defect', emptyMeetingPage, zeroReceipt(), defect('listMeetings', error)); }
    const resolved = await this.client.resolveCredentials(call, 'listMeetings');
    if ('failure' in resolved) return queryCredential(resolved, emptyMeetingPage);
    const exchange = await this.client.exchange(call, resolved.credentials, 'GET', '/users/me/meetings', query);
    if ('error' in exchange) return exchange.error === 'notBuilt' ? newQueryBranch('defect', emptyMeetingPage, zeroReceipt(), zoomFailure('listMeetings', 'LOCAL_DEFECT', 'Zoom request could not be built')) : newQueryRetry(zoomFailure('listMeetings', 'TRANSPORT', 'Zoom could not be reached'), 0);
    const result = classify(this.client, 'listMeetings', exchange), receipt = this.client.receipt(call, '', result.errorCode);
    if (result.outcome === 'retry') return newQueryRetry(result.failure!, result.retryAfter);
    if (result.outcome === 'invalid') return newQueryBranch('invalidResponse', emptyMeetingPage, receipt, result.failure);
    if (result.outcome !== 'usable') return newQueryBranch('providerRejected', emptyMeetingPage, receipt, result.failure);
    try {
      const raw = responseObject(exchange.response.body), total = integer(raw.total_records), next = raw.next_page_token ?? '';
      if (total < 0 || typeof next !== 'string' || (raw.meetings !== undefined && raw.meetings !== null && !Array.isArray(raw.meetings))) throw new TypeError('invalid page');
      pageToken(next);
      const meetings = (raw.meetings ?? []).map((item: unknown) => meetingSummary(decodeMeeting(item)));
      return newQueryBranch('listed', { meetings, totalRecords: total, ...(next ? { nextPageToken: next } : {}) }, receipt);
    } catch { return newQueryBranch('invalidResponse', emptyMeetingPage, receipt, zoomFailure('listMeetings', 'PROTOCOL', 'Zoom returned an unusable meeting page')); }
  }
}

/** Read one meeting by numeric ID. */
export class GetMeetingOperation implements Query<GetMeetingInput, Meeting> {
  /** Bind one Zoom client. */ constructor(private readonly client: ZoomClient) {}
  /** Return the manifest definition. */ definition() { return GetMeetingDefinition; }
  /** Invoke one bounded meeting read. */
  async invoke(call: Call, input: GetMeetingInput): Promise<QueryAttempt<Meeting>> {
    try { meetingID(input.meetingId); } catch (error) { return newQueryBranch('defect', emptyMeeting, zeroReceipt(), defect('getMeeting', error)); }
    const resolved = await this.client.resolveCredentials(call, 'getMeeting');
    if ('failure' in resolved) return queryCredential(resolved, emptyMeeting);
    const id = String(input.meetingId), exchange = await this.client.exchange(call, resolved.credentials, 'GET', `/meetings/${id}`);
    if ('error' in exchange) return exchange.error === 'notBuilt' ? newQueryBranch('defect', emptyMeeting, zeroReceipt(), zoomFailure('getMeeting', 'LOCAL_DEFECT', 'Zoom request could not be built')) : newQueryRetry(zoomFailure('getMeeting', 'TRANSPORT', 'Zoom could not be reached'), 0);
    const result = classify(this.client, 'getMeeting', exchange), receipt = this.client.receipt(call, id, result.errorCode);
    if (result.outcome === 'retry') return newQueryRetry(result.failure!, result.retryAfter);
    if (result.outcome === 'notFound') return newQueryBranch('notFound', { ...emptyMeeting, id: input.meetingId }, receipt, result.failure);
    if (result.outcome === 'invalid') return newQueryBranch('invalidResponse', emptyMeeting, receipt, result.failure);
    if (result.outcome !== 'usable') return newQueryBranch('providerRejected', emptyMeeting, receipt, result.failure);
    try { const meeting = decodeMeeting(responseObject(exchange.response.body)); if (meeting.id !== input.meetingId) throw new TypeError('wrong meeting'); return newQueryBranch('found', meeting, receipt); }
    catch { return newQueryBranch('invalidResponse', emptyMeeting, receipt, zoomFailure('getMeeting', 'PROTOCOL', 'Zoom returned an unusable meeting')); }
  }
}

/** Set absolute values on one meeting. */
export class UpdateMeetingOperation implements Mutation<UpdateMeetingInput, UpdatedMeeting> {
  /** Bind one Zoom client. */ constructor(private readonly client: ZoomClient) {}
  /** Return the manifest definition. */ definition() { return UpdateMeetingDefinition; }
  /** Derive the receipt key from Dex's Call ID. */ idempotencyKey(callId: string) { return callId; }
  /** Invoke one repeat-safe meeting patch. */
  async invoke(call: Call, input: UpdateMeetingInput): Promise<MutationAttempt<UpdatedMeeting>> {
    let payload: Record<string, unknown>;
    try { payload = updateRequest(input, new Date()); } catch (error) { return newMutationBranch('defect', emptyUpdate, zeroReceipt(), defect('updateMeeting', error)); }
    const resolved = await this.client.resolveCredentials(call, 'updateMeeting');
    if ('failure' in resolved) return mutationCredential(resolved, emptyUpdate);
    const id = String(input.meetingId), updated = { meetingId: input.meetingId };
    const exchange = await this.client.exchange(call, resolved.credentials, 'PATCH', `/meetings/${id}`, undefined, payload);
    if ('error' in exchange) return exchange.error === 'notBuilt' ? newMutationBranch('defect', emptyUpdate, zeroReceipt(), zoomFailure('updateMeeting', 'LOCAL_DEFECT', 'Zoom request could not be built')) : newMutationRetry(zoomFailure('updateMeeting', 'TRANSPORT', 'Zoom could not be reached'), 0);
    if (successful(exchange.response.status)) return newMutationBranch('updated', updated, this.client.receipt(call, id));
    const result = classify(this.client, 'updateMeeting', exchange), receipt = this.client.receipt(call, id, result.errorCode);
    if (result.outcome === 'notFound') return newMutationBranch('notFound', updated, receipt, result.failure);
    const status = exchange.response.status;
    if (result.outcome === 'rejected' && (definiteRefusalStatuses.has(status) || status === 429)) return newMutationBranch('providerRejected', updated, receipt, result.failure);
    // A redirect or unlisted status leaves the outcome unknown, and repeating the absolute patch is safe.
    return newMutationRetry(result.failure!, result.retryAfter);
  }
}

/** List one page of joins from the latest ended meeting instance. */
export class ListPastMeetingParticipantsOperation implements Query<ListPastMeetingParticipantsInput, ParticipantPage> {
  /** Bind one Zoom client. */ constructor(private readonly client: ZoomClient) {}
  /** Return the manifest definition. */ definition() { return ListPastMeetingParticipantsDefinition; }
  /** Invoke one bounded participant-list request. */
  async invoke(call: Call, input: ListPastMeetingParticipantsInput): Promise<QueryAttempt<ParticipantPage>> {
    let query: URLSearchParams;
    try { meetingID(input.meetingId); pageToken(input.pageToken); query = new URLSearchParams({ page_size: String(pageSize(input.pageSize)) }); if (input.pageToken) query.set('next_page_token', input.pageToken); }
    catch (error) { return newQueryBranch('defect', emptyParticipants, zeroReceipt(), defect('listPastMeetingParticipants', error)); }
    const resolved = await this.client.resolveCredentials(call, 'listPastMeetingParticipants');
    if ('failure' in resolved) return queryCredential(resolved, emptyParticipants);
    const id = String(input.meetingId), empty = { ...emptyParticipants, meetingId: input.meetingId };
    const exchange = await this.client.exchange(call, resolved.credentials, 'GET', `/past_meetings/${id}/participants`, query);
    if ('error' in exchange) return exchange.error === 'notBuilt' ? newQueryBranch('defect', emptyParticipants, zeroReceipt(), zoomFailure('listPastMeetingParticipants', 'LOCAL_DEFECT', 'Zoom request could not be built')) : newQueryRetry(zoomFailure('listPastMeetingParticipants', 'TRANSPORT', 'Zoom could not be reached'), 0);
    const result = classify(this.client, 'listPastMeetingParticipants', exchange), receipt = this.client.receipt(call, id, result.errorCode);
    if (result.outcome === 'retry') return newQueryRetry(result.failure!, result.retryAfter);
    if (result.outcome === 'notFound') return newQueryBranch('notFound', empty, receipt, result.failure);
    if (result.outcome === 'invalid') return newQueryBranch('invalidResponse', empty, receipt, result.failure);
    if (result.outcome !== 'usable') return newQueryBranch('providerRejected', empty, receipt, result.failure);
    try { return newQueryBranch('listed', participantPage(responseObject(exchange.response.body), input.meetingId), receipt); }
    catch { return newQueryBranch('invalidResponse', empty, receipt, zoomFailure('listPastMeetingParticipants', 'PROTOCOL', 'Zoom returned an unusable participant page')); }
  }
}

/** Create one meeting behind Dex's durable single-dispatch fence. */
export class CreateMeetingOperation implements Mutation<CreateMeetingInput, Meeting> {
  /** Bind one Zoom client. */ constructor(private readonly client: ZoomClient) {}
  /** Return the manifest definition. */ definition() { return CreateMeetingDefinition; }
  /** Derive the receipt key from Dex's Call ID. */ idempotencyKey(callId: string) { return callId; }
  /**
   * Invoke one fenced create attempt. Zoom accepts no idempotency key, so an unknown outcome
   * resends while possible sends stay below the send cap and selects uncertain at the cap. A
   * saved meeting ID is read back instead of resent, and a definite refusal is never resent.
   */
  async invoke(call: Call, input: CreateMeetingInput): Promise<MutationAttempt<Meeting>> {
    let built: ReturnType<typeof createRequest>;
    try { built = createRequest(input); } catch (error) { return newMutationBranch('defect', emptyMeeting, zeroReceipt(), defect('createMeeting', error)); }
    const operationMaxSends = definitionMaxSends(CreateMeetingDefinition);
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 30_000, { maxSends: operationMaxSends }); } catch { return newMutationBranch('defect', emptyMeeting, zeroReceipt(), zoomFailure('createMeeting', 'LOCAL_DEFECT', 'Zoom create dispatch fence could not be initialized')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'the Zoom create checkpoint is unreadable; no meeting was created in this attempt'), 0);
    if (decision.kind === 'readBackRequired') {
      const providerId = decision.marker?.providerId;
      if (!providerId || decision.marker?.callId !== call.id) return newMutationUncertain(built.requested, zoomFailure('createMeeting', 'TRANSPORT', 'an earlier Zoom create may have succeeded without a confirmed meeting ID, and the send cap is reached'), this.client.receipt(call));
      return this.readBack(call, built.requested, providerId);
    }
    // A saved meeting ID proves Zoom created the meeting, so it is read back and never resent.
    if (decision.kind === 'readBackThenResendAllowed' && decision.marker?.providerId) return this.readBack(call, built.requested, decision.marker.providerId);
    const priorSendUpperBound = decision.priorSendUpperBound ?? 0;
    // This attempt's send would be the last one the cap allows when no further send remains.
    const canResend = priorSendUpperBound + 1 < effectiveMaxSends(call, operationMaxSends);
    // An earlier send may have created the meeting, so a refusal of a resend proves nothing.
    const hasPossibleEarlierSend = priorSendUpperBound > 0;
    const resendOrUncertain = (failure: Failure, retryAfter = 0, errorCode = ''): MutationAttempt<Meeting> =>
      // The claim stays the last marker, so the next attempt counts this send.
      canResend ? newMutationRetry(failure, retryAfter) : newMutationUncertain(built.requested, failure, this.client.receipt(call, '', errorCode));
    const rejectOrUncertain = async (failure: Failure, receipt: ReturnType<ZoomClient['receipt']>): Promise<MutationAttempt<Meeting>> => {
      if (hasPossibleEarlierSend) return newMutationUncertain(built.requested, failure, receipt);
      try { await fence.recordCanResend(); } catch { /* The branch is terminal, so a lost canResend checkpoint cannot cause a resend. */ }
      return newMutationBranch('providerRejected', built.requested, receipt, failure);
    };
    if (Date.parse(built.requested.startTime!) <= Date.now()) {
      // A passed start time forbids the resend, so only the earlier possible create remains.
      if (hasPossibleEarlierSend) return newMutationUncertain(built.requested, zoomFailure('createMeeting', 'VALIDATION', 'an earlier attempt may have created the Zoom meeting, and its start time has passed, so it cannot be resent'), this.client.receipt(call));
      try { await fence.recordNotSent(); } catch { return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom create not-sent checkpoint was unavailable'), 0); }
      return newMutationBranch('defect', emptyMeeting, zeroReceipt(), zoomFailure('createMeeting', 'VALIDATION', 'startTime is not in the future, and Zoom would replace it with the current time'));
    }
    const resolved = await this.client.resolveCredentials(call, 'createMeeting');
    if ('failure' in resolved) {
      // notSent would hide the earlier possible send, so nothing is recorded.
      if (hasPossibleEarlierSend) return newMutationRetry(resolved.failure, 0);
      try { await fence.recordNotSent(); } catch { return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom credentials and the not-sent checkpoint were unavailable'), 0); }
      return mutationCredential(resolved, emptyMeeting);
    }
    let exchange: ZoomExchange;
    try { exchange = await fence.dispatch(signal => this.client.exchange(call, resolved.credentials, 'POST', '/users/me/meetings', undefined, built.payload, true, signal)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom create was not sent before the attempt deadline or checkpoint'), 0);
      return resendOrUncertain(zoomFailure('createMeeting', 'TRANSPORT', 'Zoom meeting outcome is unknown'));
    }
    if ('error' in exchange) {
      if (exchange.error === 'notBuilt') {
        try { await fence.recordCanResend(); } catch { /* The request was never sent; a lost canResend checkpoint leaves a counted claim. */ }
        if (hasPossibleEarlierSend) return newMutationRetry(zoomFailure('createMeeting', 'LOCAL_DEFECT', 'Zoom request could not be built; no meeting was created in this attempt'), 0);
        return newMutationBranch('defect', emptyMeeting, zeroReceipt(), zoomFailure('createMeeting', 'LOCAL_DEFECT', 'Zoom request could not be built'));
      }
      if (exchange.error === 'beforeSend') {
        try { await fence.recordCanResend(); } catch { return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom was not reached but the resend checkpoint failed'), 0); }
        return newMutationRetry(zoomFailure('createMeeting', 'TRANSPORT', 'Zoom could not be reached, so no meeting was created'), 0);
      }
      return resendOrUncertain(zoomFailure('createMeeting', 'TRANSPORT', 'Zoom meeting outcome is unknown'));
    }
    if (successful(exchange.response.status)) {
      if (exchange.isTooLarge) return resendOrUncertain(zoomFailure('createMeeting', 'RESPONSE_TOO_LARGE', 'Zoom meeting response exceeds the configured size limit'));
      if (exchange.reflectsCredential) return resendOrUncertain(zoomFailure('createMeeting', 'PROTOCOL', 'Zoom meeting response contained the connection credential'));
      let meeting: Meeting;
      try { meeting = decodeMeeting(responseObject(exchange.response.body)); }
      catch {
        // A created meeting ID means a resend could duplicate the meeting.
        if (createdMeetingID(exchange.response.body) > 0) return newMutationUncertain(built.requested, zoomFailure('createMeeting', 'PROTOCOL', 'Zoom accepted the request but returned an unusable meeting'), this.client.receipt(call));
        return resendOrUncertain(zoomFailure('createMeeting', 'PROTOCOL', 'Zoom meeting response is malformed'));
      }
      try { await fence.recordProviderId(String(meeting.id)); } catch { /* A lost checkpoint leaves the claim, which still counts this send. */ }
      return newMutationBranch('created', meeting, this.client.receipt(call, String(meeting.id)));
    }
    const result = classify(this.client, 'createMeeting', exchange), receipt = this.client.receipt(call, '', result.errorCode), status = exchange.response.status;
    if (status === 401) {
      const refreshed = await this.client.refreshAfterRejection(call, resolved.credentials.accessToken.reveal());
      if (refreshed === 'unsupported') return rejectOrUncertain(zoomFailure('createMeeting', 'AUTHENTICATION', 'Zoom rejected the connection credential; reconnect the connection'), receipt);
      if (refreshed === 'replaced') {
        try { await fence.recordCanResend(); }
        catch { return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom rejected the credential but the resend checkpoint failed'), 0); }
        return newMutationRetry(result.failure!, 0);
      }
      return rejectOrUncertain(zoomFailure('createMeeting', refreshed === 'reauthorize' ? 'AUTHENTICATION' : 'AVAILABILITY',
        refreshed === 'reauthorize' ? 'Zoom connection requires reauthorization; reconnect the connection' : 'Zoom credential refresh is temporarily unavailable'), receipt);
    }
    // The documented daily limit refuses the create, so nothing was created by this send.
    if (status === 429 && result.outcome === 'rejected') return rejectOrUncertain(result.failure!, receipt);
    if (status === 429 || status === 503) {
      try { await fence.recordCanResend(); } catch { return newMutationRetry(zoomFailure('createMeeting', 'AVAILABILITY', 'Zoom rejected the create but the resend checkpoint failed'), 0); }
      return newMutationRetry(result.failure!, result.retryAfter);
    }
    if (definiteRefusalStatuses.has(status) && createdMeetingID(exchange.response.body) === 0) return rejectOrUncertain(result.failure!, receipt);
    // 408, 409, 5xx, redirects, and other 4xx responses do not prove whether Zoom applied the create.
    return resendOrUncertain(result.failure!, result.retryAfter, result.errorCode);
  }

  private async readBack(call: Call, requested: Meeting, providerId: string): Promise<MutationAttempt<Meeting>> {
    const id = Number(providerId);
    if (!Number.isSafeInteger(id) || id < 1) return newMutationUncertain(requested, zoomFailure('createMeeting', 'PROTOCOL', 'the accepted Zoom meeting ID could not be read back'), this.client.receipt(call, providerId));
    const resolved = await this.client.resolveCredentials(call, 'createMeeting');
    if ('failure' in resolved) return resolved.retry ? newMutationRetry(resolved.failure, 0) : newMutationUncertain(requested, resolved.failure, this.client.receipt(call, providerId));
    const exchange = await this.client.exchange(call, resolved.credentials, 'GET', `/meetings/${providerId}`);
    if ('error' in exchange) return newMutationRetry(zoomFailure('createMeeting', 'TRANSPORT', 'Zoom meeting read-back could not be reached'), 0);
    const result = classify(this.client, 'createMeeting', exchange);
    if (result.outcome === 'retry') return newMutationRetry(result.failure!, result.retryAfter);
    if (result.outcome === 'usable') {
      try { const meeting = decodeMeeting(responseObject(exchange.response.body)); if (meeting.id === id) return newMutationBranch('created', meeting, this.client.receipt(call, providerId)); }
      catch { /* An unusable read-back cannot confirm a write. */ }
    }
    return newMutationUncertain(requested, zoomFailure('createMeeting', 'PROTOCOL', 'Zoom could not confirm the accepted meeting'), this.client.receipt(call, providerId));
  }
}
