// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence, type Fence, type Marker } from '@superdurable/connectors-sdk/singledispatch';
import type { FrontClient, FrontExchange, FrontRequest } from './client.js';
import { FindContactByEmailDefinition, GetConversationDefinition, ReplyToConversationDefinition, SearchConversationsDefinition, UpdateConversationDefinition } from './generated/definitions.js';
import type { Conversation, ConversationDetails, ConversationReply, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput } from './generated/models.js';
import { comment, contact, conversation, decode, identifiers, message, page, requireID, string } from './wire.js';

const zeroConversation: Conversation = { id: '', status: '', createdAt: '0001-01-01T00:00:00Z' };
const zeroDetails = (): ConversationDetails => ({ conversation: zeroConversation, messages: [], comments: [] });
const zeroUpdate = (): UpdateConversationOutput => ({ conversation: zeroConversation });
const isRetry = (exchange: FrontExchange) => ['notSent', 'rateLimited', 'unconfirmed'].includes(exchange.outcome);
function fail(client: FrontClient, operation: string, kind: Failure['kind'], message: string): Failure { return client.failure(kind, operation, message); }
function queryFailure<T>(client: FrontClient, operation: string, branch: string, output: T, receipt: ReceiptDraft, kind: Failure['kind'], message: string): QueryAttempt<T> { return newQueryBranch(branch, output, receipt, fail(client, operation, kind, message)); }
function mutationFailure<T>(client: FrontClient, operation: string, branch: string, output: T, receipt: ReceiptDraft, kind: Failure['kind'], message: string): MutationAttempt<T> { return newMutationBranch(branch, output, receipt, fail(client, operation, kind, message)); }
function header(exchange: FrontExchange, name: string): string { const value = exchange.response?.headers[name.toLowerCase()]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function mergedID(exchange: FrontExchange): string { try { const last = new URL(header(exchange, 'location'), 'https://api2.frontapp.com').pathname.split('/').at(-1) ?? ''; return identifiers.conversation.test(last) ? last : ''; } catch { return ''; } }
async function tokenOrBranch<T>(client: FrontClient, call: Call, operation: string, output: T): Promise<{ token: string } | { attempt: QueryAttempt<T> }> { const value = await client.resolveToken(call); return 'token' in value ? value : { attempt: queryFailure(client, operation, 'defect', output, {}, 'AUTHENTICATION', value.failure.message) }; }

/** Search one bounded page of Front conversations. */
export class SearchConversationsOperation implements Query<SearchConversationsInput, SearchConversationsOutput> {
  /** Bind one client. */ constructor(private readonly client: FrontClient) {}
  /** Return the manifest definition. */ definition() { return SearchConversationsDefinition; }
  /** Validate filters, search, and classify one page. */
  async invoke(call: Call, input: SearchConversationsInput): Promise<QueryAttempt<SearchConversationsOutput>> {
    let query = '';
    const output: SearchConversationsOutput = { conversations: [], totalCount: 0, query };
    try { query = buildConversationSearchQuery(input); validatePage(input); output.query = query; }
    catch (error) { return queryFailure(this.client, 'searchConversations', 'defect', output, {}, 'VALIDATION', errorMessage(error)); }
    const selected = await tokenOrBranch(this.client, call, 'searchConversations', output);
    if ('attempt' in selected) return selected.attempt;
    const parameters = new URLSearchParams({ limit: String(input.pageSize || 25) });
    if (input.pageToken) parameters.set('page_token', input.pageToken);
    const result = await this.client.exchange(call, selected.token, 'searchConversations', { method: 'GET', path: `/conversations/search/${goQueryEscape(query)}`, query: parameters });
    const receipt = this.client.receipt(call);
    if (isRetry(result)) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.outcome !== 'succeeded') return newQueryBranch(result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected', output, result.outcome === 'defect' ? {} : receipt, result.failure);
    try { const answer = page(result.response!.body); output.conversations = answer.results.map(conversation); output.totalCount = typeof answer.total === 'number' && answer.total >= 0 ? answer.total : 0; const next = this.client.nextPageToken(answer.next, '/conversations/search/'); if (next) output.nextPageToken = next; return newQueryBranch('searched', output, receipt); }
    catch (error) { return queryFailure(this.client, 'searchConversations', 'invalidResponse', { conversations: [], totalCount: 0, query }, receipt, 'PROTOCOL', `Front returned an invalid search page: ${errorMessage(error)}`); }
  }
}

/** Read one conversation and its newest message and comment pages. */
export class GetConversationOperation implements Query<GetConversationInput, ConversationDetails> {
  /** Bind one client. */ constructor(private readonly client: FrontClient) {}
  /** Return the manifest definition. */ definition() { return GetConversationDefinition; }
  /** Read and classify the three provider resources. */
  async invoke(call: Call, input: GetConversationInput): Promise<QueryAttempt<ConversationDetails>> {
    const output = zeroDetails();
    try { requireID('conversationId', input.conversationId, identifiers.conversation); if ((input.messageLimit ?? 0) < 0 || (input.messageLimit ?? 0) > 25 || (input.commentLimit ?? 0) < 0 || (input.commentLimit ?? 0) > 25) throw new Error('messageLimit and commentLimit must be 1 to 25, or zero for 10'); }
    catch (error) { return queryFailure(this.client, 'getConversation', 'defect', output, {}, 'VALIDATION', errorMessage(error)); }
    const selected = await tokenOrBranch(this.client, call, 'getConversation', output);
    if ('attempt' in selected) return selected.attempt;
    const base = `/conversations/${input.conversationId}`;
    const requests: FrontRequest[] = [{ method: 'GET', path: base }, { method: 'GET', path: `${base}/messages`, query: new URLSearchParams({ limit: String(input.messageLimit || 10) }) }, { method: 'GET', path: `${base}/comments` }];
    const receipt = this.client.receipt(call, input.conversationId);
    for (let index = 0; index < requests.length; index++) {
      const result = await this.client.exchange(call, selected.token, 'getConversation', requests[index]!);
      if (isRetry(result)) return newQueryRetry(result.failure!, result.retryAfterMs);
      if (result.outcome !== 'succeeded') {
        if (result.outcome === 'merged') { const merged = mergedID(result); if (merged) output.mergedIntoConversationId = merged; }
        const branch = result.outcome === 'merged' ? 'merged' : result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected';
        return newQueryBranch(branch, output, branch === 'defect' ? {} : receipt, result.failure);
      }
      try {
        if (index === 0) { output.conversation = conversation(decode(result.response!.body)); if (output.conversation.id !== input.conversationId) throw new Error('Front returned another conversation'); }
        else if (index === 1) { const answer = page(result.response!.body); output.messages = answer.results.map(message); if (this.client.nextPageToken(answer.next, `${base}/messages`)) output.hasOlderMessages = true; }
        else { const answer = page(result.response!.body); output.comments = answer.results.slice(0, input.commentLimit || 10).map(comment); if (answer.results.length > (input.commentLimit || 10)) output.hasOlderComments = true; }
      } catch (error) { return queryFailure(this.client, 'getConversation', 'invalidResponse', zeroDetails(), receipt, 'PROTOCOL', `Front returned an invalid conversation: ${errorMessage(error)}`); }
    }
    return newQueryBranch('found', output, receipt);
  }
}

/** Find a Front contact by one bare email address. */
export class FindContactByEmailOperation implements Query<FindContactByEmailInput, FindContactByEmailOutput> {
  /** Bind one client. */ constructor(private readonly client: FrontClient) {}
  /** Return the manifest definition. */ definition() { return FindContactByEmailDefinition; }
  /** Read the contact alias and verify its email handle. */
  async invoke(call: Call, input: FindContactByEmailInput): Promise<QueryAttempt<FindContactByEmailOutput>> {
    const output: FindContactByEmailOutput = { contacts: [] };
    try { validateBareEmail(input.email); }
    catch (error) { return queryFailure(this.client, 'findContactByEmail', 'defect', output, {}, 'VALIDATION', errorMessage(error)); }
    const selected = await tokenOrBranch(this.client, call, 'findContactByEmail', output);
    if ('attempt' in selected) return selected.attempt;
    const result = await this.client.exchange(call, selected.token, 'findContactByEmail', { method: 'GET', path: `/contacts/alt:email:${goPathEscape(input.email)}` });
    const receipt = this.client.receipt(call);
    if (isRetry(result)) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.outcome !== 'succeeded') return newQueryBranch(result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected', output, result.outcome === 'defect' ? {} : receipt, result.failure);
    try { const found = contact(decode(result.response!.body), input.email); output.contacts = [found]; return newQueryBranch('found', output, this.client.receipt(call, found.id)); }
    catch (error) { return queryFailure(this.client, 'findContactByEmail', 'invalidResponse', output, receipt, 'PROTOCOL', `Front returned an invalid contact: ${errorMessage(error)}`); }
  }
}

/** Repeat-safe status, assignment, and tag update. */
export class UpdateConversationOperation implements Mutation<UpdateConversationInput, UpdateConversationOutput> {
  /** Bind one client. */ constructor(private readonly client: FrontClient) {}
  /** Return the manifest definition. */ definition() { return UpdateConversationDefinition; }
  /** Stable call identity; writes set absolute values. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, plan writes, and confirm the result. */
  async invoke(call: Call, input: UpdateConversationInput): Promise<MutationAttempt<UpdateConversationOutput>> {
    const zero = zeroUpdate();
    try { validateUpdate(input); } catch (error) { return mutationFailure(this.client, 'updateConversation', 'defect', zero, {}, 'VALIDATION', errorMessage(error)); }
    const selected = await this.client.resolveToken(call);
    if (!('token' in selected)) return newMutationBranch('defect', zero, {}, selected.failure);
    const path = `/conversations/${input.conversationId}`;
    const receipt = this.client.receipt(call, input.conversationId);
    // After a write, the final values are safe to repeat, so an invalid or rejected read-back retries.
    const read = async (isAfterWrite: boolean): Promise<{ conversation: Conversation } | { attempt: MutationAttempt<UpdateConversationOutput> }> => {
      const result = await this.client.exchange(call, selected.token, 'updateConversation', { method: 'GET', path });
      if (isAfterWrite && (result.outcome === 'invalid' || result.outcome === 'rejected')) return { attempt: newMutationRetry({ ...result.failure!, message: `Front accepted the change but its read-back failed: ${result.failure!.message}` }, result.retryAfterMs) };
      if (result.outcome !== 'succeeded') return { attempt: classifyUpdate( result, zero, receipt) };
      try { const value = conversation(decode(result.response!.body)); if (value.id !== input.conversationId) throw new Error('Front returned another conversation'); return { conversation: value }; }
      catch (error) {
        if (isAfterWrite) return { attempt: newMutationRetry(fail(this.client, 'updateConversation', 'PROTOCOL', `Front accepted the change but returned an invalid conversation: ${errorMessage(error)}`)) };
        return { attempt: mutationFailure(this.client, 'updateConversation', 'invalidResponse', zero, receipt, 'PROTOCOL', `Front returned an invalid conversation: ${errorMessage(error)}`) };
      }
    };
    const initial = await read(false); if ('attempt' in initial) return initial.attempt;
    const writes = planWrites(input, initial.conversation, path);
    for (let index = 0; index < writes.length; index++) {
      const result = await this.client.exchange(call, selected.token, 'updateConversation', writes[index]!);
      if (result.outcome === 'notFound') result.outcome = 'rejected';
      // Every write sets a final value, so an unknown outcome repeats it.
      if (result.outcome === 'invalid' || (result.outcome === 'rejected' && !definiteRefusalStatuses.has(result.response?.status ?? 0))) return newMutationRetry(result.failure!, result.retryAfterMs);
      if (result.outcome !== 'succeeded') return classifyUpdate( result, zero, receipt, index > 0 ? '; writes sent before it stay applied' : '');
    }
    if (!writes.length) return newMutationBranch('updated', { conversation: initial.conversation, wasAlreadyApplied: true }, receipt);
    const final = await read(true); if ('attempt' in final) return final.attempt;
    if (planWrites(input, final.conversation, path).length) return newMutationRetry(fail(this.client, 'updateConversation', 'AVAILABILITY', 'Front accepted the change but does not show it on the conversation yet'));
    return newMutationBranch('updated', { conversation: final.conversation }, receipt);
  }
}

/** Fenced mutation for one reply or internal comment. */
export class ReplyToConversationOperation implements Mutation<ReplyToConversationInput, ConversationReply> {
  /** Bind one client. */ constructor(private readonly client: FrontClient) {}
  /** Return the manifest definition. */ definition() { return ReplyToConversationDefinition; }
  /** Stable call identity, recorded in the receipt. */ idempotencyKey(callId: string): string { return callId; }
  /** Send once with a durable claim, or read back an accepted provider ID. */
  async invoke(call: Call, input: ReplyToConversationInput): Promise<MutationAttempt<ConversationReply>> {
    const output: ConversationReply = { conversationId: input.conversationId, ...(input.isInternalNote ? { isInternalNote: true } : {}), ...(input.authorId ? { authorId: input.authorId } : {}) };
    try { validateReply(input); }
    catch (error) { return mutationFailure(this.client, 'replyToConversation', 'defect', output, {}, 'VALIDATION', errorMessage(error)); }
    const operationMaxSends = definitionMaxSends(ReplyToConversationDefinition);
    let fence: Fence;
    try { fence = newFence(call, 30_000, { maxSends: operationMaxSends }); }
    catch (error) { return mutationFailure(this.client, 'replyToConversation', 'defect', output, {}, 'LOCAL_DEFECT', errorMessage(error)); }
    const decision = fence.decide();
    // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
    if (decision.kind === 'failClosed') return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'the dispatch checkpoint could not be read; nothing was sent in this attempt'));
    // Front returns an ID only for an accepted write, so a saved one is read back and never resent.
    if (decision.kind === 'readBackRequired' || (decision.kind === 'readBackThenResendAllowed' && decision.marker?.providerId)) return this.readBack(call, input, output, decision.marker);
    const priorSends = decision.priorSendUpperBound ?? 0;
    const permit: ReplySendPermit = { fence, isFirstSend: priorSends === 0, canResendUnknownOutcome: priorSends + 1 < effectiveMaxSends(call, operationMaxSends) };
    const selected = await this.client.resolveToken(call);
    if (!('token' in selected) && !permit.isFirstSend) {
      // notSent would hide the earlier possible send, so nothing is recorded.
      if (selected.terminal) return this.uncertain(call, output, 'AUTHENTICATION', 'an earlier attempt may have sent the reply or comment, and the connection needs reconnection to resend it');
      return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'Front credentials were temporarily unavailable; nothing was sent in this attempt'));
    }
    if (!('token' in selected)) {
      try { await fence.recordNotSent(); }
      catch { return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'Front credentials were temporarily unavailable and the not-sent checkpoint failed')); }
      if (selected.terminal) return mutationFailure(this.client, 'replyToConversation', 'defect', output, {}, 'AUTHENTICATION', 'Reconnect the Front connection before sending the reply or comment');
      return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'Front credentials were temporarily unavailable'));
    }
    const path = `/conversations/${input.conversationId}/${input.isInternalNote ? 'comments' : 'messages'}`;
    const payload = input.isInternalNote ? { body: input.text, ...(input.authorId ? { author_id: input.authorId } : {}) } : { body: buildReplyHTML(input.text), text: input.text, ...(input.authorId ? { author_id: input.authorId } : {}), options: { archive: Boolean(input.shouldArchive) } };
    let result: FrontExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, selected.token, 'replyToConversation', { method: 'POST', path, payload, signal })); }
    catch { return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'the reply was not dispatched; retry after the checkpoint or attempt budget recovers')); }
    return this.classifySend(call, output, result, permit, selected.token);
  }

  private async readBack(call: Call, input: ReplyToConversationInput, output: ConversationReply, marker?: Marker): Promise<MutationAttempt<ConversationReply>> {
    if (!marker || marker.callId !== call.id || !marker.providerId) return this.uncertain(call, output, marker?.state === 'claimed' ? 'TRANSPORT' : 'AVAILABILITY', 'an earlier attempt may have sent the reply or comment without a provider ID');
    if (!(input.isInternalNote ? identifiers.comment : /^[A-Za-z0-9]{1,64}$/).test(marker.providerId)) return this.uncertain(call, output, 'PROTOCOL', 'an earlier attempt stored an invalid provider ID');
    const selected = await this.client.resolveToken(call);
    if (!('token' in selected)) return selected.terminal ? newMutationUncertain(output, fail(this.client, 'replyToConversation', 'AUTHENTICATION', 'Front accepted an earlier reply or comment, but its connection needs reconnection before read-back; verify the saved provider ID before retrying manually'), this.client.receipt(call, marker.providerId)) : newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'Front credentials were temporarily unavailable during read-back'));
    const path = input.isInternalNote ? `/comments/${marker.providerId}` : `/messages/alt:uid:${marker.providerId}`;
    const result = await this.client.exchange(call, selected.token, 'replyToConversation', { method: 'GET', path });
    if (result.outcome === 'succeeded') return newMutationBranch('replied', { ...output, ...(input.isInternalNote ? { commentId: marker.providerId } : { messageUid: marker.providerId }) }, this.client.receipt(call, marker.providerId));
    if (isRetry(result) || (result.outcome === 'notFound' && !input.isInternalNote)) return newMutationRetry(result.failure!, result.retryAfterMs);
    return newMutationUncertain(output, fail(this.client, 'replyToConversation', result.failure!.kind, 'Front could not confirm an earlier accepted reply or comment'), this.client.receipt(call, marker.providerId));
  }

  /** Only a definite refusal selects a refusal branch; any other outcome resends while the send cap allows. */
  private async classifySend(call: Call, output: ConversationReply, result: FrontExchange, permit: ReplySendPermit, rejectedToken: string): Promise<MutationAttempt<ConversationReply>> {
    if (result.response && result.response.status >= 200 && result.response.status < 300) {
      let identity = '';
      try { const answer = decode(result.response.body); identity = string(output.isInternalNote ? answer.id : answer.message_uid); }
      catch { /* An answer without a usable ID counts as an unknown outcome. */ }
      if (!(output.isInternalNote ? identifiers.comment : /^[A-Za-z0-9]{1,64}$/).test(identity)) return this.resendOrUncertain(call, output, permit, fail(this.client, 'replyToConversation', result.failure?.kind ?? 'PROTOCOL', 'Front answered the reply or comment without a usable provider ID'));
      try { await permit.fence.recordProviderId(identity); } catch { /* Acceptance is already known; failure cannot authorize a resend. */ }
      return newMutationBranch('replied', { ...output, ...(output.isInternalNote ? { commentId: identity } : { messageUid: identity }) }, this.client.receipt(call, identity));
    }
    const status = result.response?.status ?? 0;
    if (result.outcome === 'defect') return permit.isFirstSend ? this.reject(call, output, permit, 'defect', result.failure!) : this.resendOrUncertain(call, output, permit, result.failure!);
    if (result.outcome === 'notSent' || result.outcome === 'rateLimited' || (result.outcome === 'unconfirmed' && status === 503)) {
      // A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
      try { await permit.fence.recordCanResend(); } catch { /* counted as a possible send */ }
      return newMutationRetry(result.failure!, result.retryAfterMs);
    }
    if (result.outcome === 'unconfirmed') return this.resendOrUncertain(call, output, permit, { ...result.failure!, message: `${result.failure!.message}; the reply or comment may exist` }, result.retryAfterMs);
    // Front documents its 301 as a merged conversation, a definite refusal of the write.
    if ((result.outcome === 'notFound' && status === 404) || result.outcome === 'merged') return this.reject(call, output, permit, 'notFound', result.failure!);
    if (result.outcome === 'rejected' && status === 401) {
      const replacement = await this.client.replacementAfterRejection(call, rejectedToken);
      if (replacement === 'replacement') {
        try { await permit.fence.recordCanResend(); }
        catch { return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'the credential resend checkpoint failed')); }
        return newMutationRetry(fail(this.client, 'replyToConversation', 'AUTHENTICATION', 'Front rejected the prior API token; retry with its replacement'));
      }
      return this.reject(call, output, permit, 'providerRejected', fail(this.client, 'replyToConversation', 'AUTHENTICATION', replacement === 'reconnect' ? 'Front rejected the API token (HTTP 401); reconnect the Front connection' : 'Front rejected the API token (HTTP 401); credential refresh is temporarily unavailable'));
    }
    if (result.outcome === 'rejected' && definiteRefusalStatuses.has(status)) return this.reject(call, output, permit, 'providerRejected', result.failure!);
    if (result.outcome === 'rejected' || result.outcome === 'notFound') return this.resendOrUncertain(call, output, permit, result.failure!);
    return this.resendOrUncertain(call, output, permit, fail(this.client, 'replyToConversation', result.failure?.kind ?? 'PROTOCOL', `${result.failure?.message ?? 'the Front answer is invalid'}; the reply or comment may exist`));
  }

  /** Retry an unknown outcome while the send cap allows another send, else select uncertain. */
  private resendOrUncertain(call: Call, output: ConversationReply, permit: ReplySendPermit, cause: Failure, retryAfterMs = 0): MutationAttempt<ConversationReply> {
    // The claim stays the last marker, so the next attempt counts this send.
    return permit.canResendUnknownOutcome ? newMutationRetry(cause, retryAfterMs) : newMutationUncertain(output, cause, this.client.receipt(call, output.conversationId));
  }

  /** Select a refusal branch only when no earlier send of this call may have applied. */
  private async reject(call: Call, output: ConversationReply, permit: ReplySendPermit, branch: string, cause: Failure): Promise<MutationAttempt<ConversationReply>> {
    if (!permit.isFirstSend) return newMutationUncertain(output, cause, this.client.receipt(call, output.conversationId));
    try { await permit.fence.recordCanResend(); }
    catch { return newMutationRetry(fail(this.client, 'replyToConversation', 'AVAILABILITY', 'the refusal checkpoint failed')); }
    return newMutationBranch(branch, output, branch === 'defect' ? {} : this.client.receipt(call, output.conversationId), cause);
  }

  private uncertain(call: Call, output: ConversationReply, kind: Failure['kind'], message: string): MutationAttempt<ConversationReply> { return newMutationUncertain(output, fail(this.client, 'replyToConversation', kind, message), this.client.receipt(call, output.conversationId)); }
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid value'; }
function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%(24|26|2B|3D|3A|40)/g, code => String.fromCharCode(parseInt(code.slice(1), 16))); }
function validateBareEmail(value: string): void { const match = /^([A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]{1,64})@([A-Za-z0-9.-]{1,253})$/.exec(value); if (!match || match[1]?.startsWith('.') || match[1]?.endsWith('.') || match[1]?.includes('..') || match[2]?.startsWith('.') || match[2]?.endsWith('.') || match[2]?.includes('..') || !match[2]?.split('.').every(label => /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(label))) throw new Error('email must be one bare email address such as jane@acme.example.com'); }
function validatePage(input: SearchConversationsInput): void { if ((input.pageSize ?? 0) < 0 || (input.pageSize ?? 0) > 100 || (input.pageToken && !identifiers.pageToken.test(input.pageToken))) throw new Error('pageSize or pageToken is invalid'); }
/** Build the ordered search filters Front expects. */
export function buildConversationSearchQuery(input: SearchConversationsInput): string {
  const terms: string[] = [];
  const text = input.text?.trim() ?? '';
  if (text) { if (Buffer.byteLength(text) > 512 || /\p{Cc}/u.test(text)) throw new Error('text is too long or contains a control character'); terms.push(text); }
  if (input.inboxId) { requireID('inboxId', input.inboxId, identifiers.inbox); terms.push(`inbox:${input.inboxId}`); }
  if (input.tagId) { requireID('tagId', input.tagId, identifiers.tag); terms.push(`tag:${input.tagId}`); }
  const seen = new Set<string>();
  for (const status of input.statuses ?? []) { if (!['open','archived','snoozed','trashed','assigned','unassigned','unreplied','waiting','resolved'].includes(status)) throw new Error('statuses must be Front is: values'); if (!seen.has(status)) terms.push(`is:${status}`); seen.add(status); }
  if (input.recipientEmail) { validateBareEmail(input.recipientEmail); terms.push(`recipient:${input.recipientEmail}`); }
  if (input.assigneeId) { requireID('assigneeId', input.assigneeId, identifiers.teammate); terms.push(`assignee:${input.assigneeId}`); }
  if (input.activityAfter) { const seconds = Math.floor(Date.parse(input.activityAfter) / 1000); if (!Number.isFinite(seconds) || seconds < 1) throw new Error('activityAfter must be after 1970'); terms.push(`after:${seconds}`); }
  if (!terms.length) throw new Error('set text or at least one filter; Front has no empty search');
  return terms.join(' ');
}
/** A fence that allowed this attempt's send, with its send-cap facts. */
interface ReplySendPermit {
  /** The attempt's fence. */ fence: Fence;
  /** No earlier attempt of this call may have sent. */ isFirstSend: boolean;
  /** This attempt's send leaves room under the cap for one more. */ canResendUnknownOutcome: boolean;
}

/** Only these statuses prove Front applied nothing; any other non-2xx answer may have been applied. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

function classifyUpdate( result: FrontExchange, output: UpdateConversationOutput, receipt: ReceiptDraft, note = ''): MutationAttempt<UpdateConversationOutput> {
  if (isRetry(result)) return newMutationRetry(result.failure!, result.retryAfterMs);
  const branch = result.outcome === 'merged' || result.outcome === 'notFound' ? 'notFound' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, output, branch === 'defect' ? {} : receipt, result.failure ? { ...result.failure, message: result.failure.message + note } : undefined);
}
function validateUpdate(input: UpdateConversationInput): void {
  requireID('conversationId', input.conversationId, identifiers.conversation);
  if (input.status && !['open','archived','deleted'].includes(input.status)) throw new Error('status must be open, archived, or deleted');
  if (input.status && input.statusId) throw new Error('set status or statusId, not both');
  if (input.assigneeId && input.isUnassigned) throw new Error('set assigneeId or isUnassigned, not both');
  if ((input.addTagIds?.length ?? 0) > 25 || (input.removeTagIds?.length ?? 0) > 25) throw new Error('tag changes exceed 25');
  if (!input.status && !input.statusId && !input.assigneeId && !input.isUnassigned && !input.addTagIds?.length && !input.removeTagIds?.length) throw new Error('set at least one change');
  if (input.statusId) requireID('statusId', input.statusId, identifiers.status);
  if (input.assigneeId) requireID('assigneeId', input.assigneeId, identifiers.teammate);
  for (const id of [...input.addTagIds ?? [], ...input.removeTagIds ?? []]) requireID('tagId', id, identifiers.tag);
  if (input.addTagIds?.some(id => input.removeTagIds?.includes(id))) throw new Error('tag is both added and removed');
}
function planWrites(input: UpdateConversationInput, current: Conversation, path: string): FrontRequest[] {
  const patch: Record<string, unknown> = {};
  if (input.status && !(input.status === 'open' ? ['assigned','unassigned'].includes(current.status) : current.status === input.status)) patch.status = input.status;
  if (input.statusId && current.statusId !== input.statusId) patch.status_id = input.statusId;
  if (input.assigneeId && current.assignee?.id !== input.assigneeId) patch.assignee_id = input.assigneeId;
  if (input.isUnassigned && current.assignee?.id) patch.assignee_id = null;
  const existing = new Set(current.tags?.map(tag => tag.id) ?? []);
  const add = [...new Set(input.addTagIds ?? [])].filter(id => !existing.has(id));
  const remove = [...new Set(input.removeTagIds ?? [])].filter(id => existing.has(id));
  return [...(Object.keys(patch).length ? [{ method: 'PATCH', path, payload: patch }] : []), ...(add.length ? [{ method: 'POST', path: `${path}/tags`, payload: { tag_ids: add } }] : []), ...(remove.length ? [{ method: 'DELETE', path: `${path}/tags`, payload: { tag_ids: remove } }] : [])];
}
function validateReply(input: ReplyToConversationInput): void { requireID('conversationId', input.conversationId, identifiers.conversation); if (!input.text.trim() || Buffer.byteLength(input.text) > 65_536) throw new Error('text is required and must fit 65536 bytes'); if (input.isInternalNote && input.shouldArchive) throw new Error('shouldArchive applies only to a reply'); if (input.authorId) requireID('authorId', input.authorId, identifiers.teammate); }
/** Turn plain text into escaped Front reply paragraphs. */
export function buildReplyHTML(value: string): string { return value.replace(/\r\n?/g, '\n').split('\n\n').map(block => block.replace(/^\n+|\n+$/g, '')).filter(block => block.trim()).map(block => `<p>${block.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&#34;').replace(/'/g, '&#39;').replace(/\n/g, '<br>')}</p>`).join(''); }
