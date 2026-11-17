// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import type { HiverClient, HiverExchange, HiverRequest } from './client.js';
import { AddNoteDefinition, CreateSharedDraftDefinition, GetConversationDefinition, ListConversationsDefinition, ListInboxesDefinition, UpdateConversationDefinition } from './generated/definitions.js';
import type { AddNoteInput, AddNoteOutput, ConversationDetails, CreateSharedDraftInput, GetConversationInput, ListConversationsInput, ListConversationsOutput, ListInboxesInput, ListInboxesOutput, SharedDraft, Tag, UpdateConversationInput, UpdateConversationOutput } from './generated/models.js';
import { conversationPath, decodeConversation, decodeInbox, decodeListPage, decodeNote, decodeSharedDraft, decodeSingleData, decodeTag, inboxPath, object, optionalString, requireHiverID, wireID } from './decode.js';

const emptyInboxPage: ListInboxesOutput = { inboxes: [] };
const emptyConversationPage: ListConversationsOutput = { conversations: [] };
const emptyDetails: ConversationDetails = { conversation: { id: '', inboxId: '', status: '', tagIds: [] }, messages: [] };
const emptyUpdate: UpdateConversationOutput = { conversation: emptyDetails.conversation };
const emptyNote: AddNoteOutput = { note: { id: '', createdAt: '0001-01-01T00:00:00Z' } };
const emptyDraft: SharedDraft = { id: '' };

/** Read one page of shared inboxes. */
export class ListInboxesOperation implements Query<ListInboxesInput, ListInboxesOutput> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return ListInboxesDefinition; }
  /** Invoke one provider read. */
  async invoke(call: Call, input: ListInboxesInput): Promise<QueryAttempt<ListInboxesOutput>> {
    try { validatePage(input.pageToken, input.pageSize); } catch (error) { return queryDefect('listInboxes', emptyInboxPage, error); }
    const key = await this.client.resolveKey(call);
    if (!key) return queryCredentialFailure('listInboxes', emptyInboxPage);
    const result = await this.client.exchange(call, 'listInboxes', key, { method: 'GET', path: '/inboxes', query: pageQuery(input.pageToken, input.pageSize) });
    const terminal = queryOutcome(this.client, call, result, emptyInboxPage, '', 'providerRejected');
    if (terminal) return terminal;
    if (result.kind !== 'success') throw new Error('unclassified Hiver exchange');
    try {
      const page = decodeListPage(result.response.body);
      const inboxes = page.results.map(decodeInbox);
      return newQueryBranch('listed', { inboxes, ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}) }, this.client.receipt(call, result.response));
    } catch (error) { return queryInvalid(this.client, call, result, 'listInboxes', emptyInboxPage, `Hiver returned an invalid inbox page: ${message(error)}`); }
  }
}

/** Read one page of an inbox's conversations. */
export class ListConversationsOperation implements Query<ListConversationsInput, ListConversationsOutput> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return ListConversationsDefinition; }
  /** Invoke one provider read. */
  async invoke(call: Call, input: ListConversationsInput): Promise<QueryAttempt<ListConversationsOutput>> {
    try { requireHiverID('inboxId', input.inboxId); validatePage(input.pageToken, input.pageSize); } catch (error) { return queryDefect('listConversations', emptyConversationPage, error); }
    const key = await this.client.resolveKey(call);
    if (!key) return queryCredentialFailure('listConversations', emptyConversationPage);
    const result = await this.client.exchange(call, 'listConversations', key, { method: 'GET', path: `${inboxPath(input.inboxId)}/conversations`, query: pageQuery(input.pageToken, input.pageSize) });
    const terminal = queryOutcome(this.client, call, result, emptyConversationPage, input.inboxId, 'notFound');
    if (terminal) return terminal;
    if (result.kind !== 'success') throw new Error('unclassified Hiver exchange');
    try {
      const page = decodeListPage(result.response.body);
      const conversations = page.results.map(raw => decodeConversation(raw, input.inboxId).conversation);
      return newQueryBranch('listed', { conversations, ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}) }, this.client.receipt(call, result.response, input.inboxId));
    } catch (error) { return queryInvalid(this.client, call, result, 'listConversations', emptyConversationPage, `Hiver returned an invalid conversation page: ${message(error)}`, input.inboxId); }
  }
}

/** Read one conversation and its message IDs. */
export class GetConversationOperation implements Query<GetConversationInput, ConversationDetails> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return GetConversationDefinition; }
  /** Invoke one provider read. */
  async invoke(call: Call, input: GetConversationInput): Promise<QueryAttempt<ConversationDetails>> {
    try { requireHiverID('inboxId', input.inboxId); requireHiverID('conversationId', input.conversationId); } catch (error) { return queryDefect('getConversation', emptyDetails, error); }
    const key = await this.client.resolveKey(call);
    if (!key) return queryCredentialFailure('getConversation', emptyDetails);
    const result = await this.client.exchange(call, 'getConversation', key, { method: 'GET', path: conversationPath(input.inboxId, input.conversationId) });
    const terminal = queryOutcome(this.client, call, result, emptyDetails, input.conversationId, 'notFound');
    if (terminal) return terminal;
    if (result.kind !== 'success') throw new Error('unclassified Hiver exchange');
    try {
      const details = decodeConversation(decodeSingleData(result.response.body), input.inboxId);
      return newQueryBranch('found', details, this.client.receipt(call, result.response, details.conversation.id));
    } catch (error) { return queryInvalid(this.client, call, result, 'getConversation', emptyDetails, `Hiver returned an invalid conversation: ${message(error)}`, input.conversationId); }
  }
}

/** Set absolute conversation fields and verify them by read-back. */
export class UpdateConversationOperation implements Mutation<UpdateConversationInput, UpdateConversationOutput> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return UpdateConversationDefinition; }
  /** Correlate the absolute write to the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Invoke one repeat-safe change. */
  async invoke(call: Call, input: UpdateConversationInput): Promise<MutationAttempt<UpdateConversationOutput>> {
    try { validateUpdate(input); } catch (error) { return mutationDefect('updateConversation', emptyUpdate, error); }
    const key = await this.client.resolveKey(call);
    if (!key) return mutationCredentialFailure('updateConversation', emptyUpdate);
    const output: UpdateConversationOutput = { conversation: emptyDetails.conversation };
    if (input.assigneeEmail) {
      const result = await this.client.exchange(call, 'updateConversation', key, { method: 'GET', path: `${inboxPath(input.inboxId)}/users/search`, query: new URLSearchParams({ email: input.assigneeEmail }) });
      const terminal = updateOutcome(this.client, call, result, input.conversationId);
      if (terminal) return terminal;
    if (result.kind !== 'success') throw new Error('unclassified Hiver exchange');
      try {
        const users = decodeListPage(result.response.body).results;
        const matches = [...new Set(users.map(raw => {
          const user = object(raw); const id = wireID(user.id); requireHiverID('user id', id);
          return optionalString(user.email).trim().toLowerCase() === input.assigneeEmail!.toLowerCase() ? id : '';
        }).filter(Boolean))];
        if (matches.length !== 1) return newMutationBranch('providerRejected', emptyUpdate, this.client.receipt(call, result.response, input.conversationId), failure('updateConversation', 'VALIDATION', `assigneeEmail matches ${matches.length} users of the shared inbox instead of one; the change was not sent`));
        output.assigneeUserId = matches[0]!;
      } catch (error) { return mutationInvalid(this.client, call, result, 'updateConversation', emptyUpdate, `Hiver returned an invalid user search page: ${message(error)}`, input.conversationId); }
    }
    if (input.applyTagNames?.length || input.removeTagNames?.length) {
      const tags: Tag[] = [];
      let token = '';
      let isComplete = false;
      let lastResult: HiverExchange | undefined;
      for (let pageIndex = 0; pageIndex < 5; pageIndex++) {
        const result = await this.client.exchange(call, 'updateConversation', key, { method: 'GET', path: `${inboxPath(input.inboxId)}/tags`, query: pageQuery(token, 100) });
        const terminal = updateOutcome(this.client, call, result, input.conversationId);
        if (terminal) return terminal;
    if (result.kind !== 'success') throw new Error('unclassified Hiver exchange');
        lastResult = result;
        try { const page = decodeListPage(result.response.body); tags.push(...page.results.map(decodeTag)); token = page.nextPageToken; isComplete = !token; }
        catch (error) { return mutationInvalid(this.client, call, result, 'updateConversation', emptyUpdate, `Hiver returned an invalid tag page: ${message(error)}`, input.conversationId); }
        if (isComplete || [...(input.applyTagNames ?? []), ...(input.removeTagNames ?? [])].every(name => tags.some(tag => tag.name === name))) break;
      }
      const missing = isComplete ? 'is not a tag of the shared inbox' : 'is not among the tags on the first 5 pages Hiver returned';
      try {
        output.appliedTags = matchTags('applyTagNames', input.applyTagNames ?? [], tags, missing);
        output.removedTags = matchTags('removeTagNames', input.removeTagNames ?? [], tags, missing);
      } catch (error) { return newMutationBranch('providerRejected', emptyUpdate, this.client.receipt(call, lastResult?.response, input.conversationId), failure('updateConversation', 'VALIDATION', `${message(error)}; the change was not sent`)); }
    }
    const change: Record<string, unknown> = {};
    if (input.status) change.status = { name: input.status === 'closed' ? 'close' : input.status };
    if (input.assigneeEmail) change.assignee = { email: input.assigneeEmail };
    if (output.appliedTags?.length || output.removedTags?.length) change.tags = { ...(output.appliedTags?.length ? { to_apply: output.appliedTags.map(tag => tag.id) } : {}), ...(output.removedTags?.length ? { to_remove: output.removedTags.map(tag => tag.id) } : {}) };
    const write = await this.client.exchange(call, 'updateConversation', key, { method: 'PATCH', path: conversationPath(input.inboxId, input.conversationId), json: change });
    // The change is absolute, so an unknown outcome repeats it.
    if (write.kind === 'invalid' || (write.kind === 'rejected' && !definiteRefusalStatuses.has(write.status ?? 0))) return newMutationRetry(write.failure, write.retryAfterMs);
    const writeTerminal = updateOutcome(this.client, call, write, input.conversationId);
    if (writeTerminal) return writeTerminal;
    if (write.kind !== 'success') throw new Error('unclassified Hiver exchange');
    const read = await this.client.exchange(call, 'updateConversation', key, { method: 'GET', path: conversationPath(input.inboxId, input.conversationId) });
    // Hiver accepted the absolute change, so an unconfirmed read-back repeats it.
    if (read.kind !== 'success') return newMutationRetry({ ...read.failure, message: `Hiver accepted the change but the read-back failed: ${read.failure.message}` }, read.retryAfterMs);
    let conversation: UpdateConversationOutput['conversation'];
    try { conversation = decodeConversation(decodeSingleData(read.response.body), input.inboxId).conversation; }
    catch (error) { return newMutationRetry(failure('updateConversation', 'PROTOCOL', `Hiver accepted the change but returned an invalid conversation: ${message(error)}`)); }
    output.conversation = conversation;
    const missing = unappliedChange(input, output);
    if (missing) return newMutationRetry(failure('updateConversation', 'CONFLICT', `Hiver accepted the change but the conversation read back does not show its ${missing} yet`));
    return newMutationBranch('updated', output, this.client.receipt(call, read.response, output.conversation.id));
  }
}

/** Add one note under a durable single-dispatch fence. */
export class AddNoteOperation implements Mutation<AddNoteInput, AddNoteOutput> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return AddNoteDefinition; }
  /** Correlate the fenced write to the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Invoke one fenced note creation. */
  async invoke(call: Call, input: AddNoteInput): Promise<MutationAttempt<AddNoteOutput>> {
    return invokeFenced(call, this.client, AddNoteDefinition, emptyNote, () => { requireHiverID('inboxId', input.inboxId); requireHiverID('conversationId', input.conversationId); validateText('content', input.content, 65_536); },
      { method: 'POST', path: `${conversationPath(input.inboxId, input.conversationId)}/notes`, form: [['content', input.content]] },
      body => ({ note: decodeNote(body) }), output => output.note.id, 'id', 'added', 'Hiver accepted the note but returned an invalid note');
  }
}

/** Create one shared draft under a durable single-dispatch fence. */
export class CreateSharedDraftOperation implements Mutation<CreateSharedDraftInput, SharedDraft> {
  /** Bind one Hiver client. */ constructor(private readonly client: HiverClient) {}
  /** Return the stable definition. */ definition() { return CreateSharedDraftDefinition; }
  /** Correlate the fenced write to the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Invoke one fenced draft creation. */
  async invoke(call: Call, input: CreateSharedDraftInput): Promise<MutationAttempt<SharedDraft>> {
    return invokeFenced(call, this.client, CreateSharedDraftDefinition, emptyDraft, () => {
      requireHiverID('inboxId', input.inboxId);
      if (Boolean(input.hiverMessageId) === Boolean(input.gmailMessageId)) throw new Error('exactly one of hiverMessageId and gmailMessageId is required');
      if (input.hiverMessageId) requireHiverID('hiverMessageId', input.hiverMessageId);
      if (input.gmailMessageId && !/^[A-Za-z0-9]{1,64}$/.test(input.gmailMessageId) && !/^<[\x21-\x3B\x3D\x3F-\x7E]{1,994}>$/.test(input.gmailMessageId)) throw new Error('gmailMessageId must be a Gmail message ID or an SMTP Message-ID such as <abc123@mail.gmail.com>');
      validateText('body', input.body, 65_536);
    }, { method: 'POST', path: `${inboxPath(input.inboxId)}/conversations/shared-drafts`, form: [['body', input.body], input.hiverMessageId ? ['hiver_message_id', input.hiverMessageId] : ['gmail_message_id', input.gmailMessageId!]] },
    decodeSharedDraft, output => output.id, 'shared_draft_id', 'created', 'Hiver accepted the draft but returned an invalid shared draft');
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'hiver', operation, message }; }
function message(error: unknown): string { return error instanceof Error ? error.message : 'provider response is invalid'; }
function queryDefect<T>(operation: string, zero: T, error: unknown): QueryAttempt<T> { return newQueryBranch('defect', zero, {}, failure(operation, 'VALIDATION', message(error))); }
function mutationDefect<T>(operation: string, zero: T, error: unknown): MutationAttempt<T> { return newMutationBranch('defect', zero, {}, failure(operation, 'VALIDATION', message(error))); }
function queryCredentialFailure<T>(operation: string, zero: T): QueryAttempt<T> { return newQueryBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function mutationCredentialFailure<T>(operation: string, zero: T): MutationAttempt<T> { return newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
function queryInvalid<T>(client: HiverClient, call: Call, result: HiverExchange & { kind: 'success' }, operation: string, zero: T, detail: string, objectId = ''): QueryAttempt<T> { return newQueryBranch('invalidResponse', zero, client.receipt(call, result.response, objectId), failure(operation, 'PROTOCOL', detail)); }
function mutationInvalid<T>(client: HiverClient, call: Call, result: HiverExchange & { kind: 'success' }, operation: string, zero: T, detail: string, objectId = ''): MutationAttempt<T> { return newMutationBranch('invalidResponse', zero, client.receipt(call, result.response, objectId), failure(operation, 'PROTOCOL', detail)); }
function queryOutcome<T>(client: HiverClient, call: Call, result: HiverExchange, zero: T, objectId: string, notFoundBranch: string): QueryAttempt<T> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newQueryRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'notFound' ? notFoundBranch : result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, client.receipt(call, result.response, objectId), result.failure);
}
function updateOutcome(client: HiverClient, call: Call, result: HiverExchange, objectId: string): MutationAttempt<UpdateConversationOutput> | undefined {
  if (result.kind === 'success') return undefined;
  if (['rateLimited', 'notSent', 'unavailable'].includes(result.kind)) return newMutationRetry(result.failure, result.retryAfterMs);
  const branch = result.kind === 'notFound' ? 'notFound' : result.kind === 'invalid' ? 'invalidResponse' : result.kind === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, emptyUpdate, client.receipt(call, result.response, objectId), result.failure);
}
function validatePage(pageToken?: string, pageSize?: number): void {
  if (pageSize !== undefined && pageSize !== 0 && (!Number.isInteger(pageSize) || pageSize < 10 || pageSize > 100)) throw new Error('pageSize must be zero or from 10 to 100');
  if (pageToken && (Buffer.byteLength(pageToken) > 4096 || !/^[A-Za-z0-9+/=_.-]+$/.test(pageToken))) throw new Error('pageToken must be a next-page token exactly as Hiver returned it');
}
function pageQuery(token?: string, size?: number): URLSearchParams { const query = new URLSearchParams({ limit: String(size || 50) }); if (token) query.set('next_page', token); return query; }
function validateText(field: string, value: string, maxBytes: number): void {
  if (typeof value !== 'string' || /^[\t-\r \u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]*$/.test(value)) throw new Error(`${field} is required`);
  if (Buffer.byteLength(value) > maxBytes) throw new Error(`${field} exceeds ${maxBytes} bytes`);
  if (value.includes('\0')) throw new Error(`${field} must not contain NUL characters`);
}
function validateUpdate(input: UpdateConversationInput): void {
  requireHiverID('inboxId', input.inboxId); requireHiverID('conversationId', input.conversationId);
  if (input.status && !['open', 'pending', 'closed'].includes(input.status)) throw new Error('status must be open, pending, or closed');
  if (input.assigneeEmail && !isBareEmail(input.assigneeEmail)) throw new Error('assigneeEmail must be one bare email address');
  for (const [field, names] of [['applyTagNames', input.applyTagNames], ['removeTagNames', input.removeTagNames]] as const) {
    if ((names?.length ?? 0) > 20) throw new Error(`${field} holds more than 20 names`);
    names?.forEach((name, index) => { if (!name || name.trim() !== name || Buffer.byteLength(name) > 100 || /[\0\r\n]/.test(name)) throw new Error(`${field}[${index}] must be a tag name of 1 to 100 bytes without surrounding spaces or line breaks`); });
  }
  input.applyTagNames?.forEach((name, index) => { if (input.removeTagNames?.some(candidate => candidate.toLowerCase() === name.toLowerCase())) throw new Error(`applyTagNames[${index}] is also in removeTagNames`); });
  if (!input.status && !input.assigneeEmail && !input.applyTagNames?.length && !input.removeTagNames?.length) throw new Error('at least one of status, assigneeEmail, applyTagNames, or removeTagNames is required');
}
function isBareEmail(value: string): boolean {
  if (typeof value !== 'string' || value.trim() !== value) return false;
  const separator = value.lastIndexOf('@');
  if (separator < 1 || separator === value.length - 1) return false;
  const local = value.slice(0, separator);
  const domain = value.slice(separator + 1);
  const atom = /^(?:[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~\u0080-\uFFFF]+)(?:\.[A-Za-z0-9!#$%&'*+\-/=?^_`{|}~\u0080-\uFFFF]+)*$/u;
  const quoted = /^"(?:[\x20-\x21\x23-\x5B\x5D-\x7E\u0080-\uFFFF]|\\[\x20-\x7E])*"$/u;
  if (!atom.test(local) && !quoted.test(local)) return false;
  return /^(?:[A-Za-z0-9_\-\u0080-\uFFFF]+)(?:\.[A-Za-z0-9_\-\u0080-\uFFFF]+)*$/u.test(domain) || /^\[[\x21-\x5A\x5E-\x7E]+\]$/.test(domain);
}
function matchTags(field: string, names: string[], tags: Tag[], missing: string): Tag[] {
  const matched: Tag[] = [];
  names.forEach((name, index) => {
    const exact = tags.filter(tag => tag.name === name);
    const candidates = exact.length ? exact : tags.filter(tag => tag.name.toLowerCase() === name.toLowerCase());
    const unique = [...new Map(candidates.map(tag => [tag.id, tag])).values()];
    if (unique.length !== 1) throw new Error(`${field}[${index}] ${unique.length ? 'matches several tags of the shared inbox' : missing}`);
    if (!matched.some(tag => tag.id === unique[0]!.id)) matched.push(unique[0]!);
  });
  return matched;
}
function unappliedChange(input: UpdateConversationInput, output: UpdateConversationOutput): string {
  if (input.status && output.conversation.status !== input.status) return 'status';
  if (output.assigneeUserId && output.conversation.assignee?.id !== output.assigneeUserId) return 'assignee';
  if (output.appliedTags?.some(tag => !output.conversation.tagIds?.includes(tag.id))) return 'applied tags';
  if (output.removedTags?.some(tag => output.conversation.tagIds?.includes(tag.id))) return 'removed tags';
  return '';
}

async function invokeFenced<T>(call: Call, client: HiverClient, definition: MutationDefinition, zero: T, validate: () => void, request: HiverRequest,
  decode: (body: Uint8Array) => T, objectId: (output: T) => string, providerIdField: string, successBranch: string, invalidMessage: string): Promise<MutationAttempt<T>> {
  const operation = definition.operation.operationId;
  try { validate(); } catch (error) { return mutationDefect(operation, zero, error); }
  const operationMaxSends = definitionMaxSends(definition);
  let fence: ReturnType<typeof newFence>;
  try { fence = newFence(call, 30_000, { writeWindowFraction: 5 / 6, maxSends: operationMaxSends }); }
  catch { return newMutationBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'single-dispatch fence could not start')); }
  const decision = fence.decide();
  // FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
  if (decision.kind === 'failClosed') return newMutationRetry(failure(operation, 'AVAILABILITY', 'the single-dispatch checkpoint could not be read; nothing was sent in this attempt'));
  if (decision.kind === 'readBackRequired') return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'earlier attempts may have sent the request and the send cap is reached, so it is not sent again'), client.receipt(call, undefined, decision.marker?.providerId ?? ''));
  // A saved provider ID proves Hiver accepted the write, so it is never resent.
  if (decision.kind === 'readBackThenResendAllowed' && decision.marker?.providerId) return newMutationUncertain(zero, failure(operation, 'TRANSPORT', "Hiver accepted an earlier attempt's write, and its answer was lost"), client.receipt(call, undefined, decision.marker.providerId));
  const priorSends = decision.priorSendUpperBound ?? 0;
  const isFirstSend = priorSends === 0;
  const canResendUnknownOutcome = priorSends + 1 < effectiveMaxSends(call, operationMaxSends);
  const key = await client.resolveKey(call);
  if (!key) return retryBeforeDispatch(fence, isFirstSend, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'));
  if (!await client.waitForSlot(call.context.cancellationSignal, Date.now() + 15_000)) {
    return retryBeforeDispatch(fence, isFirstSend, failure(operation, 'AVAILABILITY', "the wait for the connection's next Hiver request slot ended; no request was sent"));
  }
  let result: HiverExchange;
  // Dispatch refused before sending, so nothing reached Hiver in this attempt.
  try { result = await fence.dispatch(signal => client.exchange(call, operation, key, { ...request, correlationCallId: call.id }, signal, true)); }
  catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Hiver write was not dispatched')); }
  const receipt = client.receipt(call, result.response);
  if (result.kind !== 'success') {
    if (result.kind === 'rateLimited' || result.kind === 'notSent' || result.status === 503) {
      // A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
      try { await fence.recordCanResend(); } catch { /* counted as a possible send */ }
      return newMutationRetry(result.failure, result.retryAfterMs);
    }
    // defect would hide an earlier possible send.
    if (result.kind === 'defect' && !isFirstSend) return resendOrUncertain(canResendUnknownOutcome, zero, result.failure, 0, receipt);
    if (result.kind === 'notFound' || result.kind === 'defect' || (result.kind === 'rejected' && definiteRefusalStatuses.has(result.status ?? 0))) {
      if (!isFirstSend) return newMutationUncertain(zero, result.failure, receipt);
      try { await fence.recordCanResend(); } catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Dex did not record the refusal checkpoint')); }
      return newMutationBranch(result.kind === 'notFound' ? 'notFound' : result.kind === 'defect' ? 'defect' : 'providerRejected', zero, receipt, result.failure);
    }
    return resendOrUncertain(canResendUnknownOutcome, zero, result.failure, result.retryAfterMs, receipt);
  }
  let output: T;
  try { output = decode(result.response.body); }
  catch (error) {
    const invalid = failure(operation, 'PROTOCOL', `${invalidMessage}: ${message(error)}`);
    const providerId = acceptedProviderId(result.response.body, providerIdField);
    if (!providerId) return resendOrUncertain(canResendUnknownOutcome, zero, invalid, 0, receipt);
    // A decoded provider ID proves acceptance, so this write is never resent.
    try { await fence.recordProviderId(providerId); } catch { /* Accepted result is known even if the optional identity checkpoint is lost. */ }
    return newMutationUncertain(zero, invalid, client.receipt(call, result.response, providerId));
  }
  try { await fence.recordProviderId(objectId(output)); } catch { /* Accepted result is known even if the optional identity checkpoint is lost. */ }
  return newMutationBranch(successBranch, output, client.receipt(call, result.response, objectId(output)));
}

/** Only these statuses prove Hiver applied nothing; any other non-2xx answer may have been applied. */
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Retry before Dispatch; after a possible earlier send nothing is recorded, so that send stays counted. */
async function retryBeforeDispatch<T>(fence: ReturnType<typeof newFence>, isFirstSend: boolean, cause: Failure): Promise<MutationAttempt<T>> {
  if (isFirstSend) {
    try { await fence.recordNotSent(); } catch { return newMutationRetry(failure(cause.operation, 'AVAILABILITY', 'Dex did not record the not-sent checkpoint')); }
  }
  return newMutationRetry(cause);
}

/** Retry an unknown outcome while the send cap allows another send, else select uncertain. */
function resendOrUncertain<T>(canResendUnknownOutcome: boolean, zero: T, cause: Failure, retryAfterMs: number, receipt: ReturnType<HiverClient['receipt']>): MutationAttempt<T> {
  // The claim stays the last marker, so the next attempt counts this send.
  return canResendUnknownOutcome ? newMutationRetry(cause, retryAfterMs) : newMutationUncertain(zero, cause, receipt);
}

/** Return the valid ID field of a 2xx single-data body, or an empty string. */
function acceptedProviderId(body: Uint8Array, field: string): string {
  try {
    const providerId = wireID(decodeSingleData(body)[field]);
    requireHiverID(field, providerId);
    return providerId;
  } catch { return ''; }
}
