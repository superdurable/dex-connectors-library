// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence, CheckpointError, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { HelpScoutClient, HelpScoutExchange, CredentialResolution } from './client.js';
import { isProvablyUnsent } from './client.js';
import { asObject, decodeConversation, decodeCustomer, decodeThread, parseObject, parseTimestamp, wireArray, wireInteger } from './decode.js';
import { SearchConversationsDefinition, GetConversationDefinition, ReplyToConversationDefinition, UpdateConversationDefinition, FindCustomerByEmailDefinition } from './generated/definitions.js';
import type { Conversation, ConversationDetails, ConversationReply, CustomerMatches, FindCustomerByEmailInput, GetConversationInput, ReplyToConversationInput, SearchConversationsInput, SearchConversationsOutput, UpdateConversationInput, UpdateConversationOutput } from './generated/models.js';

const zeroConversation: Conversation = { id: 0, number: 0, subject: '', status: '', mailboxId: 0, threadCount: 0, tags: [] };
const zeroSearch: SearchConversationsOutput = { conversations: [], page: 0, totalPages: 0, totalConversations: 0 };
const zeroDetails: ConversationDetails = { conversation: zeroConversation, threads: [] };
const zeroUpdate: UpdateConversationOutput = { conversation: zeroConversation };
const positiveId = (value: number): boolean => Number.isSafeInteger(value) && value > 0;

/** Search one bounded Help Scout conversation page. */
export class SearchConversationsOperation implements Query<SearchConversationsInput, SearchConversationsOutput> {
  /** Bind the connection client. */ constructor(private readonly client: HelpScoutClient) {}
  /** Return the manifest operation definition. */ definition() { return SearchConversationsDefinition; }
  /** Validate typed filters and read one page. */
  async invoke(call: Call, input: SearchConversationsInput): Promise<QueryAttempt<SearchConversationsOutput>> {
    let query: Record<string, string>;
    try { query = buildSearchQuery(input); }
    catch (error) { return queryFailure('searchConversations', 'defect', 'VALIDATION', zeroSearch, {}, error); }
    const exchange = await this.client.exchange(call, 'GET', '/v2/conversations', query);
    const terminal = classifyQuery(this.client, call, 'searchConversations', exchange, zeroSearch, 0, 'providerRejected');
    if (terminal) return terminal;
    try {
      const page = parseObject(responseOf(exchange).body);
      const embedded = asObject(page._embedded ?? {});
      const rawConversations = wireArray(embedded.conversations);
      if (rawConversations.length > 100) throw new TypeError('page holds more conversations than Help Scout permits');
      const pagination = asObject(page.page ?? {});
      const links = asObject(page._links ?? {});
      const pageNumber = input.page || 1;
      const output: SearchConversationsOutput = {
        conversations: rawConversations.map(item => decodeConversation(item)), page: pageNumber,
        totalPages: wireInteger(pagination.totalPages), totalConversations: wireInteger(pagination.totalElements),
        ...(links.next !== undefined && links.next !== null && pageNumber < 10_000 ? { nextPage: pageNumber + 1 } : {}),
      };
      return newQueryBranch('searched', output, this.client.receipt(call));
    } catch (error) { return queryFailure('searchConversations', 'invalidResponse', 'PROTOCOL', zeroSearch, this.client.receipt(call), error); }
  }
}

/** Read one conversation and its newest thread page. */
export class GetConversationOperation implements Query<GetConversationInput, ConversationDetails> {
  /** Bind the connection client. */ constructor(private readonly client: HelpScoutClient) {}
  /** Return the manifest operation definition. */ definition() { return GetConversationDefinition; }
  /** Read the conversation then its newest threads. */
  async invoke(call: Call, input: GetConversationInput): Promise<QueryAttempt<ConversationDetails>> {
    if (!positiveId(input.conversationId) || (input.threadLimit !== undefined && (!Number.isInteger(input.threadLimit) || input.threadLimit < 0 || input.threadLimit > 25))) return queryFailure('getConversation', 'defect', 'VALIDATION', zeroDetails, {});
    const conversationId = input.conversationId;
    const response = await this.client.exchange(call, 'GET', conversationPath(conversationId));
    if ('response' in response && response.response.status === 301) return mergedConversation(this.client, call, conversationId, response.response);
    const terminal = classifyQuery(this.client, call, 'getConversation', response, zeroDetails, conversationId, 'providerRejected', 'notFound');
    if (terminal) return terminal;
    let conversation: Conversation;
    try { conversation = decodeConversation(parseObject(responseOf(response).body), conversationId); }
    catch (error) { return queryFailure('getConversation', 'invalidResponse', 'PROTOCOL', zeroDetails, this.client.receipt(call, conversationId), error); }
    const threadResponse = await this.client.exchange(call, 'GET', `${conversationPath(conversationId)}/threads`, { page: '1' });
    const threadTerminal = classifyQuery(this.client, call, 'getConversation', threadResponse, zeroDetails, conversationId, 'providerRejected', 'notFound');
    if (threadTerminal) return threadTerminal;
    try {
      const page = parseObject(responseOf(threadResponse).body);
      const rawThreads = wireArray(asObject(page._embedded ?? {}).threads);
      if (rawThreads.length > 100) throw new TypeError('thread page is too large');
      const limit = input.threadLimit || 10;
      const threads = rawThreads.slice(0, limit).map(decodeThread);
      const links = asObject(page._links ?? {});
      const output: ConversationDetails = { conversation, threads,
        ...(rawThreads.length > limit || links.next !== undefined && links.next !== null ? { hasOlderThreads: true } : {}) };
      return newQueryBranch('found', output, this.client.receipt(call, conversationId));
    } catch (error) { return queryFailure('getConversation', 'invalidResponse', 'PROTOCOL', zeroDetails, this.client.receipt(call, conversationId), error); }
  }
}

/** Find customer profiles by one exact email address. */
export class FindCustomerByEmailOperation implements Query<FindCustomerByEmailInput, CustomerMatches> {
  /** Bind the connection client. */ constructor(private readonly client: HelpScoutClient) {}
  /** Return the manifest operation definition. */ definition() { return FindCustomerByEmailDefinition; }
  /** Read the first customer search page. */
  async invoke(call: Call, input: FindCustomerByEmailInput): Promise<QueryAttempt<CustomerMatches>> {
    const empty: CustomerMatches = { email: input.email, customers: [] };
    if (!isBareEmail(input.email)) return queryFailure('findCustomerByEmail', 'defect', 'VALIDATION', empty, {});
    const exchange = await this.client.exchange(call, 'GET', '/v3/customers', { email: input.email });
    const terminal = classifyQuery(this.client, call, 'findCustomerByEmail', exchange, { email: '', customers: [] }, 0, 'providerRejected');
    if (terminal) return terminal;
    try {
      const page = parseObject(responseOf(exchange).body);
      const rawCustomers = wireArray(asObject(page._embedded ?? {}).customers);
      const customers = rawCustomers.slice(0, 50).map(decodeCustomer);
      const links = asObject(page._links ?? {});
      const output: CustomerMatches = { email: input.email, customers,
        ...(rawCustomers.length > 50 || links.next !== undefined && links.next !== null ? { hasMore: true } : {}) };
      if (!customers.length) return queryFailure('findCustomerByEmail', 'notFound', 'NOT_FOUND', output, this.client.receipt(call));
      return newQueryBranch('found', output, this.client.receipt(call, customers[0]!.id));
    } catch (error) { return queryFailure('findCustomerByEmail', 'invalidResponse', 'PROTOCOL', empty, this.client.receipt(call), error); }
  }
}

/** Set final conversation values with read-before-write retries. */
export class UpdateConversationOperation implements Mutation<UpdateConversationInput, UpdateConversationOutput> {
  /** Bind the connection client. */ constructor(private readonly client: HelpScoutClient) {}
  /** Return the manifest operation definition. */ definition() { return UpdateConversationDefinition; }
  /** Correlate repeat-safe writes with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Read, write changed final values, and read back. */
  async invoke(call: Call, input: UpdateConversationInput): Promise<MutationAttempt<UpdateConversationOutput>> {
    try { validateUpdateInput(input); }
    catch (error) { return mutationFailure('updateConversation', 'defect', 'VALIDATION', zeroUpdate, {}, error); }
    const read = async (value: UpdateConversationOutput): Promise<Conversation | MutationAttempt<UpdateConversationOutput>> => {
      const exchange = await this.client.exchange(call, 'GET', conversationPath(input.conversationId));
      if ('response' in exchange && exchange.response.status === 301) return mutationFailure('updateConversation', 'notFound', 'NOT_FOUND', value, this.client.receipt(call, input.conversationId));
      const terminal = classifyMutation(this.client, call, 'updateConversation', exchange, value, input.conversationId, 'providerRejected', 'notFound');
      if (terminal) return terminal;
      try { return decodeConversation(parseObject(responseOf(exchange).body), input.conversationId); }
      catch (error) { return mutationFailure('updateConversation', 'invalidResponse', 'PROTOCOL', value, this.client.receipt(call, input.conversationId), error); }
    };
    const current = await read(zeroUpdate);
    if (!('id' in current)) return current;
    const writes = planUpdates(input, current);
    if (!writes.length) return newMutationBranch('updated', { conversation: current, wasAlreadyApplied: true }, this.client.receipt(call, input.conversationId));
    for (const write of writes) {
      const exchange = await this.client.exchange(call, write.method, write.path, undefined, write.body);
      const terminal = classifyMutation(this.client, call, 'updateConversation', exchange, { conversation: current }, input.conversationId, 'providerRejected', 'notFound');
      if (terminal) return terminal;
    }
    const updated = await read({ conversation: current });
    if (!('id' in updated)) return updated;
    return newMutationBranch('updated', { conversation: updated }, this.client.receipt(call, input.conversationId));
  }
}

/** Add one customer reply or internal note under a durable dispatch fence. */
export class ReplyToConversationOperation implements Mutation<ReplyToConversationInput, ConversationReply> {
  /** Bind the connection client. */ constructor(private readonly client: HelpScoutClient) {}
  /** Return the manifest operation definition. */ definition() { return ReplyToConversationDefinition; }
  /** Correlate the fenced write with the Dex Call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Checkpoint a single send and confirm accepted thread IDs on retry. */
  async invoke(call: Call, input: ReplyToConversationInput): Promise<MutationAttempt<ConversationReply>> {
    const output: ConversationReply = { conversationId: input.conversationId,
      ...(input.isInternalNote ? { isInternalNote: true } : {}), ...(input.customerId ? { customerId: input.customerId } : {}) };
    try { validateReplyInput(input); }
    catch (error) { return mutationFailure('replyToConversation', 'defect', 'VALIDATION', output, {}, error); }
    const receipt = this.client.receipt(call, input.conversationId);
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 30_000); }
    catch (error) { return mutationFailure('replyToConversation', 'defect', 'LOCAL_DEFECT', output, {}, error); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'the dispatch checkpoint is unavailable, so the thread is not sent again'), receipt);
    if (decision.kind === 'readBackRequired') return this.readBack(call, output, decision.marker);
    const credentials = await this.client.resolveCredentials(call);
    if (credentials.kind !== 'resolved') {
      if (credentials.kind === 'terminal') return mutationFailure('replyToConversation', 'defect', 'AUTHENTICATION', output, {}, new Error(credentials.message));
      try { await fence.recordNotSent(); }
      catch { /* A failed checkpoint cannot authorize another send. */ }
      return newMutationRetry(failure('replyToConversation', 'AVAILABILITY', credentials.message));
    }
    if (!input.isInternalNote && !input.customerId) {
      const read = await this.client.exchangeWithToken(call, credentials.token, 'GET', conversationPath(input.conversationId));
      if ('response' in read && read.response.status === 301) return mutationFailure('replyToConversation', 'notFound', 'NOT_FOUND', output, receipt);
      const terminal = classifyMutation(this.client, call, 'replyToConversation', read, output, input.conversationId, 'providerRejected', 'notFound');
      if (terminal) { if (terminal.kind === 'retry') { try { await fence.recordNotSent(); } catch { /* A failed checkpoint remains fail closed. */ } } return terminal; }
      try {
        const conversation = decodeConversation(parseObject(responseOf(read).body), input.conversationId);
        if (!conversation.primaryCustomer) return mutationFailure('replyToConversation', 'providerRejected', 'VALIDATION', output, receipt, new Error('the conversation has no primary customer to reply to; pass customerId'));
        output.customerId = conversation.primaryCustomer.id;
      } catch (error) { return mutationFailure('replyToConversation', 'providerRejected', 'PROTOCOL', output, receipt, error); }
    }
    const path = `${conversationPath(input.conversationId)}/${input.isInternalNote ? 'notes' : 'reply'}`;
    const body = input.isInternalNote ? { text: input.text, ...(input.status ? { status: input.status } : {}) } :
      { text: input.text, ...(input.status ? { status: input.status } : {}), customer: { id: output.customerId } };
    let exchange: HelpScoutExchange;
    try { exchange = await fence.dispatch(() => this.client.exchangeWithToken(call, credentials.token, 'POST', path, undefined, body, false)); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure('replyToConversation', 'AVAILABILITY', 'the thread was not sent before the Execute dispatch deadline'));
      return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', "the thread may exist because Help Scout's answer was lost"), receipt);
    }
    if ('error' in exchange) {
      if (isProvablyUnsent(exchange.error)) {
        try { await fence.recordCanResend(); } catch { /* A failed checkpoint remains fail closed. */ }
        return newMutationRetry(failure('replyToConversation', 'TRANSPORT', 'Help Scout could not be reached, so no thread was sent'));
      }
      return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', "the thread may exist because Help Scout's answer was lost"), receipt);
    }
    const response = exchange.response;
    if (response.status >= 200 && response.status < 300) {
      const raw = String(response.headers['resource-id'] ?? '').trim();
      const threadId = /^[1-9]\d*$/.test(raw) ? Number(raw) : 0;
      if (!positiveId(threadId)) return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'Help Scout accepted the thread without a valid Resource-Id; the thread may exist'), receipt);
      output.threadId = threadId;
      try { await fence.recordProviderId(String(threadId)); } catch { /* A lost ID still forbids another send. */ }
      return newMutationBranch('replied', output, this.client.receipt(call, threadId));
    }
    const outcome = this.client.classifyFailure('replyToConversation', response, credentials.token);
    const status = response.status;
    if ([429, 503, 504].includes(status)) {
      try { await fence.recordCanResend(); } catch { /* A failed checkpoint remains fail closed. */ }
      return newMutationRetry(outcome.failure, outcome.retryAfterMs);
    }
    if ([404, 410].includes(status)) return newMutationBranch('notFound', output, receipt, outcome.failure);
    if (status >= 400 && status < 500 && ![408, 409].includes(status)) return newMutationBranch('providerRejected', output, receipt, outcome.failure);
    return newMutationUncertain(output, { ...outcome.failure, message: `${outcome.failure.message}; the thread may exist` }, receipt);
  }

  private async readBack(call: Call, output: ConversationReply, marker: { callId: string; providerId?: string | undefined } | undefined): Promise<MutationAttempt<ConversationReply>> {
    const receipt = this.client.receipt(call, output.conversationId);
    const threadId = Number(marker?.providerId);
    if (marker?.callId !== call.id || !positiveId(threadId)) return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'an earlier attempt may have sent the thread without a confirmable ID'), receipt);
    const credential = await this.client.resolveCredentials(call);
    if (credential.kind === 'retry') return newMutationRetry(failure('replyToConversation', 'AVAILABILITY', credential.message));
    if (credential.kind === 'terminal') return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'the accepted thread could not be confirmed because authorization failed'), receipt);
    for (let page = 1; page <= 10; page++) {
      const exchange = await this.client.exchangeWithToken(call, credential.token, 'GET', `${conversationPath(output.conversationId)}/threads`, { page: String(page) });
      if ('error' in exchange) return newMutationRetry(failure('replyToConversation', 'AVAILABILITY', 'Help Scout thread read-back is temporarily unavailable'));
      if (exchange.isTooLarge || exchange.isMalformed) return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'Help Scout returned an unusable thread read-back'), receipt);
      if ([429, 408].includes(exchange.response.status) || exchange.response.status >= 500) return newMutationRetry(failure('replyToConversation', 'AVAILABILITY', 'Help Scout thread read-back is temporarily unavailable'));
      if (exchange.response.status !== 200) return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'Help Scout did not confirm the accepted thread'), receipt);
      try {
        const body = parseObject(exchange.response.body);
        const threads = wireArray(asObject(body._embedded ?? {}).threads);
        if (threads.length > 100) throw new TypeError('thread page is too large');
        const found = threads.map(asObject).find(thread => wireInteger(thread.id) === threadId);
        if (found) {
          output.threadId = threadId;
          if (!output.isInternalNote && !output.customerId) {
            const customer = found.customer;
            if (customer && wireInteger(asObject(customer).id) > 0) output.customerId = wireInteger(asObject(customer).id);
            else {
              const primary = await this.client.exchangeWithToken(call, credential.token, 'GET', conversationPath(output.conversationId));
              if ('response' in primary && primary.response.status === 200 && !primary.isTooLarge && !primary.isMalformed) {
                try {
                  const customerId = decodeConversation(parseObject(primary.response.body), output.conversationId).primaryCustomer?.id;
                  if (customerId) output.customerId = customerId;
                }
                catch { /* A failed read-back leaves the accepted thread confirmed without a customer ID. */ }
              }
            }
          }
          return newMutationBranch('replied', output, this.client.receipt(call, threadId));
        }
        if (asObject(body._links ?? {}).next === undefined) break;
      } catch { return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'Help Scout returned an unusable thread read-back'), receipt); }
    }
    return newMutationUncertain(output, failure('replyToConversation', 'TRANSPORT', 'Help Scout did not confirm the accepted thread in its thread pages'), receipt);
  }
}

function validateUpdateInput(input: UpdateConversationInput): void {
  if (!positiveId(input.conversationId)) throw new TypeError('conversationId must be positive');
  if (input.status && !['active', 'pending', 'closed', 'spam'].includes(input.status)) throw new TypeError('status is invalid');
  if (input.assigneeId !== undefined && (!Number.isSafeInteger(input.assigneeId) || input.assigneeId < 0)) throw new TypeError('assigneeId is invalid');
  if (input.isUnassigned && input.assigneeId) throw new TypeError('isUnassigned cannot be combined with assigneeId');
  validateTags('addTags', input.addTags ?? []); validateTags('removeTags', input.removeTags ?? []);
  if ((input.addTags ?? []).some(tag => (input.removeTags ?? []).some(other => other.toLowerCase() === tag.toLowerCase()))) throw new TypeError('tag appears in both addTags and removeTags');
  if (!input.status && !input.assigneeId && !input.isUnassigned && !input.addTags?.length && !input.removeTags?.length) throw new TypeError('at least one change is required');
}

function planUpdates(input: UpdateConversationInput, current: Conversation): Array<{ method: string; path: string; body: unknown }> {
  const path = conversationPath(input.conversationId);
  const writes: Array<{ method: string; path: string; body: unknown }> = [];
  if (input.status && input.status !== current.status) writes.push({ method: 'PATCH', path, body: { op: 'replace', path: '/status', value: input.status } });
  if (input.isUnassigned && current.assignee) writes.push({ method: 'PATCH', path, body: { op: 'remove', path: '/assignTo' } });
  else if (input.assigneeId && input.assigneeId !== current.assignee?.id) writes.push({ method: 'PATCH', path, body: { op: 'replace', path: '/assignTo', value: input.assigneeId } });
  const tags = (current.tags ?? []).filter(tag => !(input.removeTags ?? []).some(remove => remove.toLowerCase() === tag.toLowerCase()));
  for (const tag of input.addTags ?? []) if (!tags.some(existing => existing.toLowerCase() === tag.toLowerCase())) tags.push(tag);
  if (tags.length !== (current.tags ?? []).length || tags.some((tag, index) => tag !== (current.tags ?? [])[index])) writes.push({ method: 'PUT', path: `${path}/tags`, body: { tags } });
  return writes;
}

function validateReplyInput(input: ReplyToConversationInput): void {
  if (!positiveId(input.conversationId) || !input.text?.trim() || Buffer.byteLength(input.text) > 65_536 || (input.customerId !== undefined && (!Number.isSafeInteger(input.customerId) || input.customerId < 0)) || input.isInternalNote && !!input.customerId || input.status && !['active', 'pending', 'closed', 'spam'].includes(input.status)) throw new TypeError('reply input is invalid');
}

function classifyMutation<T>(client: HelpScoutClient, call: Call, operation: string, exchange: HelpScoutExchange | CredentialResolution, value: T, objectId: number, rejectedBranch: string, notFoundBranch = rejectedBranch): MutationAttempt<T> | undefined {
  if ('kind' in exchange) {
    if (exchange.kind === 'retry') return newMutationRetry(failure(operation, 'AVAILABILITY', exchange.message));
    if (exchange.kind === 'terminal') return newMutationBranch('defect', value, {}, failure(operation, 'AUTHENTICATION', exchange.message));
    throw new TypeError('unexpected resolved credential');
  }
  if ('error' in exchange) return newMutationRetry(failure(operation, 'TRANSPORT', 'Help Scout could not be reached'));
    if (exchange.isTooLarge) return mutationFailure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', value, client.receipt(call, objectId));
    if (exchange.isMalformed) return mutationFailure(operation, 'invalidResponse', 'PROTOCOL', value, client.receipt(call, objectId));
  const response = exchange.response;
  if (response.status >= 200 && response.status < 300) return undefined;
  const outcome = client.classifyFailure(operation, response, exchange.accessToken);
  if (outcome.isRetry) return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch([404, 410].includes(response.status) ? notFoundBranch : rejectedBranch, value, client.receipt(call, objectId, outcome.logRef), outcome.failure);
}

function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], value: T, receipt: ReceiptDraft, error?: unknown): MutationAttempt<T> {
  return newMutationBranch(branch, value, receipt, failure(operation, kind, error instanceof Error ? error.message : 'Help Scout returned a malformed response'));
}

function responseOf(exchange: HelpScoutExchange | CredentialResolution): ProviderResponse {
  if ('response' in exchange) return exchange.response;
  throw new TypeError('Help Scout exchange has no response');
}

function buildSearchQuery(input: SearchConversationsInput): Record<string, string> {
  const page = input.page || 1;
  if (!Number.isInteger(page) || page < 1 || page > 10_000) throw new TypeError('page must be from 1 through 10000, or zero for page 1');
  if (input.mailboxId !== undefined && (!Number.isSafeInteger(input.mailboxId) || input.mailboxId < 0)) throw new TypeError('mailboxId must be a positive Help Scout inbox ID, or zero for every inbox');
  const query: Record<string, string> = { page: String(page) };
  if (input.mailboxId) query.mailbox = String(input.mailboxId);
  if (input.status) { if (!['active', 'pending', 'closed', 'spam', 'all'].includes(input.status)) throw new TypeError('status is invalid'); query.status = input.status; }
  if (input.tag) { validateTags('tag', [input.tag]); query.tag = input.tag; }
  if (input.customerEmail) { if (!isBareEmail(input.customerEmail)) throw new TypeError('customerEmail must be one bare email address'); query.query = `(email:"${input.customerEmail}")`; }
  if (input.modifiedSince) { query.modifiedSince = parseTimestamp(input.modifiedSince).slice(0, 19) + 'Z'; }
  return query;
}

function mergedConversation(client: HelpScoutClient, call: Call, conversationId: number, response: ProviderResponse): QueryAttempt<ConversationDetails> {
  const location = String(response.headers.location ?? '');
  const matched = /^(?:https:\/\/api\.helpscout\.net)?\/v2\/conversations\/([1-9]\d*)$/.exec(location);
  const targetId = matched ? Number(matched[1]) : 0;
  if (!positiveId(targetId) || targetId === conversationId) return queryFailure('getConversation', 'providerRejected', 'PROTOCOL', zeroDetails, client.receipt(call, conversationId));
  return newQueryBranch('merged', { ...zeroDetails, mergedIntoConversationId: targetId }, client.receipt(call, targetId), failure('getConversation', 'NOT_FOUND', `conversation ${conversationId} was merged into conversation ${targetId}`));
}

function classifyQuery<T>(client: HelpScoutClient, call: Call, operation: string, exchange: HelpScoutExchange | CredentialResolution, zero: T, objectId: number, rejectedBranch: string, notFoundBranch = rejectedBranch): QueryAttempt<T> | undefined {
  if ('kind' in exchange) {
    if (exchange.kind === 'retry') return newQueryRetry(failure(operation, 'AVAILABILITY', exchange.message));
    if (exchange.kind === 'terminal') return newQueryBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', exchange.message));
    throw new TypeError('unexpected resolved credential');
  }
  if ('error' in exchange) return newQueryRetry(failure(operation, 'TRANSPORT', 'Help Scout could not be reached'));
  if (exchange.isTooLarge) return queryFailure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', zero, client.receipt(call, objectId));
  if (exchange.isMalformed) return queryFailure(operation, 'invalidResponse', 'PROTOCOL', zero, client.receipt(call, objectId));
  const response = exchange.response;
  if (response.status >= 200 && response.status < 300) return undefined;
  const outcome = client.classifyFailure(operation, response, exchange.accessToken);
  if (outcome.isRetry) return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  const branch = [404, 410].includes(response.status) ? notFoundBranch : rejectedBranch;
  return newQueryBranch(branch, zero, client.receipt(call, objectId, outcome.logRef), outcome.failure);
}

function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): QueryAttempt<T> {
  const message = error instanceof Error ? error.message : branch === 'notFound' ? 'no Help Scout customer has this email address' : 'Help Scout returned a malformed response';
  return newQueryBranch(branch, zero, receipt, failure(operation, kind, message));
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'helpscout', operation, message }; }
function conversationPath(conversationId: number): string { return `/v2/conversations/${conversationId}`; }

function isBareEmail(value: string): boolean {
  if (!value || /[ "'()<>,;\\]/u.test(value) || hasControlCharacter(value)) return false;
  const at = value.lastIndexOf('@');
  if (at < 1 || at === value.length - 1) return false;
  const local = value.slice(0, at), domain = value.slice(at + 1);
  if (local.startsWith('.') || local.endsWith('.') || local.includes('..') || domain.startsWith('.') || domain.endsWith('.') || domain.includes('..')) return false;
  const validLocal = /^[\p{L}\p{N}!#$%&*+\-/=?^_`{|}~]+(?:\.[\p{L}\p{N}!#$%&*+\-/=?^_`{|}~]+)*$/u.test(local);
  const validDomain = domain.startsWith('[') && domain.endsWith(']') ? isIP(domain.slice(1, -1)) === 4 : /^[\p{L}\p{N}_-]+(?:\.[\p{L}\p{N}_-]+)*$/u.test(domain);
  return validLocal && validDomain;
}

function validateTags(field: string, tags: string[]): void {
  const seen = new Set<string>();
  for (const tag of tags) {
    if (!tag || tag.trim() !== tag || Buffer.byteLength(tag) > 100 || /[,"()\\]/u.test(tag) || hasControlCharacter(tag) || seen.has(tag.toLowerCase())) throw new TypeError(`${field} holds an invalid or duplicate tag`);
    seen.add(tag.toLowerCase());
  }
}

function hasControlCharacter(value: string): boolean { return [...value].some(character => { const code = character.codePointAt(0)!; return code < 32 || code === 127; }); }
