// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence, type Marker } from '@superdurable/connectors-sdk/singledispatch';
import { failure, isCredentialRejected, retryAfter, type SlackClient, type SlackExchange } from './client.js';
import { GetThreadReplyDefinition, GetWorkspaceIdentityDefinition, ListChannelMessagesDefinition, ListChannelsDefinition, ListThreadMessagesDefinition, PostChannelMessageDefinition, PostThreadReplyDefinition } from './generated/definitions.js';
import type { Channel, ChannelMessage, GetThreadReplyInput, GetThreadReplyOutput, GetWorkspaceIdentityInput, GetWorkspaceIdentityOutput, ListChannelMessagesInput, ListChannelMessagesOutput, ListChannelsInput, ListChannelsOutput, ListThreadMessagesInput, ListThreadMessagesOutput, Message, PostChannelMessageInput, PostMessageOutput, PostThreadReplyInput } from './generated/models.js';

const emptyMessage: Message = { channelId: '', timestamp: '', text: '' };
const emptyReply: GetThreadReplyOutput = { message: emptyMessage };
const emptyPost: PostMessageOutput = { message: emptyMessage };
const emptyList: ListThreadMessagesOutput = { messages: [] };
const emptyChannels: ListChannelsOutput = { channels: [] };
const emptyChannelHistory: ListChannelMessagesOutput = { messages: [] };
const emptyIdentity: GetWorkspaceIdentityOutput = { teamId: '', teamName: '', workspaceUrl: '', botUserId: '', botUserName: '', botId: '' };

/** Read one Slack channel page with the bot token and filter it by name. */
export class ListChannelsOperation implements Query<ListChannelsInput, ListChannelsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return ListChannelsDefinition; }
  /** Invoke Slack conversations.list, or users.conversations for joined channels only. */
  async invoke(call: Call, input: ListChannelsInput): Promise<QueryAttempt<ListChannelsOutput>> {
    const operation = 'listChannels';
    const visibility = input.visibility ?? '';
    const channelTypes = Object.hasOwn(channelTypesByVisibility, visibility) ? channelTypesByVisibility[visibility] : undefined;
    const limit = input.limit ?? 0;
    if (!channelTypes || !Number.isInteger(limit) || limit < 0 || limit > 999) return newQueryBranch('defect', emptyChannels, {}, failure(operation, 'VALIDATION', 'visibility must be blank, public, or private, and limit must be from 0 through 999'));
    const credential = await this.client.resolveToken(call, 'bot');
    if (!credential.token) return newQueryBranch('defect', emptyChannels, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable'));
    const query = new URLSearchParams({ exclude_archived: 'true', types: channelTypes, limit: String(limit || 200) });
    if (input.cursor) query.set('cursor', input.cursor);
    // users.conversations lists only joined channels and omits is_member.
    const path = input.memberOnly ? 'users.conversations' : 'conversations.list';
    const response = await this.client.get(call, credential.token, path, query, 'bot');
    const classified = classifyQuery(this.client, call, operation, response, emptyChannels, 'Slack channels are temporarily unavailable', 'Slack rate limited the channel query');
    if (classified) return classified;
    const body = response.body as SlackBody & { channels?: unknown };
    const channels = decodeChannels(body.channels);
    if (!channels || channels.some(channel => !channel.channelId)) return newQueryBranch('invalidResponse', emptyChannels, this.client.receipt(call, response), failure(operation, 'PROTOCOL', 'Slack returned an invalid channel list'));
    const nameFilter = channelNameFilter(input.nameFilter ?? '');
    const matches = channels.filter(channel => !nameFilter || channel.name.toLowerCase().includes(nameFilter))
      .map(channel => ({ ...channel, isMember: channel.isMember || Boolean(input.memberOnly) }));
    const nextCursor = body.response_metadata?.next_cursor;
    return newQueryBranch('listed', { channels: matches, ...(nextCursor ? { nextCursor } : {}) }, this.client.receipt(call, response));
  }
}

/** Read the bot token's workspace and identity with a POST to auth.test, as Slack documents it. */
export class GetWorkspaceIdentityOperation implements Query<GetWorkspaceIdentityInput, GetWorkspaceIdentityOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return GetWorkspaceIdentityDefinition; }
  /** Invoke the Slack auth.test query. */
  async invoke(call: Call, _input: GetWorkspaceIdentityInput): Promise<QueryAttempt<GetWorkspaceIdentityOutput>> {
    const operation = 'getWorkspaceIdentity';
    const credential = await this.client.resolveToken(call, 'bot');
    if (!credential.token) return newQueryBranch('defect', emptyIdentity, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable'));
    const response = await this.client.postForm(call, credential.token, 'auth.test', new URLSearchParams(), 'bot');
    const classified = classifyQuery(this.client, call, operation, response, emptyIdentity, 'Slack workspace identity is temporarily unavailable', 'Slack rate limited the identity query');
    if (classified) return classified;
    const identity = decodeIdentity(response.body as Record<string, unknown>);
    if (!identity || !identity.botUserId || !(identity.teamId || identity.enterpriseId)) return newQueryBranch('invalidResponse', emptyIdentity, this.client.receipt(call, response), failure(operation, 'PROTOCOL', 'Slack returned an invalid workspace identity'));
    return newQueryBranch('read', identity, this.client.receipt(call, response, identity.teamId));
  }
}

/** Read one bounded Slack thread page. */
export class ListThreadMessagesOperation implements Query<ListThreadMessagesInput, ListThreadMessagesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return ListThreadMessagesDefinition; }
  /** Invoke the Slack conversations.replies query. */
  async invoke(call: Call, input: ListThreadMessagesInput): Promise<QueryAttempt<ListThreadMessagesOutput>> {
    const operation = 'listThreadMessages';
    if (!input.channelId.trim() || !input.threadTimestamp.trim() || !Number.isInteger(input.pageSize) || input.pageSize < 1 || input.pageSize > 15) return newQueryBranch('defect', emptyList, {}, failure(operation, 'VALIDATION', 'channel, thread timestamp, and page size from 1 through 15 are required'));
    const credential = await this.client.resolveToken(call, 'user');
    if (!credential.token) return newQueryBranch('defect', emptyList, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable'));
    const query = new URLSearchParams({ channel: input.channelId, ts: input.threadTimestamp, limit: String(input.pageSize) });
    if (input.cursor) query.set('cursor', input.cursor);
    const response = await this.client.get(call, credential.token, 'conversations.replies', query);
    const classified = classifyQuery(this.client, call, operation, response, emptyList, 'Slack thread messages are temporarily unavailable', 'Slack rate limited the thread query');
    if (classified) return classified;
    const body = response.body as SlackBody;
    const messages = (body.messages ?? []).map(message => convertMessage(input.channelId, message));
    return newQueryBranch('read', { messages, ...(body.response_metadata?.next_cursor ? { nextCursor: body.response_metadata.next_cursor } : {}) }, this.client.receipt(call, response));
  }
}

/** Read one specific Slack thread reply. */
export class GetThreadReplyOperation implements Query<GetThreadReplyInput, GetThreadReplyOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return GetThreadReplyDefinition; }
  /** Invoke the Slack conversations.replies query. */
  async invoke(call: Call, input: GetThreadReplyInput): Promise<QueryAttempt<GetThreadReplyOutput>> {
    const operation = 'getThreadReply';
    if (!input.channelId.trim() || !input.threadTimestamp.trim() || !input.replyTimestamp.trim() || input.replyTimestamp === input.threadTimestamp) return newQueryBranch('defect', emptyReply, {}, failure(operation, 'VALIDATION', 'channel, thread timestamp, and a distinct reply timestamp are required'));
    const credential = await this.client.resolveToken(call, 'user');
    if (!credential.token) return newQueryBranch('defect', emptyReply, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable'));
    const query = replyQuery(input.channelId, input.threadTimestamp, input.replyTimestamp);
    const response = await this.client.get(call, credential.token, 'conversations.replies', query);
    const classified = classifyQuery(this.client, call, operation, response, emptyReply, 'Slack thread reply is temporarily unavailable', 'Slack rate limited the reply query');
    if (classified) return classified;
    const body = response.body as SlackBody;
    const match = (body.messages ?? []).find(message => message.ts === input.replyTimestamp && message.thread_ts === input.threadTimestamp);
    return match ? newQueryBranch('found', { message: convertMessage(input.channelId, match) }, this.client.receipt(call, response, match.ts ?? ''))
      : newQueryBranch('notFound', emptyReply, this.client.receipt(call, response), failure(operation, 'NOT_FOUND', 'Slack thread reply was not found'));
  }
}

/** Read one bounded, newest-first page of a channel's top-level messages with the bot token. */
export class ListChannelMessagesOperation implements Query<ListChannelMessagesInput, ListChannelMessagesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return ListChannelMessagesDefinition; }
  /** Invoke the Slack conversations.history query. */
  async invoke(call: Call, input: ListChannelMessagesInput): Promise<QueryAttempt<ListChannelMessagesOutput>> {
    const operation = 'listChannelMessages';
    const pageSize = input.pageSize ?? 0;
    const timestamps = [input.oldestTimestamp ?? '', input.latestTimestamp ?? ''];
    if (!input.channelId.trim() || !Number.isInteger(pageSize) || pageSize < 0 || pageSize > maximumChannelHistoryPageSize ||
      timestamps.some(timestamp => timestamp !== '' && !slackTimestampPattern.test(timestamp))) {
      return newQueryBranch('defect', emptyChannelHistory, {}, failure(operation, 'VALIDATION', 'channel, valid Slack message timestamps, and page size 1 through 15 (blank or 0 reads 15) are required'));
    }
    const credential = await this.client.resolveToken(call, 'bot');
    if (!credential.token) return newQueryBranch('defect', emptyChannelHistory, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable'));
    const query = new URLSearchParams({ channel: input.channelId, limit: String(pageSize || maximumChannelHistoryPageSize) });
    if (input.oldestTimestamp) query.set('oldest', input.oldestTimestamp);
    if (input.latestTimestamp) query.set('latest', input.latestTimestamp);
    if (input.isInclusive) query.set('inclusive', 'true');
    if (input.cursor) query.set('cursor', input.cursor);
    const response = await this.client.get(call, credential.token, 'conversations.history', query, 'bot');
    const classified = classifyQuery(this.client, call, operation, response, emptyChannelHistory, 'Slack channel history is temporarily unavailable', 'Slack rate limited the channel history query');
    if (classified) return classified;
    const body = response.body as SlackBody;
    const messages = decodeChannelHistory(input.channelId, (response.body as Record<string, unknown>).messages);
    if (!messages) return newQueryBranch('invalidResponse', emptyChannelHistory, this.client.receipt(call, response), failure(operation, 'PROTOCOL', 'Slack returned an invalid channel history'));
    if (messages.some(message => !message.timestamp)) return newQueryBranch('invalidResponse', emptyChannelHistory, this.client.receipt(call, response), failure(operation, 'PROTOCOL', 'Slack returned a channel message without a timestamp'));
    const nextCursor = body.response_metadata?.next_cursor;
    return newQueryBranch('read', { messages, ...(nextCursor ? { nextCursor } : {}) }, this.client.receipt(call, response));
  }
}

/** Post a top-level channel message with a durable single-dispatch fence. */
export class PostChannelMessageOperation implements Mutation<PostChannelMessageInput, PostMessageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return PostChannelMessageDefinition; }
  /** Match Go's stable Call ID idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Invoke the Slack chat.postMessage mutation. */
  invoke(call: Call, input: PostChannelMessageInput): Promise<MutationAttempt<PostMessageOutput>> { return postMessage(this.client, call, 'postChannelMessage', input.channelId, '', input.text); }
}

/** Post a thread reply with a durable single-dispatch fence. */
export class PostThreadReplyOperation implements Mutation<PostThreadReplyInput, PostMessageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SlackClient) {}
  /** Return the manifest definition. */ definition() { return PostThreadReplyDefinition; }
  /** Match Go's stable Call ID idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Invoke the Slack chat.postMessage mutation. */
  invoke(call: Call, input: PostThreadReplyInput): Promise<MutationAttempt<PostMessageOutput>> { return postMessage(this.client, call, 'postThreadReply', input.channelId, input.threadTimestamp, input.text); }
}

// Slack caps conversations.history at 15 messages for non-Marketplace apps installed since May 2025.
const maximumChannelHistoryPageSize = 15;
const slackTimestampPattern = /^[0-9]+(\.[0-9]+)?$/;

const channelTypesByVisibility: Record<string, string> = { '': 'public_channel,private_channel', public: 'public_channel', private: 'private_channel' };

/** Read an optional field like Go's json.Unmarshal: null is the zero value and a wrong type fails. */
function optionalField<T>(value: Record<string, unknown>, key: string, type: 'string' | 'boolean' | 'object', zero: T): T | undefined {
  const item = value[key];
  if (item === undefined || item === null) return zero;
  if (type === 'object' ? typeof item !== 'object' || Array.isArray(item) : typeof item !== type) return undefined;
  return item as T;
}

/** Decode conversations.list and users.conversations channels with Go-compatible type checks. */
function decodeChannels(value: unknown): Channel[] | undefined {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) return undefined;
  const channels: Channel[] = [];
  for (const raw of value) {
    const item = (raw ?? {}) as Record<string, unknown>;
    if (typeof item !== 'object' || Array.isArray(item)) return undefined;
    const topic = optionalField<Record<string, unknown>>(item, 'topic', 'object', {});
    const purpose = optionalField<Record<string, unknown>>(item, 'purpose', 'object', {});
    const fields = [
      optionalField(item, 'id', 'string', ''), optionalField(item, 'name', 'string', ''),
      optionalField(item, 'is_private', 'boolean', false), optionalField(item, 'is_member', 'boolean', false),
      topic && optionalField(topic, 'value', 'string', ''), purpose && optionalField(purpose, 'value', 'string', ''),
    ] as const;
    if (fields.some(field => field === undefined)) return undefined;
    const [channelId, name, isPrivate, isMember, topicValue, purposeValue] = fields as unknown as [string, string, boolean, boolean, string, string];
    channels.push({ channelId, name, isPrivate, isMember, ...(topicValue ? { topic: topicValue } : {}), ...(purposeValue ? { purpose: purposeValue } : {}) });
  }
  return channels;
}

/** Decode conversations.history messages with Go-compatible type checks for the fields shared decoding skips. */
function decodeChannelHistory(channelId: string, value: unknown): ChannelMessage[] | undefined {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) return undefined;
  const messages: ChannelMessage[] = [];
  for (const raw of value) {
    const item = (raw ?? {}) as Record<string, unknown>;
    if (typeof item !== 'object' || Array.isArray(item)) return undefined;
    const fields = [
      optionalField(item, 'ts', 'string', ''), optionalField(item, 'thread_ts', 'string', ''), optionalField(item, 'user', 'string', ''),
      optionalField(item, 'bot_id', 'string', ''), optionalField(item, 'text', 'string', ''),
    ] as const;
    const replyCount = item.reply_count ?? 0;
    if (fields.some(field => field === undefined) || !Number.isSafeInteger(replyCount)) return undefined;
    const [timestamp, threadTimestamp, userId, botId, text] = fields as unknown as [string, string, string, string, string];
    messages.push({
      channelId, timestamp, ...(threadTimestamp ? { threadTimestamp } : {}), ...(userId ? { userId } : {}), ...(botId ? { botId } : {}), text,
      ...(replyCount ? { replyCount: replyCount as number } : {}),
    });
  }
  return messages;
}

/** Decode the auth.test identity with Go-compatible type checks. */
function decodeIdentity(body: Record<string, unknown>): GetWorkspaceIdentityOutput | undefined {
  const fields = [
    optionalField(body, 'team_id', 'string', ''), optionalField(body, 'team', 'string', ''), optionalField(body, 'url', 'string', ''),
    optionalField(body, 'enterprise_id', 'string', ''), optionalField(body, 'user_id', 'string', ''), optionalField(body, 'user', 'string', ''), optionalField(body, 'bot_id', 'string', ''),
  ] as const;
  if (fields.some(field => field === undefined)) return undefined;
  const [teamId, teamName, workspaceUrl, enterpriseId, botUserId, botUserName, botId] = fields as unknown as [string, string, string, string, string, string, string];
  return { teamId, teamName, workspaceUrl, ...(enterpriseId ? { enterpriseId } : {}), botUserId, botUserName, botId };
}

/** Trim spaces and one leading # and lowercase the filter. */
function channelNameFilter(nameFilter: string): string {
  const trimmed = nameFilter.trim();
  return (trimmed.startsWith('#') ? trimmed.slice(1) : trimmed).toLowerCase();
}

interface SlackMessage { channel?: string; ts?: string; thread_ts?: string; user?: string; text?: string }
interface SlackBody { ok?: boolean; error?: string; channel?: string; ts?: string; message?: SlackMessage; messages?: SlackMessage[]; response_metadata?: { next_cursor?: string } }

function convertMessage(channelId: string, message: SlackMessage): Message {
  return { channelId, timestamp: message.ts ?? '', text: message.text ?? '', ...(message.thread_ts ? { threadTimestamp: message.thread_ts } : {}), ...(message.user ? { userId: message.user } : {}) };
}

function classifyResponse(operation: string, response: SlackExchange): Failure | undefined {
  const body = response.body as SlackBody;
  if (response.status >= 200 && response.status < 300 && body.ok) return undefined;
  let kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 404 ? 'NOT_FOUND' : 'PROVIDER_REJECTION';
  if (['invalid_auth', 'not_authed', 'token_expired', 'token_revoked', 'account_inactive'].includes(body.error ?? '')) kind = 'AUTHENTICATION';
  if (['missing_scope', 'no_permission'].includes(body.error ?? '')) kind = 'AUTHORIZATION';
  if (['channel_not_found', 'thread_not_found', 'message_not_found'].includes(body.error ?? '')) kind = 'NOT_FOUND';
  return failure(operation, kind, 'Slack rejected the request');
}

/** Classify a Slack query by status first, because Slack may send 429 or 5xx without a JSON body. */
function classifyQuery<Output>(client: SlackClient, call: Call, operation: string, response: SlackExchange, empty: Output, unavailable: string, limited: string): QueryAttempt<Output> | undefined {
  if (response.status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', limited), retryAfter(response));
  if (response.status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', unavailable), retryAfter(response));
  if (response.error === 'tooLarge') return newQueryBranch('invalidResponse', empty, client.receipt(call, response), failure(operation, 'RESPONSE_TOO_LARGE', 'Slack response exceeds configured size limit'));
  if (response.error === 'invalid') return newQueryBranch('invalidResponse', empty, client.receipt(call, response), failure(operation, 'PROTOCOL', 'Slack returned malformed JSON'));
  if (response.error) return newQueryRetry(failure(operation, 'AVAILABILITY', unavailable));
  const rejected = classifyResponse(operation, response);
  return rejected ? newQueryBranch('providerRejected', empty, client.receipt(call, response), rejected) : undefined;
}

function replyQuery(channelId: string, threadTimestamp: string, replyTimestamp: string): URLSearchParams {
  return new URLSearchParams({ channel: channelId, ts: threadTimestamp, oldest: replyTimestamp, latest: replyTimestamp, inclusive: 'true', limit: '1' });
}

async function postMessage(client: SlackClient, call: Call, operation: string, channelId: string, threadTimestamp: string, text: string): Promise<MutationAttempt<PostMessageOutput>> {
  if (!channelId.trim() || !text.trim() || [...text].length > client.maxMessageCharacters) return newMutationBranch('defect', emptyPost, {}, failure(operation, 'VALIDATION', 'channel and bounded non-empty text are required'));
  let fence;
  try { fence = newFence(call, 30_000); }
  catch { return newMutationBranch('defect', emptyPost, {}, failure(operation, 'LOCAL_DEFECT', 'Slack dispatch fence could not be constructed')); }
  const decision = fence.decide();
  if (decision.kind === 'failClosed') return newMutationUncertain(emptyPost, failure(operation, 'TRANSPORT', 'an earlier attempt may have posted the Slack message'), client.receipt(call));
  if (decision.kind === 'readBackRequired') return readBack(client, call, operation, channelId, threadTimestamp, decision.marker);
  const credential = await client.resolveToken(call, 'bot');
  if (!credential.token) {
    try { await fence.recordNotSent(); }
    catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack credentials and no-send checkpoint are unavailable')); }
    return credential.terminal ? newMutationBranch('defect', emptyPost, {}, failure(operation, 'AUTHENTICATION', 'Slack connection credentials are unavailable; reconnect the connection'))
      : newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack credentials are temporarily unavailable; no message was sent'));
  }
  const payload: Record<string, string> = { channel: channelId, text, client_msg_id: call.idempotencyKey ?? '' };
  if (threadTimestamp) payload.thread_ts = threadTimestamp;
  let response: SlackExchange;
  try { response = await fence.dispatch(signal => client.request('POST', 'chat.postMessage', credential.token!, undefined, payload, signal)); }
  catch (error) {
    if (error instanceof CheckpointError) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack dispatch claim could not be recorded'));
    if (error instanceof InsufficientTimeError) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack dispatch was not sent before the Step deadline'));
    return newMutationUncertain(emptyPost, failure(operation, 'TRANSPORT', 'Slack message outcome is unknown'), client.receipt(call));
  }
  if (response.status === 429 || response.status === 503) {
    if (!await canResend(fence)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack retry checkpoint could not be recorded'));
    return response.status === 429 ? newMutationRetry(failure(operation, 'RATE_LIMIT', 'Slack rate limited the message'), retryAfter(response)) : newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack message service is temporarily unavailable'), retryAfter(response));
  }
  const body = response.body as SlackBody | undefined;
  if (response.status === 401 || (!response.error && response.status >= 200 && response.status < 300 && !body?.ok && isCredentialRejected(response))) {
    const replacement = await client.refreshRejectedToken(call, 'bot', credential.token);
    if (!replacement.token) return newMutationBranch('providerRejected', emptyPost, client.receipt(call, response), failure(operation, 'AUTHENTICATION', replacement.terminal ? 'Slack rejected the connection credential; reconnect the connection' : 'Slack credential refresh is temporarily unavailable'));
    if (!await canResend(fence)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack retry checkpoint could not be recorded'));
    return newMutationRetry(failure(operation, 'AUTHENTICATION', 'Slack connection credential was refreshed after rejection'));
  }
  if (response.error) {
    if (response.error === 'notSent') {
      if (!await canResend(fence)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack retry checkpoint could not be recorded'));
      return newMutationRetry(failure(operation, 'TRANSPORT', 'Slack message request did not reach the provider'));
    }
    return newMutationUncertain(emptyPost, failure(operation, 'TRANSPORT', 'Slack message outcome is unknown'), client.receipt(call, response));
  }
  if (response.status === 409) return newMutationUncertain(emptyPost, failure(operation, 'PROTOCOL', 'Slack returned a conflict without a confirmable message identity'), client.receipt(call, response));
  if (!body?.ok) {
    if (response.status >= 200 && response.status < 300 && ['service_unavailable', 'ratelimited'].includes(body?.error ?? '')) {
      if (!await canResend(fence)) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack retry checkpoint could not be recorded'));
      return newMutationRetry(failure(operation, body?.error === 'ratelimited' ? 'RATE_LIMIT' : 'AVAILABILITY', 'Slack did not process the message'), retryAfter(response));
    }
    if (['internal_error', 'fatal_error'].includes(body?.error ?? '')) {
      const timestamp = body?.message?.ts || body?.ts || '';
      if (timestamp) {
        try { await fence.recordProviderId(timestamp); } catch { /* Read-back can still prove this attempt's result. */ }
        return readBack(client, call, operation, channelId, threadTimestamp, { callId: call.id, attempt: call.context.attempt, state: 'claimed', providerId: timestamp });
      }
      return newMutationUncertain(emptyPost, failure(operation, 'AVAILABILITY', 'Slack message outcome is unknown'), client.receipt(call, response));
    }
  }
  if (response.status >= 500) return newMutationUncertain(emptyPost, failure(operation, 'AVAILABILITY', 'Slack message outcome is unknown'), client.receipt(call, response));
  const rejected = classifyResponse(operation, response);
  if (rejected) return newMutationBranch('providerRejected', emptyPost, client.receipt(call, response), rejected);
  const message: SlackMessage = { ...body?.message };
  if (!message.ts && body?.ts) message.ts = body.ts;
  if (!message.channel && body?.channel) message.channel = body.channel;
  if (message.ts) { try { await fence.recordProviderId(message.ts); } catch { /* The accepted result remains authoritative. */ } }
  if (!message.ts || !message.channel) {
    if (message.ts) return readBack(client, call, operation, channelId, threadTimestamp, { callId: call.id, attempt: call.context.attempt, state: 'claimed', providerId: message.ts });
    return newMutationUncertain(emptyPost, failure(operation, 'PROTOCOL', 'Slack returned an invalid message response'), client.receipt(call, response));
  }
  return newMutationBranch('sent', { message: convertMessage(message.channel, message) }, client.receipt(call, response, message.ts));
}

async function canResend(fence: ReturnType<typeof newFence>): Promise<boolean> { try { await fence.recordCanResend(); return true; } catch { return false; } }

async function readBack(client: SlackClient, call: Call, operation: string, channelId: string, threadTimestamp: string, marker: Marker | undefined): Promise<MutationAttempt<PostMessageOutput>> {
  if (!marker || marker.callId !== call.id || !marker.providerId) return newMutationUncertain(emptyPost, failure(operation, 'TRANSPORT', 'an earlier Slack message cannot be identified'), client.receipt(call));
  const credential = await client.resolveToken(call, 'user');
  if (!credential.token) return credential.terminal ? newMutationUncertain(emptyPost, failure(operation, 'AUTHENTICATION', 'Slack message read-back credentials are unavailable; reconnect the connection to confirm the post'), client.receipt(call, undefined, marker.providerId))
    : newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack message read-back credentials are unavailable'));
  const query = replyQuery(channelId, threadTimestamp || marker.providerId, marker.providerId);
  const response = await client.get(call, credential.token, 'conversations.replies', query);
  if (response.error === 'invalid' || response.error === 'tooLarge') return newMutationUncertain(emptyPost, failure(operation, 'PROTOCOL', 'Slack message read-back returned an unusable response'), client.receipt(call, response, marker.providerId));
  if (response.error || response.status === 429 || response.status >= 500) return newMutationRetry(failure(operation, 'AVAILABILITY', 'Slack message read-back is temporarily unavailable'), retryAfter(response));
  const body = response.body as SlackBody;
  if (response.status < 200 || response.status >= 300 || !body.ok) return newMutationUncertain(emptyPost, failure(operation, 'PROTOCOL', 'Slack message read-back could not confirm the post'), client.receipt(call, response, marker.providerId));
  const match = (body.messages ?? []).find(message => message.ts === marker.providerId && (!threadTimestamp || message.thread_ts === threadTimestamp));
  return match ? newMutationBranch('sent', { message: convertMessage(channelId, match) }, client.receipt(call, response, marker.providerId))
    : newMutationUncertain(emptyPost, failure(operation, 'PROTOCOL', 'Slack message read-back did not find the post'), client.receipt(call, response, marker.providerId));
}
