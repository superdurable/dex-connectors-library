// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { isBareEmail, type CredentialResult, type ReamazeClient, type ReamazeExchange } from './client.js';
import { CreateConversationDefinition, FindContactByEmailDefinition, GetConversationDefinition, ReplyToConversationDefinition, SearchConversationsDefinition, UpdateConversationDefinition } from './generated/definitions.js';
import type { Conversation, ConversationDetails, ConversationMessage, ConversationReply, CreateConversationInput, CreateConversationOutput, FindContactByEmailInput, FindContactByEmailOutput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput } from './generated/models.js';
import { decodeConversation, decodeMessage, encodeQuery, matchingContacts, page, parseBody, parseInstant, pathEscape } from './wire.js';

const zeroConversation: Conversation = { id: '', status: 0, tags: [], channel: { slug: '' }, createdAt: '0001-01-01T00:00:00Z' };
const zeroMessage: ConversationMessage = { body: '', visibility: 0, isInternalNote: false, origin: 0, createdAt: '0001-01-01T00:00:00Z' };
const zeroSearch: SearchConversationsOutput = { conversations: [], totalCount: 0, pageCount: 0 };
const zeroDetails: ConversationDetails = { conversation: zeroConversation, messages: [] };
const zeroCreate: CreateConversationOutput = { conversation: zeroConversation };
const zeroUpdate: UpdateConversationOutput = { conversation: zeroConversation };
const zeroFind: FindContactByEmailOutput = { contacts: [] };
const conversationIdPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,254}$/;
const channelPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;
const tagPattern = /^[A-Za-z0-9](?:[A-Za-z0-9 _./+:#&-]{0,62}[A-Za-z0-9_./+:#&-])?$/;

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'reamaze', operation, message }; }
function credentialQuery<Output>(operation: string, value: Output, credential: CredentialResult): QueryAttempt<Output> {
  return credential.kind === 'transient' ? newQueryRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded yet; nothing was sent')) :
    newQueryBranch('defect', value, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
}
function credentialMutation<Output>(operation: string, value: Output, credential: CredentialResult): MutationAttempt<Output> {
  return credential.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded yet; nothing was sent')) :
    newMutationBranch('defect', value, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
}
function exchangeQuery<Output>(client: ReamazeClient, call: Call, result: ReamazeExchange, value: Output, objectId = '', hasNotFound = false): QueryAttempt<Output> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newQueryRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : result.kind === 'notFound' && hasNotFound ? 'notFound' : 'providerRejected';
  return newQueryBranch(branch, value, client.receipt(call, objectId, result.requestId), result.failure);
}
function exchangeMutation<Output>(client: ReamazeClient, call: Call, result: ReamazeExchange, value: Output, objectId = '', hasNotFound = false): MutationAttempt<Output> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newMutationRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : result.kind === 'notFound' && hasNotFound ? 'notFound' : 'providerRejected';
  return newMutationBranch(branch, value, client.receipt(call, objectId, result.requestId), result.failure);
}
function invalidQuery<Output>(client: ReamazeClient, call: Call, operation: string, value: Output, objectId: string, message: string, requestId = ''): QueryAttempt<Output> {
  return newQueryBranch('invalidResponse', value, client.receipt(call, objectId, requestId), failure(operation, 'PROTOCOL', message));
}
function validTags(tags: string[] | undefined): boolean {
  const seen = new Set<string>();
  for (const tag of tags ?? []) { const lowered = tag.toLowerCase(); if (Buffer.byteLength(tag) > 64 || !tagPattern.test(tag) || seen.has(lowered)) return false; seen.add(lowered); }
  return true;
}
function validText(text: string): boolean { return text.trim().length > 0 && Buffer.byteLength(text) <= 16_384; }
function validStatus(status: number | undefined): boolean { return status === undefined || Number.isInteger(status) && status >= 0 && status <= 9; }
function validHoldUntil(value: string | undefined, status: number | undefined): boolean {
  if (!value) return true;
  if (status !== 5) return false;
  try { parseInstant(value); return true; } catch { return false; }
}
function path(id: string): string { return `/conversations/${pathEscape(id)}`; }
function messagesFrom(body: Uint8Array): { messages: ConversationMessage[]; pageCount: number; totalCount: number } {
  const wire = parseBody(body);
  const listing = page(wire, wire.messages);
  return { messages: listing.values.map(decodeMessage), pageCount: listing.pageCount, totalCount: listing.totalCount };
}

/** Search one bounded conversation page. */
export class SearchConversationsOperation implements Query<SearchConversationsInput, SearchConversationsOutput> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return SearchConversationsDefinition; }
  /** Validate, request, and decode one search page. */
  async invoke(call: Call, input: SearchConversationsInput): Promise<QueryAttempt<SearchConversationsOutput>> {
    const operation = 'searchConversations';
    if (input.filter && !['open', 'unassigned', 'archived', 'all'].includes(input.filter) || input.requesterEmail && !isBareEmail(input.requesterEmail) || !validTags(input.tags) || input.channel && !channelPattern.test(input.channel) || input.sort && !['updated', 'changed'].includes(input.sort) || !Number.isInteger(input.page ?? 0) || (input.page ?? 0) < 0 || (input.page ?? 0) > 10_000) return newQueryBranch('defect', zeroSearch, {}, failure(operation, 'VALIDATION', 'invalid search input'));
    try { if (input.customerMessageSince) parseInstant(input.customerMessageSince); if (input.customerMessageUntil) parseInstant(input.customerMessageUntil); }
    catch { return newQueryBranch('defect', zeroSearch, {}, failure(operation, 'VALIDATION', 'invalid search date')); }
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') return credentialQuery(operation, zeroSearch, credential);
    const requestedPage = input.page || 1;
    const query = encodeQuery({ page: String(requestedPage), ...(input.filter ? { filter: input.filter } : {}), ...(input.requesterEmail ? { for: input.requesterEmail } : {}), ...(input.tags?.length ? { tag: input.tags.join(',') } : {}), ...(input.channel ? { category: input.channel } : {}), ...(input.sort ? { sort: input.sort } : {}), ...(input.customerMessageSince ? { start_date: input.customerMessageSince } : {}), ...(input.customerMessageUntil ? { end_date: input.customerMessageUntil } : {}) });
    const result = await this.client.exchange(call, operation, credential, 'GET', '/conversations', query);
    const terminal = exchangeQuery(this.client, call, result, zeroSearch);
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      const wire = parseBody(result.response.body); const listing = page(wire, wire.conversations);
      const conversations = listing.values.map(value => decodeConversation(value, false));
      const output: SearchConversationsOutput = { conversations, totalCount: listing.totalCount, pageCount: listing.pageCount, ...(requestedPage < listing.pageCount && requestedPage < 10_000 ? { nextPage: requestedPage + 1 } : {}) };
      return newQueryBranch('searched', output, this.client.receipt(call, '', result.requestId));
    } catch { return invalidQuery(this.client, call, operation, zeroSearch, '', 'Re:amaze returned an invalid conversation page', result.kind === 'success' ? result.requestId : ''); }
  }
}

/** Read one conversation and its newest messages. */
export class GetConversationOperation implements Query<GetConversationInput, ConversationDetails> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return GetConversationDefinition; }
  /** Read the conversation and first message page. */
  async invoke(call: Call, input: GetConversationInput): Promise<QueryAttempt<ConversationDetails>> {
    const operation = 'getConversation';
    if (!conversationIdPattern.test(input.conversationId) || !Number.isInteger(input.messageLimit ?? 0) || (input.messageLimit ?? 0) < 0 || (input.messageLimit ?? 0) > 30) return newQueryBranch('defect', zeroDetails, {}, failure(operation, 'VALIDATION', 'invalid conversation read input'));
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') return credentialQuery(operation, zeroDetails, credential);
    const first = await this.client.exchange(call, operation, credential, 'GET', path(input.conversationId));
    const firstTerminal = exchangeQuery(this.client, call, first, zeroDetails, input.conversationId, true);
    if (firstTerminal) return firstTerminal;
    let conversation: Conversation;
    try { if (first.kind !== 'success') throw new Error('missing response'); conversation = decodeConversation(parseBody(first.response.body), true, input.conversationId); }
    catch { return invalidQuery(this.client, call, operation, zeroDetails, input.conversationId, 'Re:amaze returned an invalid conversation', first.requestId); }
    const second = await this.client.exchange(call, operation, credential, 'GET', path(input.conversationId) + '/messages', encodeQuery({ page: '1' }));
    const secondTerminal = exchangeQuery(this.client, call, second, zeroDetails, input.conversationId, true);
    if (secondTerminal) return secondTerminal;
    try {
      if (second.kind !== 'success') throw new Error('missing response');
      const listing = messagesFrom(second.response.body); const limit = input.messageLimit || 10;
      const messages = listing.messages.slice(0, limit);
      return newQueryBranch('found', { conversation, messages, ...(listing.messages.length > limit || listing.pageCount > 1 || listing.totalCount > messages.length ? { hasMoreMessages: true } : {}) }, this.client.receipt(call, input.conversationId, second.requestId));
    } catch { return invalidQuery(this.client, call, operation, zeroDetails, input.conversationId, 'Re:amaze returned an invalid message page', second.requestId); }
  }
}

/** Resolve exact contact matches from one candidate page. */
export class FindContactByEmailOperation implements Query<FindContactByEmailInput, FindContactByEmailOutput> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return FindContactByEmailDefinition; }
  /** Search contact candidates and filter exact email matches. */
  async invoke(call: Call, input: FindContactByEmailInput): Promise<QueryAttempt<FindContactByEmailOutput>> {
    const operation = 'findContactByEmail';
    if (!isBareEmail(input.email)) return newQueryBranch('defect', zeroFind, {}, failure(operation, 'VALIDATION', 'email must be one bare email address such as jane@example.com'));
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') return credentialQuery(operation, zeroFind, credential);
    const result = await this.client.exchange(call, operation, credential, 'GET', '/contacts', encodeQuery({ q: input.email, type: 'email', page: '1' }));
    const terminal = exchangeQuery(this.client, call, result, zeroFind);
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      const output = matchingContacts(parseBody(result.response.body), input.email);
      return newQueryBranch(output.contacts.length ? 'found' : 'notFound', output, this.client.receipt(call, '', result.requestId));
    } catch { return invalidQuery(this.client, call, operation, zeroFind, '', 'Re:amaze returned an invalid contact page', result.requestId); }
  }
}

/** Replace only changed conversation fields. */
export class UpdateConversationOperation implements Mutation<UpdateConversationInput, UpdateConversationOutput> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return UpdateConversationDefinition; }
  /** Correlate a repeat-safe mutation. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, compare, and update the conversation. */
  async invoke(call: Call, input: UpdateConversationInput): Promise<MutationAttempt<UpdateConversationOutput>> {
    const operation = 'updateConversation';
    if (!conversationIdPattern.test(input.conversationId) || !validStatus(input.status) || !validHoldUntil(input.holdUntil, input.status) || input.assigneeEmail && !isBareEmail(input.assigneeEmail) || !validTags(input.addTags) || !validTags(input.removeTags) || (input.addTags ?? []).some(tag => input.removeTags?.some(removed => removed.toLowerCase() === tag.toLowerCase())) || input.status === undefined && !input.assigneeEmail && !input.addTags?.length && !input.removeTags?.length) return newMutationBranch('defect', zeroUpdate, {}, failure(operation, 'VALIDATION', 'invalid update input'));
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') return credentialMutation(operation, zeroUpdate, credential);
    const first = await this.client.exchange(call, operation, credential, 'GET', path(input.conversationId));
    const firstTerminal = exchangeMutation(this.client, call, first, zeroUpdate, input.conversationId, true);
    if (firstTerminal) return firstTerminal;
    let current: Conversation;
    try { if (first.kind !== 'success') throw new Error('missing response'); current = decodeConversation(parseBody(first.response.body), true, input.conversationId); }
    catch { return newMutationBranch('invalidResponse', zeroUpdate, this.client.receipt(call, input.conversationId, first.requestId), failure(operation, 'PROTOCOL', 'Re:amaze returned an invalid conversation')); }
    const change: Record<string, unknown> = {};
    if (input.status !== undefined && (input.status !== current.status || input.holdUntil)) { change.status = input.status; if (input.holdUntil) change.hold_until = input.holdUntil; }
    if (input.assigneeEmail && current.assignee?.email?.toLowerCase() !== input.assigneeEmail.toLowerCase()) change.assignee = { email: input.assigneeEmail };
    const tags = current.tags ?? [];
    const wanted = tags.filter(tag => !input.removeTags?.some(removed => removed.toLowerCase() === tag.toLowerCase()));
    for (const tag of input.addTags ?? []) if (!wanted.some(existing => existing.toLowerCase() === tag.toLowerCase())) wanted.push(tag);
    if (JSON.stringify(wanted) !== JSON.stringify(tags)) change.tag_list = wanted;
    if (!Object.keys(change).length) return newMutationBranch('updated', { conversation: current, wasAlreadyApplied: true }, this.client.receipt(call, input.conversationId, first.requestId));
    const second = await this.client.exchange(call, operation, credential, 'PUT', path(input.conversationId), undefined, { conversation: change });
    const secondTerminal = exchangeMutation(this.client, call, second, { conversation: current }, input.conversationId, true);
    if (secondTerminal) return secondTerminal;
    try { if (second.kind !== 'success') throw new Error('missing response'); return newMutationBranch('updated', { conversation: decodeConversation(parseBody(second.response.body), true, input.conversationId) }, this.client.receipt(call, input.conversationId, second.requestId)); }
    catch { return newMutationBranch('invalidResponse', { conversation: current }, this.client.receipt(call, input.conversationId, second.requestId), failure(operation, 'PROTOCOL', 'Re:amaze returned an invalid updated conversation')); }
  }
}

function dispatchKey(call: Call): string { return 'dex-' + (call.idempotencyKey || call.id); }
function uncertainNotFound(operation: string): Failure { return failure(operation, 'TRANSPORT', 'an earlier attempt may have sent the request without a confirmed outcome, and Re:amaze shows no record with its key'); }
function fencedWrite<Output>(client: ReamazeClient, call: Call, result: ReamazeExchange, value: Output, objectId: string, hasNotFound: boolean, recordCanResend: () => Promise<void>): Promise<MutationAttempt<Output> | undefined> {
  if (result.kind === 'success') return Promise.resolve(undefined);
  const receipt = client.receipt(call, objectId, result.requestId);
  if (result.kind === 'rateLimited' || result.kind === 'notSent' || result.kind === 'unavailable') {
    if (result.kind === 'rateLimited' || result.kind === 'notSent' || result.status === 503) return recordCanResend().then(() => newMutationRetry(result.failure, result.retryAfterMs), () => newMutationRetry(failure(result.failure.operation, 'AVAILABILITY', 'retry checkpoint could not be recorded'), result.retryAfterMs));
    return Promise.resolve(newMutationRetry(result.failure, result.retryAfterMs));
  }
  if (result.kind === 'invalid') return Promise.resolve(newMutationUncertain(value, result.failure, receipt));
  if (result.kind === 'rejected' && result.status === 409) return Promise.resolve(newMutationRetry(result.failure));
  const branch = result.kind === 'defect' ? 'defect' : result.kind === 'notFound' && hasNotFound ? 'notFound' : 'providerRejected';
  return Promise.resolve(newMutationBranch(branch, value, receipt, result.failure));
}
function readBackTerminal<Output>(client: ReamazeClient, call: Call, operation: string, result: ReamazeExchange, value: Output, objectId: string): MutationAttempt<Output> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newMutationRetry(result.failure, result.retryAfterMs);
  return newMutationUncertain(value, failure(operation, result.failure.kind, 'an earlier attempt may have sent the request, and it could not be read back: ' + result.failure.message), client.receipt(call, objectId, result.requestId));
}
function validCreate(input: CreateConversationInput): boolean {
  return validText(input.subject) && validText(input.message) && channelPattern.test(input.channel) && isBareEmail(input.requester.email) && (!input.requester.name || input.requester.name === input.requester.name.trim() && Buffer.byteLength(input.requester.name) <= 255) && validStatus(input.status) && validHoldUntil(input.holdUntil, input.status) && (!input.assigneeEmail || isBareEmail(input.assigneeEmail)) && validTags(input.tags);
}

/** Create one conversation under a durable single-dispatch fence. */
export class CreateConversationOperation implements Mutation<CreateConversationInput, CreateConversationOutput> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return CreateConversationDefinition; }
  /** Derive the provider-visible dispatch key from the Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Fence the create, then reconcile unconfirmed dispatches by data key. */
  async invoke(call: Call, input: CreateConversationInput): Promise<MutationAttempt<CreateConversationOutput>> {
    const operation = 'createConversation';
    if (!validCreate(input)) return newMutationBranch('defect', zeroCreate, {}, failure(operation, 'VALIDATION', 'invalid create input'));
    let fence;
    try { fence = newFence(call, CreateConversationDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zeroCreate, {}, failure(operation, 'LOCAL_DEFECT', 'single dispatch fence could not be constructed')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(zeroCreate, failure(operation, 'TRANSPORT', 'dispatch checkpoint cannot be trusted'), this.client.receipt(call));
    if (decision.kind === 'readBackRequired') {
      const credential = await this.client.resolveCredentials(call);
      if (credential.kind !== 'resolved') return credential.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded to read back an earlier write')) : newMutationUncertain(zeroCreate, failure(operation, 'AUTHENTICATION', 'the earlier write cannot be read back; reconnect the connection in Dex Web'), this.client.receipt(call, decision.marker?.providerId));
      return this.reconcile(call, credential, decision.marker?.providerId);
    }
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') {
      try { await fence.recordNotSent(); } catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'credential checkpoint could not be recorded')); }
      return credential.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded yet; nothing was sent')) : newMutationBranch('defect', zeroCreate, {}, failure(operation, 'AUTHENTICATION', 'Re:amaze connection credentials are unusable; reconnect the connection in Dex Web'));
    }
    const payload = { conversation: { subject: input.subject, category: input.channel, ...(input.tags?.length ? { tag_list: input.tags } : {}), ...(input.status !== undefined ? { status: input.status } : {}), ...(input.holdUntil ? { hold_until: input.holdUntil } : {}), ...(input.assigneeEmail ? { assignee: input.assigneeEmail } : {}), data: { dex_dispatch_key: dispatchKey(call) }, message: { body: input.message, ...(input.shouldSuppressNotifications ? { suppress_notifications: true } : {}) }, user: { ...(input.requester.name ? { name: input.requester.name } : {}), email: input.requester.email } } };
    let result: ReamazeExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, operation, credential, 'POST', '/conversations', undefined, payload, signal)); }
    catch (error) { return error instanceof CheckpointError || error instanceof InsufficientTimeError ? newMutationRetry(failure(operation, 'AVAILABILITY', 'write was not sent before the attempt deadline or checkpoint')) : newMutationUncertain(zeroCreate, failure(operation, 'TRANSPORT', 'write dispatch outcome could not be confirmed'), {}); }
    if (result.kind === 'invalid' && result.status >= 200 && result.status < 300) return this.reconcile(call, credential);
    const terminal = await fencedWrite(this.client, call, result, zeroCreate, '', false, () => fence.recordCanResend());
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      const conversation = decodeConversation(parseBody(result.response.body), true);
      try { await fence.recordProviderId(conversation.id); } catch { /* The failed checkpoint cannot authorize another POST. */ }
      return newMutationBranch('created', { conversation }, this.client.receipt(call, conversation.id, result.requestId));
    } catch { return this.reconcile(call, credential); }
  }

  private async reconcile(call: Call, credential: Extract<CredentialResult, { kind: 'resolved' }>, candidateId = ''): Promise<MutationAttempt<CreateConversationOutput>> {
    const operation = 'createConversation'; const key = dispatchKey(call);
    const query = encodeQuery({ filter: 'all', 'data[dex_dispatch_key]': key });
    const result = await this.client.exchange(call, operation, credential, 'GET', candidateId ? path(candidateId) : '/conversations', candidateId ? undefined : query);
    const terminal = readBackTerminal(this.client, call, operation, result, zeroCreate, candidateId);
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      if (candidateId) {
        const wire = parseBody(result.response.body);
        if (typeof (wire.data as Record<string, unknown> | undefined)?.dex_dispatch_key !== 'string' || (wire.data as Record<string, unknown>).dex_dispatch_key !== key) return newMutationUncertain(zeroCreate, uncertainNotFound(operation), this.client.receipt(call, candidateId, result.requestId));
        const conversation = decodeConversation(wire, true, candidateId);
        return newMutationBranch('created', { conversation, wasAlreadyApplied: true }, this.client.receipt(call, conversation.id, result.requestId));
      }
      const wire = parseBody(result.response.body); const listing = page(wire, wire.conversations);
      for (const item of listing.values) {
        const candidate = item as Record<string, unknown>;
        const data = candidate.data as Record<string, unknown> | undefined;
        if (data?.dex_dispatch_key === key) { const conversation = decodeConversation(candidate, true); return newMutationBranch('created', { conversation, wasAlreadyApplied: true }, this.client.receipt(call, conversation.id, result.requestId)); }
        if (!data && typeof candidate.slug === 'string' && conversationIdPattern.test(candidate.slug)) return this.reconcile(call, credential, candidate.slug);
      }
      return newMutationUncertain(zeroCreate, uncertainNotFound(operation), this.client.receipt(call, '', result.requestId));
    } catch { return newMutationUncertain(zeroCreate, failure(operation, 'PROTOCOL', 'an earlier attempt of this Step sent the request, and the conversation read back is invalid'), this.client.receipt(call, candidateId, result.kind === 'success' ? result.requestId : '')); }
  }
}

/** Send a public reply or internal note once, reconciling by origin ID. */
export class ReplyToConversationOperation implements Mutation<ReplyToConversationInput, ConversationReply> {
  /** Bind the client. */ constructor(private readonly client: ReamazeClient) {}
  /** Return the generated definition. */ definition() { return ReplyToConversationDefinition; }
  /** Derive the origin ID from the Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Fence the reply and read back an unconfirmed message. */
  async invoke(call: Call, input: ReplyToConversationInput): Promise<MutationAttempt<ConversationReply>> {
    const operation = 'replyToConversation'; const zero: ConversationReply = { conversationId: input.conversationId, message: zeroMessage };
    if (!conversationIdPattern.test(input.conversationId) || !validText(input.text)) return newMutationBranch('defect', zero, {}, failure(operation, 'VALIDATION', 'invalid reply input'));
    let fence;
    try { fence = newFence(call, ReplyToConversationDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'single dispatch fence could not be constructed')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'dispatch checkpoint cannot be trusted'), this.client.receipt(call));
    if (decision.kind === 'readBackRequired') {
      const credential = await this.client.resolveCredentials(call);
      if (credential.kind !== 'resolved') return credential.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded to read back an earlier write')) : newMutationUncertain(zero, failure(operation, 'AUTHENTICATION', 'the earlier write cannot be read back; reconnect the connection in Dex Web'), this.client.receipt(call, input.conversationId));
      return this.reconcile(call, credential, input, zero);
    }
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind !== 'resolved') {
      try { await fence.recordNotSent(); } catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'credential checkpoint could not be recorded')); }
      return credential.kind === 'transient' ? newMutationRetry(failure(operation, 'AVAILABILITY', 'connection credentials could not be loaded yet; nothing was sent')) : newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'Re:amaze connection credentials are unusable; reconnect the connection in Dex Web'));
    }
    const payload = { message: { body: input.text, visibility: input.isInternalNote ? 1 : 0, origin_id: dispatchKey(call), ...(input.shouldSuppressNotifications ? { suppress_notifications: true } : {}), ...(input.shouldSuppressAutoResolve ? { suppress_autoresolve: true } : {}) } };
    let result: ReamazeExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, operation, credential, 'POST', path(input.conversationId) + '/messages', undefined, payload, signal)); }
    catch (error) { return error instanceof CheckpointError || error instanceof InsufficientTimeError ? newMutationRetry(failure(operation, 'AVAILABILITY', 'write was not sent before the attempt deadline or checkpoint')) : newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'write dispatch outcome could not be confirmed'), {}); }
    if (result.kind === 'invalid' && result.status >= 200 && result.status < 300) return this.reconcile(call, credential, input, zero);
    const terminal = await fencedWrite(this.client, call, result, { conversationId: '', message: zeroMessage }, input.conversationId, true, () => fence.recordCanResend());
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      const wire = parseBody(result.response.body);
      const conversation = wire.conversation as Record<string, unknown> | undefined;
      if (conversation?.slug && conversation.slug !== input.conversationId) throw new Error('wrong conversation');
      const message = decodeMessage(wire);
      return newMutationBranch('replied', { conversationId: input.conversationId, message }, this.client.receipt(call, input.conversationId, result.requestId));
    } catch { return this.reconcile(call, credential, input, zero); }
  }

  private async reconcile(call: Call, credential: Extract<CredentialResult, { kind: 'resolved' }>, input: ReplyToConversationInput, zero: ConversationReply): Promise<MutationAttempt<ConversationReply>> {
    const operation = 'replyToConversation';
    const result = await this.client.exchange(call, operation, credential, 'GET', path(input.conversationId) + '/messages', encodeQuery({ page: '1' }));
    const terminal = readBackTerminal(this.client, call, operation, result, zero, input.conversationId);
    if (terminal) return terminal;
    try {
      if (result.kind !== 'success') throw new Error('missing response');
      const { messages } = messagesFrom(result.response.body);
      const message = messages.find(candidate => candidate.originId === dispatchKey(call));
      if (message) return newMutationBranch('replied', { conversationId: input.conversationId, message, wasAlreadyApplied: true }, this.client.receipt(call, input.conversationId, result.requestId));
      return newMutationUncertain(zero, uncertainNotFound(operation), this.client.receipt(call, input.conversationId, result.requestId));
    } catch { return newMutationUncertain(zero, failure(operation, 'PROTOCOL', 'an earlier attempt of this Step sent the message, and the message page read back is invalid'), this.client.receipt(call, input.conversationId, result.kind === 'success' ? result.requestId : '')); }
  }
}
