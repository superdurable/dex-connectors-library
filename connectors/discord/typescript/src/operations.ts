// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CheckpointError, DispatchNotAllowedError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import type { DiscordClient, ExchangeFailure, FencedExchange } from './client.js';
import { AddReactionDefinition, CreateMessageDefinition, GetChannelDefinition, ListChannelMessagesDefinition } from './generated/definitions.js';
import type { AddReactionInput, AddedReaction, Channel, CreateMessageInput, GetChannelInput, ListChannelMessagesInput, Message, MessagePage } from './generated/models.js';

const snowflake = /^[0-9]{17,20}$/;
const customEmoji = /^[A-Za-z0-9_]{2,32}:[0-9]{17,20}$/;
const goWhitespace = /^\p{White_Space}+|\p{White_Space}+$/gu;
const zeroChannel: Channel = { id: '', type: 0 };
const zeroMessage: Message = { id: '', channelId: '', author: { id: '', username: '', isBot: false }, content: '', timestamp: '0001-01-01T00:00:00Z', type: 0 };
const zeroPage: MessagePage = { messages: [], isFull: false };
const zeroReaction: AddedReaction = { channelId: '', messageId: '', emoji: '' };
// Only these statuses prove Discord refused a send; 408, 409, 429, and other 4xx leave the outcome unknown.
const refusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

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
  /**
   * Validate, send under a single-dispatch fence, and decode one message. An unknown outcome resends
   * with the same enforced nonce while possible sends stay below the send cap and selects uncertain
   * at the cap. A definite refusal of the first send selects its branch and is never resent. A 2xx is never
   * resent: an unreadable answer selects uncertain, and an answer whose message ID is valid is checkpointed
   * and read back by that ID.
   */
  async invoke(call: Call, input: CreateMessageInput): Promise<MutationAttempt<Message>> {
    let body: Record<string, unknown>;
    try {
      validateSnowflake('channelId', input.channelId);
      if (!trimGoWhitespace(input.content)) throw new Error('content must not be blank');
      if ([...input.content].length > 2000 || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(input.content)) throw new Error('content must be valid UTF-8 of at most 2000 characters');
      if (!call.idempotencyKey) throw new Error('createMessage requires an idempotency key');
      body = { content: input.content, nonce: createNonce(call.idempotencyKey), enforce_nonce: true, allowed_mentions: { parse: [] } };
      if (input.replyToMessageId) {
        validateSnowflake('replyToMessageId', input.replyToMessageId);
        body.message_reference = { message_id: input.replyToMessageId, fail_if_not_exists: true };
      }
    } catch (error) { return mutationFailure('createMessage', 'defect', 'VALIDATION', zeroMessage, {}, error); }
    let encoded: Uint8Array;
    try { encoded = Buffer.from(JSON.stringify(body), 'utf8'); }
    catch { return mutationFailure('createMessage', 'defect', 'LOCAL_DEFECT', zeroMessage, {}, new Error('createMessage request could not be encoded')); }
    const operationMaxSends = definitionMaxSends(CreateMessageDefinition);
    let fence: Fence;
    try {
      fence = newFence(call, CreateMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: operationMaxSends });
    } catch { return mutationFailure('createMessage', 'defect', 'LOCAL_DEFECT', zeroMessage, {}, new Error('Discord dispatch fence could not be constructed')); }
    const receipt = this.client.receipt(call, input.channelId);
    const decision = fence.decide();
    // An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
    if (decision.kind === 'failClosed') return createMessageRetry('AVAILABILITY', 'Discord dispatch checkpoint is unreadable; no message was sent in this attempt');
    // A saved message ID proves Discord accepted an earlier send, so it is read back at any send count and never resent.
    if (decision.kind !== 'sendAllowed' && decision.marker?.providerId) return this.readBackSavedMessage(call, input.channelId, decision.marker.providerId);
    if (decision.kind === 'readBackRequired') return createMessageUncertain('TRANSPORT', 'an earlier attempt may have created the Discord message, and the send cap is reached', receipt);
    // Without a saved message ID, the resend with the same enforced nonce is Discord's only read-back.
    const priorSendUpperBound = decision.priorSendUpperBound ?? 0;
    // This attempt's send would be the last one the cap allows when no further send remains.
    const canResendUnknownOutcome = priorSendUpperBound + 1 < effectiveMaxSends(call, operationMaxSends);
    // An earlier send may have created the message, so a refusal of this resend proves nothing.
    const hasPossibleEarlierSend = priorSendUpperBound > 0;
    const resolution = await this.client.resolveBotToken(call, 'createMessage');
    if ('outcome' in resolution) return localCreateMessageFailure(fence, hasPossibleEarlierSend, resolution.outcome, receipt);
    const path = `/channels/${input.channelId}/messages`;
    let exchange: FencedExchange;
    try {
      exchange = await fence.dispatch(signal => this.client.sendFenced(path, resolution.token, encoded, signal));
    } catch (error) {
      // The Fence raises these before sending, including its AggregateError for a short window and a failed notSent.
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof DispatchNotAllowedError || isFenceInsufficientTimeAggregateError(error))
        return createMessageRetry('AVAILABILITY', 'Discord dispatch was not started');
      exchange = { status: 0, headers: {}, body: new Uint8Array(), error: 'request', requestSent: true };
    }
    if (exchange.status === 0) {
      if (!exchange.requestSent) {
        try { await fence.recordCanResend(); }
        catch { /* A lost canResend checkpoint leaves the claim, which the next attempt counts as a send. */ }
        return createMessageRetry('TRANSPORT', 'Discord could not be reached before sending');
      }
      return resendOrSelectUncertain(canResendUnknownOutcome, 'TRANSPORT', 'Discord message outcome is unknown', 0, receipt);
    }
    if (exchange.status < 200 || exchange.status >= 300)
      return this.classifyRefusal(fence, exchange, input.channelId, canResendUnknownOutcome, hasPossibleEarlierSend, receipt);
    // A 2xx proves Discord created the message, and nonce deduplication is unverified, so nothing below resends.
    if (exchange.error) return createMessageUncertain('TRANSPORT', 'Discord accepted the message, but its response could not be read', receipt);
    let message: Message;
    try { message = decodeMessage(decodeObject(exchange.body), input.channelId); }
    catch (error) { return this.readBackAcceptedMessage(call, fence, resolution.token, input.channelId, exchange.body, error, receipt); }
    return newMutationBranch('created', message, this.client.receipt(call, message.id));
  }

  /** Confirm a 2xx whose body is not a valid message: a valid top-level message ID is checkpointed and read back. */
  private async readBackAcceptedMessage(call: Call, fence: Fence, token: string, channelId: string, body: Uint8Array, decodeError: unknown, receipt: ReceiptDraft): Promise<MutationAttempt<Message>> {
    let messageId: unknown;
    try { messageId = decodeObject(body).id; } catch { messageId = undefined; }
    if (typeof messageId !== 'string' || !snowflake.test(messageId))
      return createMessageUncertain('PROTOCOL', `Discord accepted the message, but its response carries no message ID: ${decodeError instanceof Error ? decodeError.message : 'invalid response'}`, receipt);
    let isMessageIdSaved = true;
    try { await fence.recordProviderId(messageId); } catch { isMessageIdSaved = false; }
    return this.readBackMessage(call, token, channelId, messageId, isMessageIdSaved);
  }

  /** Read back the message ID an earlier attempt checkpointed. */
  private async readBackSavedMessage(call: Call, channelId: string, messageId: string): Promise<MutationAttempt<Message>> {
    const receipt = this.client.receipt(call, messageId);
    if (!snowflake.test(messageId)) return createMessageUncertain('PROTOCOL', 'an earlier attempt created the Discord message, but its checkpointed message ID is invalid', receipt);
    const resolution = await this.client.resolveBotToken(call, 'createMessage');
    if ('outcome' in resolution) {
      // The checkpoint keeps the saved message ID, so the next attempt reads it back again.
      if (resolution.outcome.disposition === 'retry') return newMutationRetry(resolution.outcome.failure, resolution.outcome.retryAfterMs);
      return createMessageUncertain(resolution.outcome.failure.kind, `an earlier attempt created the Discord message, but it cannot be read back: ${resolution.outcome.failure.message}`, receipt);
    }
    return this.readBackMessage(call, resolution.token, channelId, messageId, true);
  }

  /**
   * Read an accepted message by ID and select created when Discord returns it. An unknown result retries
   * only when the ID is checkpointed for the next attempt; a definite refusal, such as 404 or a missing
   * Read Message History permission, selects uncertain.
   */
  private async readBackMessage(call: Call, token: string, channelId: string, messageId: string, isMessageIdSaved: boolean): Promise<MutationAttempt<Message>> {
    const receipt = this.client.receipt(call, messageId);
    let failure: Failure = { kind: 'TRANSPORT', provider: 'discord', operation: 'createMessage', message: 'Discord accepted the message, but reading it back did not complete' };
    let retryAfterMs = 0;
    let exchange: FencedExchange;
    try { exchange = await this.client.readAcceptedMessage(`/channels/${channelId}/messages/${messageId}`, token, call.context.cancellationSignal); }
    catch { exchange = { status: 0, headers: {}, body: new Uint8Array(), error: 'request', requestSent: true }; }
    const isSuccess = exchange.status >= 200 && exchange.status < 300;
    if (isSuccess && !exchange.error) {
      let message: Message | undefined;
      try { message = decodeMessage(decodeObject(exchange.body), channelId); } catch { message = undefined; }
      if (message?.id === messageId) return newMutationBranch('created', message, receipt);
      failure = { ...failure, kind: 'PROTOCOL', message: 'Discord accepted the message, but its read-back answer is invalid' };
    } else if (exchange.status !== 0 && !isSuccess) {
      const classified = this.client.classifyErrorResponse('createMessage', { status: exchange.status, headers: exchange.headers, body: exchange.body }, receipt);
      failure = { ...classified.failure, message: `Discord accepted the message, but reading it back failed: ${classified.failure.message}` };
      retryAfterMs = classified.retryAfterMs;
      if (!exchange.error && refusalStatuses.has(exchange.status)) return newMutationUncertain(zeroMessage, failure, receipt);
    }
    if (isMessageIdSaved) return newMutationRetry(failure, retryAfterMs);
    return newMutationUncertain(zeroMessage, failure, receipt);
  }

  /**
   * Classify a non-2xx answer: 429 and 503 prove no processing, and only a readable allowlisted
   * refusal selects its branch; every other answer is an unknown outcome.
   */
  private async classifyRefusal(fence: Fence, exchange: FencedExchange, channelId: string, canResendUnknownOutcome: boolean,
    hasPossibleEarlierSend: boolean, receipt: ReceiptDraft): Promise<MutationAttempt<Message>> {
    const classified = this.client.classifyErrorResponse('createMessage', { status: exchange.status, headers: exchange.headers, body: exchange.body }, receipt);
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return createMessageRetry('AVAILABILITY', 'Discord retry checkpoint could not be recorded'); }
      return newMutationRetry(classified.failure, classified.retryAfterMs);
    }
    const isDefiniteRefusal = !exchange.error && refusalStatuses.has(exchange.status) &&
      (classified.disposition === 'notFound' || classified.disposition === 'rejected') && !isAcceptedMessageBody(exchange.body, channelId);
    // An unreadable, contradictory, or unlisted answer cannot prove that Discord created nothing.
    if (!isDefiniteRefusal) return resendOrSelectUncertain(canResendUnknownOutcome, classified.failure.kind, classified.failure.message, classified.retryAfterMs, receipt);
    if (hasPossibleEarlierSend) return createMessageUncertain(classified.failure.kind, `an earlier attempt may have created the Discord message: ${classified.failure.message}`, receipt);
    try { await fence.recordCanResend(); }
    catch { /* The branch is terminal, so a lost canResend checkpoint cannot cause a resend. */ }
    return newMutationBranch(classified.disposition === 'rejected' ? 'providerRejected' : 'notFound', zeroMessage, receipt, classified.failure);
  }
}

/**
 * Handle a failure before dispatch: a transient one retries, recording notSent only when no earlier
 * send may have created the message; a terminal one selects its branch before any possible send and
 * uncertain after one.
 */
async function localCreateMessageFailure(fence: Fence, hasPossibleEarlierSend: boolean, outcome: ExchangeFailure, receipt: ReceiptDraft): Promise<MutationAttempt<Message>> {
  if (outcome.disposition === 'retry') {
    if (!hasPossibleEarlierSend) {
      try { await fence.recordNotSent(); }
      catch { /* Without notSent the next attempt counts this attempt as a possible send. */ }
    }
    // After a possible send, notSent would hide that send, so nothing is recorded.
    return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  }
  if (hasPossibleEarlierSend) return createMessageUncertain(outcome.failure.kind, `an earlier attempt may have created the Discord message: ${outcome.failure.message}`, receipt);
  try { await fence.recordNotSent(); }
  catch { /* The branch is terminal, so a lost notSent checkpoint cannot cause a resend. */ }
  return newMutationBranch(outcome.disposition === 'rejected' ? 'providerRejected' : 'defect', zeroMessage, {}, outcome.failure);
}

/** Retry a possibly created message while the send cap allows another send. */
function resendOrSelectUncertain(canResendUnknownOutcome: boolean, kind: Failure['kind'], message: string, retryAfterMs: number, receipt: ReceiptDraft): MutationAttempt<Message> {
  // The claim stays the last marker, so the next attempt counts this send.
  if (canResendUnknownOutcome) return createMessageRetry(kind, message, retryAfterMs);
  return createMessageUncertain(kind, message, receipt);
}

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Report an error body that nevertheless describes a message in the channel. */
function isAcceptedMessageBody(body: Uint8Array, channelId: string): boolean {
  try { decodeMessage(decodeObject(body), channelId); return true; } catch { return false; }
}

/** Match only the Fence's pre-send AggregateError: too little time, and the notSent checkpoint failed. */
function isFenceInsufficientTimeAggregateError(error: unknown): boolean {
  return error instanceof AggregateError && error.errors.some(inner => inner instanceof InsufficientTimeError);
}

function createMessageRetry(kind: Failure['kind'], message: string, retryAfterMs = 0): MutationAttempt<Message> {
  return newMutationRetry({ kind, provider: 'discord', operation: 'createMessage', message }, retryAfterMs);
}

function createMessageUncertain(kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<Message> {
  return newMutationUncertain(zeroMessage, { kind, provider: 'discord', operation: 'createMessage', message }, receipt);
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
    if ('outcome' in result) return mutationOutcome(result.outcome, zeroReaction);
    return newMutationBranch('added', { channelId: input.channelId, messageId: input.messageId, emoji: input.emoji }, this.client.receipt(call, input.messageId));
  }
}

function validateSnowflake(name: string, value: string): void { if (!snowflake.test(value)) throw new Error(`${name} must be a Discord snowflake ID of 17 to 20 digits`); }
function trimGoWhitespace(value: string): string { return value.replace(goWhitespace, ''); }
function isUnicodeEmojiCandidate(value: string): boolean {
  if (!value || Buffer.byteLength(value, 'utf8') > 64 || trimGoWhitespace(value) !== value || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)) return false;
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
function mutationOutcome<T>(outcome: ExchangeFailure, zero: T): MutationAttempt<T> {
  // addReaction is declared safe, so an oversized answer retries instead of selecting invalidResponse.
  if (outcome.disposition === 'retry' || outcome.disposition === 'invalidResponse') return newMutationRetry(outcome.failure, outcome.retryAfterMs);
  return newMutationBranch(outcome.disposition === 'rejected' ? 'providerRejected' : outcome.disposition, zero, outcome.receipt, outcome.failure);
}
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): QueryAttempt<T> {
  return newQueryBranch(branch, zero, receipt, { kind, provider: 'discord', operation, message: error instanceof Error ? error.message : 'Discord response is invalid' });
}
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], zero: T, receipt: ReceiptDraft, error?: unknown): MutationAttempt<T> {
  return newMutationBranch(branch, zero, receipt, { kind, provider: 'discord', operation, message: error instanceof Error ? error.message : 'Discord response is invalid' });
}
