// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  ReauthorizationRequiredError, definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain,
  newQueryBranch, newQueryRetry, resolveCredentialAfterRejection, zeroReceipt,
  type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt,
  type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition,
} from '@superdurable/connectors-sdk';
import { CheckpointError, Fence, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import {
  ListThreadRepliesDefinition, PostChannelMessageDefinition, PostChatMessageDefinition,
  PostThreadReplyDefinition,
} from './generated/definitions.js';
import {
  ListThreadRepliesInputSchema, PostChannelMessageInputSchema, PostChatMessageInputSchema,
  PostThreadReplyInputSchema,
  type Credentials, type ListThreadRepliesInput, type ListThreadRepliesOutput, type Message,
  type PostChannelMessageInput, type PostChatMessageInput, type PostMessageOutput,
  type PostThreadReplyInput,
} from './generated/models.js';
import type { Exchange, TeamsClient } from './client.js';
import {
  buildContent, channelMessagesPath, comparisonText, decodeMessage, extractMessageText, parseGraphTime,
  postedOutput, threadRepliesPath, validateChatID, validateMessageID, validateTeamChannel,
} from './message.js';

const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', { fatal: true });
const messageID = /^[0-9]{1,20}$/;
// Only these statuses prove Graph refused a write; every other status is an unknown outcome.
const conclusiveRejectionStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
const emptyPost = (): PostMessageOutput => ({ createdAt: '0001-01-01T00:00:00Z', sender: {} });
const emptyReplies = (): ListThreadRepliesOutput => ({ replies: [] });

function statusKind(status: number): FailureKind {
  switch (status) {
    case 400: case 413: case 422: return 'VALIDATION';
    case 401: return 'AUTHENTICATION';
    case 403: return 'AUTHORIZATION';
    case 404: case 410: return 'NOT_FOUND';
    case 409: case 412: return 'CONFLICT';
    case 429: return 'RATE_LIMIT';
    default: return 'PROVIDER_REJECTION';
  }
}
function rejection(client: TeamsClient, operation: string, subject: string, exchange: Exchange): Failure {
  let message = `Microsoft Graph rejected the ${subject} with HTTP ${exchange.status}${client.errorCodeSuffix(exchange)}`;
  if (exchange.status === 401) message += '; reconnect the connection';
  else if (exchange.status === 403) message += '; the connected account must belong to the team, channel, or chat, and the app needs every requested Graph permission, with administrator consent for ChannelMessage.Read.All';
  return client.failure(operation, statusKind(exchange.status), message);
}
interface Classification { kind: string; failure?: Failure; retryAfterMs: number }
function classifyRead(client: TeamsClient, operation: string, subject: string, result: Exchange): Classification {
  if (result.error) return { kind: 'retry', failure: client.failure(operation, 'TRANSPORT', `Microsoft Graph ${subject} is temporarily unavailable`), retryAfterMs: 0 };
  const status = result.status;
  if (status === 404) return { kind: 'notFound', failure: rejection(client, operation, subject, result), retryAfterMs: 0 };
  if (status === 429) return { kind: 'retry', failure: client.failure(operation, 'RATE_LIMIT', `Microsoft Graph throttled the ${subject}`), retryAfterMs: client.retryAfter(result) };
  if (status === 408 || status >= 500 && status !== 501) return { kind: 'retry', failure: client.failure(operation, 'AVAILABILITY', `Microsoft Graph ${subject} is temporarily unavailable (HTTP ${status}${client.errorCodeSuffix(result)})`), retryAfterMs: client.retryAfter(result) };
  if (status >= 400) return { kind: 'providerRejected', failure: rejection(client, operation, subject, result), retryAfterMs: 0 };
  if (status < 200 || status >= 300) return { kind: 'invalidResponse', failure: client.failure(operation, 'PROTOCOL', `Microsoft Graph returned unexpected HTTP ${status} for the ${subject}`), retryAfterMs: 0 };
  if (result.bodyReadFailed) return { kind: 'retry', failure: client.failure(operation, 'TRANSPORT', `Microsoft Graph ${subject} response was interrupted`), retryAfterMs: 0 };
  if (result.bodyTooLarge) return { kind: 'invalidResponse', failure: client.failure(operation, 'RESPONSE_TOO_LARGE', `Microsoft Graph ${subject} response exceeds the configured size limit`), retryAfterMs: 0 };
  if (result.reflectsSecret) return { kind: 'invalidResponse', failure: client.failure(operation, 'PROTOCOL', `Microsoft Graph ${subject} response contains credential material`), retryAfterMs: 0 };
  return { kind: 'read', retryAfterMs: 0 };
}
function classifyWrite(client: TeamsClient, operation: string, subject: string, result: Exchange): Classification {
  if (result.error && !result.dispatched) return { kind: 'notApplied', failure: client.failure(operation, 'TRANSPORT', `Microsoft Graph could not be reached, so no ${subject} was sent`), retryAfterMs: 0 };
  if (result.error) return { kind: 'ambiguous', failure: client.failure(operation, 'TRANSPORT', `Microsoft Graph ${subject} outcome is unknown`), retryAfterMs: 0 };
  const status = result.status;
  if (status >= 200 && status < 300) {
    if (result.bodyReadFailed) return { kind: 'ambiguous', failure: client.failure(operation, 'TRANSPORT', `Microsoft Graph accepted the ${subject} but its response was interrupted`), retryAfterMs: 0 };
    if (result.bodyTooLarge) return { kind: 'ambiguous', failure: client.failure(operation, 'RESPONSE_TOO_LARGE', `Microsoft Graph accepted the ${subject} but its response exceeds the configured size limit`), retryAfterMs: 0 };
    if (result.reflectsSecret) return { kind: 'ambiguous', failure: client.failure(operation, 'PROTOCOL', `Microsoft Graph accepted the ${subject} but its response contains credential material`), retryAfterMs: 0 };
    return { kind: 'accepted', retryAfterMs: 0 };
  }
  if (status === 429) return { kind: 'notApplied', failure: client.failure(operation, 'RATE_LIMIT', `Microsoft Graph throttled the ${subject} before storing it`), retryAfterMs: client.retryAfter(result) };
  if (status === 503) return { kind: 'notApplied', failure: client.failure(operation, 'AVAILABILITY', `Microsoft Graph could not accept the ${subject} while unavailable (HTTP 503)`), retryAfterMs: client.retryAfter(result) };
  if (conclusiveRejectionStatuses.has(status)) return { kind: 'rejected', failure: rejection(client, operation, subject, result), retryAfterMs: 0 };
  return { kind: 'ambiguous', failure: client.failure(operation, 'AVAILABILITY', `Microsoft Graph ${subject} outcome is unknown after HTTP ${status}${client.errorCodeSuffix(result)}`), retryAfterMs: client.retryAfter(result) };
}
function decodeJSON(body: Uint8Array): unknown { return JSON.parse(decoder.decode(body)) as unknown; }
function sameThreadLink(client: TeamsClient, link: string, path: string): boolean {
  if (Buffer.byteLength(link) > 4096 || !/^[!-~]+$/.test(link)) return false;
  try {
    const parsed = new URL(link), endpoint = new URL(client.endpoint);
    return parsed.protocol === endpoint.protocol && parsed.host.toLowerCase() === endpoint.host.toLowerCase() &&
      !parsed.username && !parsed.password && !parsed.hash && decodeURIComponent(parsed.pathname).toLowerCase() === (endpoint.pathname + path).toLowerCase();
  } catch { return false; }
}

/** Read one bounded page of Microsoft Teams thread replies. */
export class ListThreadRepliesOperation implements Query<ListThreadRepliesInput, ListThreadRepliesOutput> {
  constructor(readonly client: TeamsClient) { if (!client) throw new TypeError('Teams client is required'); }
  /** Return the generated operation definition. */
  definition(): QueryDefinition { return ListThreadRepliesDefinition; }
  /** Validate and read one Graph reply page. */
  async invoke(call: Call, input: ListThreadRepliesInput): Promise<QueryAttempt<ListThreadRepliesOutput>> {
    const operation = 'listThreadReplies'; const output = emptyReplies();
    try {
      ListThreadRepliesInputSchema.parse(input);
      const team = input.teamId.trim(), channel = input.channelId.trim(), message = input.messageId.trim();
      validateTeamChannel(team, channel); validateMessageID('messageId', message);
      const pageSize = input.pageSize === undefined || input.pageSize === 0 ? 20 : input.pageSize;
      const maximum = input.maxTextCharacters === undefined || input.maxTextCharacters === 0 ? 4000 : input.maxTextCharacters;
      if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 50) throw new TypeError('pageSize must be from 1 to 50');
      if (!Number.isSafeInteger(maximum) || maximum < 1 || maximum > 100_000) throw new TypeError('maxTextCharacters must be from 1 to 100000');
      const path = threadRepliesPath(team, channel, message);
      if (input.cursor && !sameThreadLink(this.client, input.cursor, path)) throw new TypeError('cursor must be a nextCursor returned for the same thread');
      let credentials: Credentials;
      try { credentials = await this.client.resolveCredentials(call); }
      catch (error) {
        const reauthorization = error instanceof ReauthorizationRequiredError;
        const failure = this.client.failure(operation, 'AUTHENTICATION', reauthorization ?
          'Microsoft Teams connection credentials are unavailable; reconnect the connection' :
          'Microsoft Teams connection credentials are temporarily unavailable');
        return reauthorization ? newQueryBranch('defect', output, zeroReceipt(), failure) : newQueryRetry(failure);
      }
      const { exchange } = await this.client.exchange(call, credentials, 'GET', path,
        input.cursor ? { absoluteURL: input.cursor } : { query: { '$top': String(pageSize) } });
      const classification = classifyRead(this.client, operation, 'thread replies', exchange);
      const receipt = this.client.receipt(call, exchange, message);
      if (classification.kind === 'retry') return newQueryRetry(classification.failure!, classification.retryAfterMs);
      if (classification.kind !== 'read') return newQueryBranch(classification.kind, output, receipt, classification.failure);
      try {
        const decoded = decodeJSON(exchange.body);
        if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) throw new TypeError('the body is not a chatMessage collection');
        const collection = decoded as Record<string, unknown>;
        if (!Array.isArray(collection.value)) throw new TypeError('the body is not a chatMessage collection');
        const replies = collection.value.map((value: unknown, index: number) => {
          try { return decodeMessage(value, maximum); }
          catch (error) { throw new TypeError(`reply ${index}: ${error instanceof Error ? error.message : 'invalid message'}`); }
        });
        const next = collection['@odata.nextLink'] ?? '';
        if (typeof next !== 'string' || next && !sameThreadLink(this.client, next, path)) throw new TypeError('the next-page link does not name this thread on the Graph endpoint');
        return newQueryBranch('read', next ? { replies, nextCursor: next } : { replies }, receipt);
      } catch (error) {
        const message = error instanceof Error ? error.message : 'invalid JSON';
        return newQueryBranch('invalidResponse', output, receipt, this.client.failure(operation, 'PROTOCOL', `Microsoft Graph returned an invalid reply page: ${message}`));
      }
    } catch (error) {
      return newQueryBranch('defect', output, zeroReceipt(), this.client.failure(operation, 'VALIDATION', error instanceof Error ? error.message : 'invalid input'));
    }
  }
}

interface Post {
  operation: string; subject: string; path: string; readBackPath: string;
  replyToID: string; body: Record<string, unknown>; comparison: string; requested: PostMessageOutput;
}
/** Shared single-dispatch implementation for non-idempotent Graph posts. */
abstract class PostOperation<Input> implements Mutation<Input, PostMessageOutput> {
  constructor(readonly client: TeamsClient) { if (!client) throw new TypeError('Teams client is required'); }
  abstract definition(): MutationDefinition;
  /** The call ID correlates receipts but is never sent as a Graph idempotency key. */
  idempotencyKey(callID: string): string { return callID; }
  abstract invoke(call: Call, input: Input): Promise<MutationAttempt<PostMessageOutput>>;

  /**
   * Resend an unknown outcome while possible sends stay below the send cap. A post that can be read
   * back is looked for before each resend and at the cap; otherwise uncertain is selected at the cap.
   */
  protected async send(call: Call, post: Post): Promise<MutationAttempt<PostMessageOutput>> {
    const client = this.client;
    const operationMaxSends = definitionMaxSends(this.definition());
    let fence: Fence;
    try { fence = new Fence(call, 30_000, { maxSends: operationMaxSends }); }
    catch { return newMutationBranch('defect', post.requested, zeroReceipt(), client.failure(post.operation, 'LOCAL_DEFECT', 'Microsoft Teams dispatch fence could not be constructed')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationRetry(client.failure(post.operation, 'AVAILABILITY', 'the Microsoft Teams dispatch checkpoint is unreadable; no message was sent in this attempt'));
    const priorSendUpperBound = decision.priorSendUpperBound ?? 0;
    // This attempt's send is the last one the cap allows when no further send remains.
    const canResendUnknownOutcome = priorSendUpperBound + 1 < effectiveMaxSends(call, operationMaxSends);
    // An earlier send may have posted, so a refusal or local failure now cannot prove nothing changed.
    const hasPossibleEarlierSend = priorSendUpperBound > 0;
    let credentials: Credentials;
    try { credentials = await client.resolveCredentials(call); }
    catch (error) {
      const reauthorization = error instanceof ReauthorizationRequiredError;
      let failure = client.failure(post.operation, 'AUTHENTICATION', reauthorization ?
        'Microsoft Teams connection credentials are unavailable; reconnect the connection' :
        'Microsoft Teams connection credentials are temporarily unavailable');
      if (decision.kind === 'sendAllowed') {
        try { await fence.recordNotSent(); }
        catch { failure = { ...failure, message: failure.message + '; the not-sent checkpoint also failed' }; }
      }
      if (!reauthorization) return newMutationRetry(failure);
      if (hasPossibleEarlierSend) return newMutationUncertain(post.requested, failure, zeroReceipt());
      return newMutationBranch('defect', post.requested, zeroReceipt(), failure);
    }
    const savedMessageID = decision.marker?.providerId ?? '';
    if (decision.kind === 'readBackRequired') return (await this.reconcile(call, credentials, post, savedMessageID, false)).reconciled;
    // A saved message ID proves Graph stored an earlier post, so it is read back and never resent.
    if (decision.kind === 'readBackThenResendAllowed' && (post.readBackPath || savedMessageID)) {
      const { reconciled, shouldResend } = await this.reconcile(call, credentials, post, savedMessageID, true);
      if (!shouldResend) return reconciled;
    }
    const body = encoder.encode(JSON.stringify(post.body).replace(/[<>&\u2028\u2029]/g, character =>
      `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`));
    let exchange: Exchange;
    try {
      ({ exchange, credentials } = await fence.dispatch(signal => client.exchange(call, credentials, 'POST', post.path,
        { payload: body, retryRejected: false, signal })));
    } catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(client.failure(post.operation, 'AVAILABILITY', `the ${post.subject} was not sent before the dispatch deadline`));
      return resendOrSelectUncertain(post, canResendUnknownOutcome, client.failure(post.operation, 'TRANSPORT', 'Microsoft Teams message dispatch outcome is unknown'), 0, zeroReceipt());
    }
    if (exchange.status === 401 && 'resolveAfterRejection' in client.credentials) {
      try {
        const replacement = await resolveCredentialAfterRejection(client.credentials, call, client.refreshDriver, call.context.cancellationSignal);
        if (replacement.accessToken.reveal() !== credentials.accessToken.reveal()) {
          let failure = client.failure(post.operation, 'AUTHENTICATION', 'Microsoft Graph rejected the access token; the refreshed credential will be used on the next attempt');
          try { await fence.recordCanResend(); }
          catch { failure = { ...failure, message: failure.message + '; the safe-resend checkpoint failed' }; }
          return newMutationRetry(failure);
        }
      } catch { /* Keep Graph's conclusive 401 when refresh did not produce a new token. */ }
    }
    const classification = classifyWrite(client, post.operation, post.subject, exchange);
    const receipt = client.receipt(call, exchange);
    if (classification.kind === 'accepted') {
      let isMessageIDSaved = false;
      try {
        const resource = decodeJSON(exchange.body);
        if (resource && typeof resource === 'object' && 'id' in resource && typeof resource.id === 'string' && messageID.test(resource.id)) {
          try { await fence.recordProviderId(resource.id); isMessageIDSaved = true; }
          catch { /* A lost read-back checkpoint still counts this send against the cap. */ }
        }
        const message = decodeMessage(resource);
        return newMutationBranch('sent', postedOutput(post.requested, message, false), client.receipt(call, exchange, message.id));
      } catch {
        // Graph stored this message, and a resend would duplicate it; it cannot be read back.
        if (isMessageIDSaved && !post.readBackPath) return newMutationUncertain(post.requested, client.failure(post.operation, 'PROTOCOL', `Microsoft Graph stored the ${post.subject} but returned an unusable message, and the connection cannot read it back`), receipt);
        classification.failure = client.failure(post.operation, 'PROTOCOL', `Microsoft Graph accepted the ${post.subject} but returned an unusable message`);
      }
    } else if (classification.kind === 'notApplied') {
      let failure = classification.failure!;
      try { await fence.recordCanResend(); }
      catch { failure = { ...failure, message: failure.message + '; the safe-resend checkpoint failed' }; }
      return newMutationRetry(failure, classification.retryAfterMs);
    } else if (classification.kind === 'rejected') {
      // A message ID in a refusal contradicts it, so the outcome is unknown.
      if (carriesMessageID(exchange.body)) return resendOrSelectUncertain(post, canResendUnknownOutcome, client.failure(post.operation, 'PROTOCOL', `Microsoft Graph refused the ${post.subject} but returned a message ID`), classification.retryAfterMs, receipt);
      if (hasPossibleEarlierSend) return newMutationUncertain(post.requested, { ...classification.failure!, message: `${classification.failure!.message}; an earlier attempt of this Step may have sent the ${post.subject}` }, receipt);
      try { await fence.recordCanResend(); }
      catch { /* The refusal ends the Step, so a lost checkpoint cannot cause a resend. */ }
      return newMutationBranch('providerRejected', post.requested, receipt, classification.failure);
    } else if (classification.kind === 'defect') {
      if (hasPossibleEarlierSend) return newMutationUncertain(post.requested, classification.failure!, receipt);
      return newMutationBranch('defect', post.requested, receipt, classification.failure);
    }
    return resendOrSelectUncertain(post, canResendUnknownOutcome, classification.failure!, classification.retryAfterMs, receipt);
  }

  /**
   * Read the provider only. When `isResendAllowed`, a definite absence or an inconclusive listing
   * returns `shouldResend` instead of a final uncertain.
   */
  private async reconcile(call: Call, credentials: Credentials, post: Post, providerID: string, isResendAllowed: boolean): Promise<{ reconciled: MutationAttempt<PostMessageOutput>; shouldResend: boolean }> {
    const client = this.client;
    const final = (reconciled: MutationAttempt<PostMessageOutput>, shouldResend = false) => ({ reconciled, shouldResend });
    if (!post.readBackPath) return final(newMutationUncertain(post.requested, client.failure(post.operation, 'TRANSPORT', `an earlier attempt of this Step may have sent the ${post.subject}, and the connection cannot read it back, so it is not sent again`), client.receipt(call, { status: 0, headers: {}, body: new Uint8Array(), dispatched: false })));
    if (providerID && !messageID.test(providerID)) return final(newMutationUncertain(post.requested, client.failure(post.operation, 'PROTOCOL', 'the earlier Microsoft Teams message has an invalid provider ID'), zeroReceipt()));
    const { exchange } = await client.exchange(call, credentials, 'GET', post.readBackPath + (providerID ? `/${providerID}` : ''), providerID ? {} : { query: { '$top': '50' } });
    const classification = classifyRead(client, post.operation, `${post.subject} read-back`, exchange);
    const receipt = client.receipt(call, exchange, providerID);
    if (classification.kind === 'retry') return final(newMutationRetry(classification.failure!, classification.retryAfterMs));
    if (providerID) {
      if (classification.kind === 'read') {
        try { const resource = decodeJSON(exchange.body); if (resource && typeof resource === 'object' && 'id' in resource && resource.id === providerID) return final(newMutationBranch('sent', postedOutput(post.requested, decodeMessage(resource), true), receipt)); }
        catch { /* Inconclusive read-back keeps the write uncertain. */ }
      }
      return final(newMutationUncertain(post.requested, client.failure(post.operation, 'PROTOCOL', 'the earlier Microsoft Teams message could not be confirmed by its provider ID'), receipt));
    }
    if (classification.kind !== 'read') return final(newMutationUncertain(post.requested, client.failure(post.operation, classification.failure!.kind, `an earlier attempt of this Step sent the ${post.subject} and it could not be read back, so it is not sent again: ${classification.failure!.message}`), receipt), isResendAllowed);
    let collection: Record<string, unknown>;
    try {
      const decoded = decodeJSON(exchange.body);
      if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded) || !('value' in decoded) || !Array.isArray(decoded.value)) throw new TypeError('invalid collection');
      collection = decoded as Record<string, unknown>;
    } catch { return final(newMutationUncertain(post.requested, client.failure(post.operation, 'PROTOCOL', `an earlier attempt of this Step sent the ${post.subject}, and the messages read back are invalid`), receipt), isResendAllowed); }
    const context = call.context as typeof call.context & { fixedClock?: Date };
    const earliest = context.firstAttemptAt ?? context.fixedClock ?? new Date(0);
    const matches: Message[] = [];
    for (const value of collection.value as unknown[]) {
      if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
      const resource = value as Record<string, unknown>;
      if (typeof resource.id !== 'string' || !messageID.test(resource.id) || resource.messageType !== 'message' || resource.deletedDateTime != null) continue;
      const from = resource.from as Record<string, unknown> | undefined;
      const body = resource.body as Record<string, unknown> | undefined;
      if (!from || !from.user || !body) continue;
      let created: string;
      try { created = parseGraphTime(resource.createdDateTime, 'createdDateTime'); }
      catch { continue; }
      if (Date.parse(created) < earliest.getTime() - 60_000 || (resource.replyToId ?? '') !== post.replyToID || String(resource.subject ?? '').trim() !== String(post.body.subject ?? '').trim()) continue;
      if (comparisonText(extractMessageText(String(body.contentType ?? ''), String(body.content ?? ''))) !== post.comparison) continue;
      try { matches.push(decodeMessage(value)); }
      catch { /* Only valid matching messages can confirm a write. */ }
    }
    if (matches.length === 1) return final(newMutationBranch('sent', postedOutput(post.requested, matches[0]!, true), client.receipt(call, exchange, matches[0]!.id)));
    if (!matches.length) return final(newMutationUncertain(post.requested, client.failure(post.operation, 'TRANSPORT', `an earlier attempt of this Step sent the ${post.subject} without a confirmed outcome, and the newest messages show no match, so it is not sent again`), receipt), isResendAllowed);
    // Several matches mean a duplicate may already exist, so another send is never allowed.
    return final(newMutationUncertain(post.requested, client.failure(post.operation, 'CONFLICT', `an earlier attempt of this Step sent the ${post.subject} without a confirmed outcome, and several identical messages were found, so it is not sent again`), receipt));
  }
}

/** Retry an unknown outcome; a readable post is read back first on the next attempt. */
function resendOrSelectUncertain(post: Post, canResendUnknownOutcome: boolean, failure: Failure, retryAfterMs: number, receipt: ReturnType<TeamsClient['receipt']>): MutationAttempt<PostMessageOutput> {
  // The claim stays the last marker, so the next attempt counts this send and reads back.
  if (post.readBackPath) return newMutationRetry(failure, Math.max(retryAfterMs, 5_000));
  if (canResendUnknownOutcome) return newMutationRetry(failure, retryAfterMs);
  return newMutationUncertain(post.requested, { ...failure, message: `${failure.message}; the send cap is reached, so the ${post.subject} is not sent again` }, receipt);
}
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}
function carriesMessageID(body: Uint8Array): boolean {
  try {
    const resource = decodeJSON(body);
    return !!resource && typeof resource === 'object' && !Array.isArray(resource) && 'id' in resource && typeof resource.id === 'string' && messageID.test(resource.id);
  } catch { return false; }
}
/** Post one top-level Teams channel message. */
export class PostChannelMessageOperation extends PostOperation<PostChannelMessageInput> {
  /** Return the generated Mutation definition. */
  definition(): MutationDefinition { return PostChannelMessageDefinition; }
  /** Validate and fence one Graph channel post. */
  async invoke(call: Call, input: PostChannelMessageInput): Promise<MutationAttempt<PostMessageOutput>> {
    const team = input.teamId?.trim() ?? '', channel = input.channelId?.trim() ?? '';
    const requested = { ...emptyPost(), ...(team ? { teamId: team } : {}), ...(channel ? { channelId: channel } : {}) };
    try {
      PostChannelMessageInputSchema.parse(input); validateTeamChannel(team, channel);
      const { body, comparison } = buildContent(input.content, input.contentType, input.importance, input.subject?.trim() ?? '', this.client.maxMessageBytes);
      const path = channelMessagesPath(team, channel);
      return this.send(call, { operation: 'postChannelMessage', subject: 'channel message', path, readBackPath: path, replyToID: '', body, comparison, requested });
    } catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), this.client.failure('postChannelMessage', 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
  }
}
/** Post one reply to an existing channel thread. */
export class PostThreadReplyOperation extends PostOperation<PostThreadReplyInput> {
  /** Return the generated Mutation definition. */
  definition(): MutationDefinition { return PostThreadReplyDefinition; }
  /** Validate and fence one Graph reply. */
  async invoke(call: Call, input: PostThreadReplyInput): Promise<MutationAttempt<PostMessageOutput>> {
    const team = input.teamId?.trim() ?? '', channel = input.channelId?.trim() ?? '', message = input.messageId?.trim() ?? '';
    const requested = { ...emptyPost(), ...(team ? { teamId: team } : {}), ...(channel ? { channelId: channel } : {}), ...(message ? { replyToId: message } : {}) };
    try {
      PostThreadReplyInputSchema.parse(input); validateTeamChannel(team, channel); validateMessageID('messageId', message);
      const { body, comparison } = buildContent(input.content, input.contentType, input.importance, '', this.client.maxMessageBytes);
      const path = threadRepliesPath(team, channel, message);
      return this.send(call, { operation: 'postThreadReply', subject: 'thread reply', path, readBackPath: path, replyToID: message, body, comparison, requested });
    } catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), this.client.failure('postThreadReply', 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
  }
}
/** Post one message in an existing Teams chat. */
export class PostChatMessageOperation extends PostOperation<PostChatMessageInput> {
  /** Return the generated Mutation definition. */
  definition(): MutationDefinition { return PostChatMessageDefinition; }
  /** Validate and fence one Graph chat post. */
  async invoke(call: Call, input: PostChatMessageInput): Promise<MutationAttempt<PostMessageOutput>> {
    const chat = input.chatId?.trim() ?? '';
    const requested = { ...emptyPost(), ...(chat ? { chatId: chat } : {}) };
    try {
      PostChatMessageInputSchema.parse(input); validateChatID(chat);
      const { body, comparison } = buildContent(input.content, input.contentType, input.importance, '', this.client.maxMessageBytes);
      return this.send(call, { operation: 'postChatMessage', subject: 'chat message', path: `/chats/${chat}/messages`, readBackPath: '', replyToID: '', body, comparison, requested });
    } catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), this.client.failure('postChatMessage', 'VALIDATION', error instanceof Error ? error.message : 'invalid input')); }
  }
}
