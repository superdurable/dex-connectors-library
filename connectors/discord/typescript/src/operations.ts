// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { DiscordClient, ExchangeFailure } from './client.js';
import { AddReactionDefinition, CreateMessageDefinition, GetChannelDefinition, ListChannelMessagesDefinition } from './generated/definitions.js';
import type { AddReactionInput, AddedReaction, Channel, CreateMessageInput, GetChannelInput, ListChannelMessagesInput, Message, MessagePage } from './generated/models.js';

const snowflake = /^[0-9]{17,20}$/;
const customEmoji = /^[A-Za-z0-9_]{2,32}:[0-9]{17,20}$/;
const zeroChannel: Channel = { id: '', type: 0 };
const zeroMessage: Message = { id: '', channelId: '', author: { id: '', username: '', isBot: false }, content: '', timestamp: '0001-01-01T00:00:00Z', type: 0 };
const zeroPage: MessagePage = { messages: [], isFull: false };
const zeroReaction: AddedReaction = { channelId: '', messageId: '', emoji: '' };

/** Read one Discord channel. */
export class GetChannelOperation implements Query<GetChannelInput, Channel> {
  /** Bind a client. */ constructor(private readonly client: DiscordClient) {}
  /** Return the manifest definition. */ definition() { return GetChannelDefinition; }
  /** Validate and read the channel. */
  async invoke(call: Call, input: GetChannelInput): Promise<QueryAttempt<Channel>> {
    try { validateSnowflake('channelId', input.channelId); }
    catch (error) { return queryFailure('getChannel', 'defect', 'VALIDATION', zeroChannel, {}, error); }
    const result = await this.client.exchange(call, 'getChannel', 'GET', `/channels/${input.channelId}`, input.channelId);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroChannel);
    try {
      const body = decodeObject(result.response.body);
      if (body.id !== input.channelId || !Number.isInteger(body.type)) throw new Error('invalid channel');
      const channel: Channel = { id: input.channelId, type: body.type as number };
      for (const [source, target] of [['guild_id', 'guildId'], ['name', 'name'], ['topic', 'topic'], ['last_message_id', 'lastMessageId']] as const) {
        if (body[source] !== undefined && body[source] !== null) {
          if (typeof body[source] !== 'string') throw new Error('invalid channel field');
          if ((source === 'guild_id' || source === 'last_message_id') && body[source] !== '' && !snowflake.test(body[source])) throw new Error('invalid channel ID');
          if (body[source] !== '') Object.assign(channel, { [target]: body[source] });
        }
      }
      return newQueryBranch('found', channel, this.client.receipt(call, input.channelId));
    } catch { return queryFailure('getChannel', 'invalidResponse', 'PROTOCOL', zeroChannel, this.client.receipt(call, input.channelId)); }
  }
}

/** Read one bounded message page. */
export class ListChannelMessagesOperation implements Query<ListChannelMessagesInput, MessagePage> {
  /** Bind a client. */ constructor(private readonly client: DiscordClient) {}
  /** Return the manifest definition. */ definition() { return ListChannelMessagesDefinition; }
  /** Validate, request, and decode one page. */
  async invoke(call: Call, input: ListChannelMessagesInput): Promise<QueryAttempt<MessagePage>> {
    let limit: number;
    let query: string;
    try {
      validateSnowflake('channelId', input.channelId);
      limit = input.limit || 50;
      if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('limit must be from 1 through 100, or zero for Discord\'s default of 50');
      if (input.beforeMessageId && input.afterMessageId) throw new Error('set at most one of beforeMessageId and afterMessageId');
      if (input.beforeMessageId) validateSnowflake('beforeMessageId', input.beforeMessageId);
      if (input.afterMessageId) validateSnowflake('afterMessageId', input.afterMessageId);
      const parameters = new URLSearchParams();
      if (input.afterMessageId) parameters.set('after', input.afterMessageId);
      if (input.beforeMessageId) parameters.set('before', input.beforeMessageId);
      parameters.set('limit', String(limit));
      query = parameters.toString();
    } catch (error) { return queryFailure('listChannelMessages', 'defect', 'VALIDATION', zeroPage, {}, error); }
    const result = await this.client.exchange(call, 'listChannelMessages', 'GET', `/channels/${input.channelId}/messages?${query}`, input.channelId);
    if ('outcome' in result) return queryOutcome(result.outcome, zeroPage);
    try {
      const body: unknown = JSON.parse(Buffer.from(result.response.body).toString('utf8'));
      if (!Array.isArray(body) || body.length > limit) throw new Error('invalid message page');
      const messages = body.map(value => decodeMessage(value, input.channelId));
      const page: MessagePage = { messages, isFull: messages.length === limit,
        ...(messages.length ? { oldestMessageId: messages[messages.length - 1]!.id } : {}) };
      return newQueryBranch('listed', page, this.client.receipt(call, input.channelId));
    } catch { return queryFailure('listChannelMessages', 'invalidResponse', 'PROTOCOL', zeroPage, this.client.receipt(call, input.channelId)); }
  }
}

/** Send one nonce-deduplicated Discord message. */
export class CreateMessageOperation implements Mutation<CreateMessageInput, Message> {
  /** Bind a client. */ constructor(private readonly client: DiscordClient) {}
  /** Return the manifest definition. */ definition() { return CreateMessageDefinition; }
  /** Use the stable Dex Call ID as the idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and decode one message. */
  async invoke(call: Call, input: CreateMessageInput): Promise<MutationAttempt<Message>> {
    let body: Record<string, unknown>;
    try {
      validateSnowflake('channelId', input.channelId);
      if (!input.content.trim()) throw new Error('content must not be blank');
      if ([...input.content].length > 2000 || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(input.content)) throw new Error('content must be valid UTF-8 of at most 2000 characters');
      if (!call.idempotencyKey) throw new Error('createMessage requires an idempotency key');
      body = { content: input.content, nonce: createNonce(call.idempotencyKey), enforce_nonce: true, allowed_mentions: { parse: [] } };
      if (input.replyToMessageId) {
        validateSnowflake('replyToMessageId', input.replyToMessageId);
        body.message_reference = { message_id: input.replyToMessageId, fail_if_not_exists: true };
      }
    } catch (error) { return mutationFailure('createMessage', 'defect', 'VALIDATION', zeroMessage, {}, error); }
    const result = await this.client.exchange(call, 'createMessage', 'POST', `/channels/${input.channelId}/messages`, input.channelId, body);
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroMessage);
    try {
      const message = decodeMessage(decodeObject(result.response.body), input.channelId);
      return newMutationBranch('created', message, this.client.receipt(call, message.id));
    } catch { return mutationFailure('createMessage', 'invalidResponse', 'PROTOCOL', zeroMessage, this.client.receipt(call, input.channelId)); }
  }
}

/** Add the bot's repeat-safe reaction to a message. */
export class AddReactionOperation implements Mutation<AddReactionInput, AddedReaction> {
  /** Bind a client. */ constructor(private readonly client: DiscordClient) {}
  /** Return the manifest definition. */ definition() { return AddReactionDefinition; }
  /** Use the stable Dex Call ID; Discord's PUT itself is repeat-safe. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and add one reaction. */
  async invoke(call: Call, input: AddReactionInput): Promise<MutationAttempt<AddedReaction>> {
    try {
      validateSnowflake('channelId', input.channelId);
      validateSnowflake('messageId', input.messageId);
      if (!customEmoji.test(input.emoji) && !isUnicodeEmojiCandidate(input.emoji)) throw new Error('emoji must be a Unicode emoji or a custom emoji as name:id');
    } catch (error) { return mutationFailure('addReaction', 'defect', 'VALIDATION', zeroReaction, {}, error); }
    const result = await this.client.exchange(call, 'addReaction', 'PUT', `/channels/${input.channelId}/messages/${input.messageId}/reactions/${pathEscape(input.emoji)}/@me`, input.messageId);
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroReaction, false);
    return newMutationBranch('added', { channelId: input.channelId, messageId: input.messageId, emoji: input.emoji }, this.client.receipt(call, input.messageId));
  }
}

function validateSnowflake(name: string, value: string): void { if (!snowflake.test(value)) throw new Error(`${name} must be a Discord snowflake ID of 17 to 20 digits`); }
function isUnicodeEmojiCandidate(value: string): boolean {
  if (!value || Buffer.byteLength(value, 'utf8') > 64 || value.trim() !== value || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)) return false;
  return [...value].every(character => !/[\p{Cc}\p{Zs}\p{Zl}\p{Zp}]/u.test(character) && (character.codePointAt(0)! >= 128 || /[0-9#*]/.test(character)));
}
function pathEscape(value: string): string { return encodeURIComponent(value).replace(/%3A/gi, ':').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function createNonce(key: string): string { return createHash('sha256').update(key).digest('base64url').slice(0, 25); }
function decodeObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('response is not an object');
  return value as Record<string, unknown>;
}
function decodeMessage(value: unknown, channelId: string): Message {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid message');
  const body = value as Record<string, unknown>;
  if (typeof body.id !== 'string' || !snowflake.test(body.id) || body.channel_id !== channelId || typeof body.content !== 'string' || !Number.isInteger(body.type)) throw new Error('invalid message');
  if (!body.author || typeof body.author !== 'object' || Array.isArray(body.author)) throw new Error('invalid author');
  const author = body.author as Record<string, unknown>;
  if (typeof author.id !== 'string' || !snowflake.test(author.id)) throw new Error('invalid author ID');
  if (author.username !== undefined && typeof author.username !== 'string') throw new Error('invalid author username');
  if (author.bot !== undefined && typeof author.bot !== 'boolean') throw new Error('invalid author bot flag');
  const timestamp = decodeTime(body.timestamp);
  const editedTimestamp = body.edited_timestamp == null ? undefined : decodeTime(body.edited_timestamp);
  return { id: body.id, channelId, author: { id: author.id, username: (author.username as string | undefined) ?? '', isBot: (author.bot as boolean | undefined) ?? false },
    content: body.content, timestamp, type: body.type as number, ...(editedTimestamp ? { editedTimestamp } : {}) };
}
function decodeTime(value: unknown): string {
  if (typeof value !== 'string' || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(value)) throw new Error('invalid timestamp');
  const parts = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.(\d+))?(Z|[+-](\d\d):(\d\d))$/.exec(value);
  if (!parts) throw new Error('invalid timestamp');
  const [, year, month, day, hour, minute, second, fraction, , offsetHour, offsetMinute] = parts;
  const yearNumber = Number(year), monthNumber = Number(month), dayNumber = Number(day);
  const daysInMonth = new Date(Date.UTC(yearNumber, monthNumber, 0)).getUTCDate();
  if (monthNumber < 1 || monthNumber > 12 || dayNumber < 1 || dayNumber > daysInMonth || Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59 || Number(offsetHour ?? 0) > 23 || Number(offsetMinute ?? 0) > 59) throw new Error('invalid timestamp');
  const fractional = fraction ?? '';
  if (fractional.length > 9) throw new Error('invalid timestamp');
  const secondsOnly = fractional ? value.replace(`.${fractional}`, '') : value;
  const date = new Date(secondsOnly);
  if (Number.isNaN(date.getTime())) throw new Error('invalid timestamp');
  const utcSeconds = date.toISOString().replace(/\.000Z$/, '');
  const trimmedFraction = fractional.replace(/0+$/, '');
  return `${utcSeconds}${trimmedFraction ? `.${trimmedFraction}` : ''}Z`;
}
function queryOutcome<T>(outcome: ExchangeFailure, zero: T): QueryAttempt<T> {
  if (outcome.disposition === 'retry') return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.disposition === 'rejected' ? 'providerRejected' : outcome.disposition, zero, outcome.receipt, outcome.failure);
}
function mutationOutcome<T>(outcome: ExchangeFailure, zero: T, hasInvalidResponse = true): MutationAttempt<T> {
  if (outcome.disposition === 'retry') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  const branch = outcome.disposition === 'rejected' || (!hasInvalidResponse && outcome.disposition === 'invalidResponse') ? 'providerRejected' : outcome.disposition;
  return newMutationBranch(branch, zero, outcome.receipt, outcome.failure);
}
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): QueryAttempt<T> {
  return newQueryBranch(branch, zero, receipt, { kind, provider: 'discord', operation, message: error instanceof Error ? error.message : 'Discord response is invalid' });
}
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): MutationAttempt<T> {
  return newMutationBranch(branch, zero, receipt, { kind, provider: 'discord', operation, message: error instanceof Error ? error.message : 'Discord response is invalid' });
}
