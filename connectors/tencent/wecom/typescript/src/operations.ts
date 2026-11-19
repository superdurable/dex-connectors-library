// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, type Call, type Failure, type Mutation, type MutationAttempt } from '@superdurable/connectors-sdk';
import { CheckpointError, DispatchNotAllowedError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { SecretString } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { ReauthorizationRequiredError } from '@superdurable/connectors-sdk';
import { isCredentialMaterialError, type Credentials, type SendApplicationMessageInput, type SendGroupRobotMessageInput, type SentApplicationMessage, type SentGroupRobotMessage } from './generated/models.js';
import { SendApplicationMessageDefinition, SendGroupRobotMessageDefinition } from './generated/definitions.js';
import { decodeEnvelope, isReadableRefusal, isRetryableCode, kindForCode, refusalStatuses, retryAfterMs, type WeComClient, type WeComExchange } from './client.js';

const emptyRobot: SentGroupRobotMessage = { messageType: '', contentBytes: 0 };
const emptyApplication: SentApplicationMessage = { messageId: '', invalidUserIds: [], invalidDepartmentIds: [], invalidTagIds: [], unlicensedUserIds: [] };
const webhookKeyPattern = /^[A-Za-z0-9-]{8,64}$/;
const corpIdPattern = /^[A-Za-z0-9]{4,64}$/;
const agentIdPattern = /^[1-9][0-9]{0,9}$/;
const positiveGroupId = /^[1-9][0-9]{0,9}$/;
const messageIdPattern = /^[A-Za-z0-9_=+/-]{1,128}$/;
const duplicateResendCutoffMs = 3 * 3_600_000 + 50 * 60_000;
const executionTimeoutMs = 30_000;

type Output = SentGroupRobotMessage | SentApplicationMessage;
type SendState<T extends Output> = { fence: Fence; canResend: boolean; hadPriorSend: boolean; operation: string; empty: T };
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'wecom', operation, message }; }
function branch<T extends Output>(state: SendState<T>, name: string, kind: Failure['kind'], message: string): MutationAttempt<T> { return newMutationBranch(name, state.empty, {}, failure(state.operation, kind, message)); }
function retry<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string, delay = 0): MutationAttempt<T> { return newMutationRetry(failure(state.operation, kind, message), delay); }
function uncertain<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string): MutationAttempt<T> { return newMutationUncertain(state.empty, failure(state.operation, kind, message)); }
async function checkpointNotSent<T extends Output>(state: SendState<T>): Promise<void> {
  if (state.hadPriorSend) return;
  try { await state.fence.recordNotSent(); } catch { /* A lost proof cannot authorize a send. */ }
}
async function checkpointCanResend<T extends Output>(state: SendState<T>): Promise<void> {
  try { await state.fence.recordCanResend(); } catch { /* The claim remains and counts as a possible send. */ }
}
async function checkpointAccepted<T extends Output>(state: SendState<T>, marker: string): Promise<void> {
  try { await state.fence.recordProviderId(marker); } catch { /* The accepted marker remains in this attempt if persistence fails. */ }
}
async function localFailure<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string, rejected = false): Promise<MutationAttempt<T>> {
  if (state.hadPriorSend) return uncertain(state, kind, `${message}; an earlier attempt may have delivered the message`);
  await checkpointNotSent(state);
  return branch(state, rejected ? 'providerRejected' : 'defect', kind, message);
}
async function retryBeforeSend<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string): Promise<MutationAttempt<T>> {
  await checkpointNotSent(state);
  return retry(state, kind, message);
}
function resendOrUncertain<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string, delay = 0): MutationAttempt<T> {
  return state.canResend ? retry(state, kind, message, delay) : uncertain(state, kind, message);
}
async function rejectOrUncertain<T extends Output>(state: SendState<T>, kind: Failure['kind'], message: string): Promise<MutationAttempt<T>> {
  if (state.hadPriorSend) return uncertain(state, kind, message);
  await checkpointCanResend(state);
  return branch(state, 'providerRejected', kind, message);
}
async function resolveCredentials<T extends Output>(client: WeComClient, call: Call, state: SendState<T>): Promise<{ credentials: Credentials } | { outcome: MutationAttempt<T> }> {
  try { return { credentials: await client.credentials.resolve(call, call.context.cancellationSignal) }; }
  catch (error) {
    if (error instanceof ReauthorizationRequiredError || error instanceof ObjectNotFoundError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || isCredentialMaterialError(error))
      return { outcome: await localFailure(state, 'AUTHENTICATION', 'WeCom connection credentials are unavailable') };
    return { outcome: await retryBeforeSend(state, 'AUTHENTICATION', 'WeCom credentials are temporarily unavailable') };
  }
}
function validateContent(value: string, limit: number): boolean { return Boolean(value) && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value) && Buffer.byteLength(value) <= limit; }
function printableIdentifier(value: string): boolean { return Buffer.byteLength(value) <= 64 && /^[!-~]+$/.test(value) && !value.includes('|'); }
function validateRobot(input: SendGroupRobotMessageInput): string {
  if (!input.content || !validateContent(input.content, 4096)) return 'content must be nonempty UTF-8';
  if (input.messageType === 'text') { if (Buffer.byteLength(input.content) > 2048) return 'text content must be at most 2048 UTF-8 bytes'; }
  else if (input.messageType === 'markdown') { if (input.mentionedUserIds?.length || input.mentionedMobileNumbers?.length) return 'markdown posts cannot carry mention lists'; }
  else return 'messageType must be text or markdown';
  if ([...(input.mentionedUserIds ?? []), ...(input.mentionedMobileNumbers ?? [])].some(value => !printableIdentifier(value))) return 'each mention must be a nonblank user ID, mobile number, or @all of at most 64 bytes';
  return '';
}
function validateApplication(input: SendApplicationMessageInput): string {
  if (!validateContent(input.content, 2048)) return 'content must be nonempty UTF-8 of at most 2048 bytes';
  if (input.messageType !== 'text' && input.messageType !== 'markdown') return 'messageType must be text or markdown';
  if (input.messageType === 'markdown' && input.isConfidential) return 'isConfidential applies only to text messages';
  const hasLists = Boolean(input.toUserIds?.length || input.toDepartmentIds?.length || input.toTagIds?.length);
  if (Boolean(input.toAllMembers) === hasLists) return 'choose toAllMembers or at least one recipient list';
  if ((input.toUserIds?.length ?? 0) > 1000 || (input.toDepartmentIds?.length ?? 0) > 100 || (input.toTagIds?.length ?? 0) > 100) return 'too many recipients';
  if (input.toUserIds?.some(value => !printableIdentifier(value) || value === '@all')) return 'invalid toUserIds entry';
  if ([...(input.toDepartmentIds ?? []), ...(input.toTagIds ?? [])].some(value => !positiveGroupId.test(value))) return 'invalid department or tag ID';
  return '';
}
function newFenceState<T extends Output>(call: Call, operation: string, empty: T): { state: SendState<T> } | { outcome: MutationAttempt<T> } {
  let fence: Fence;
  try { fence = newFence(call, executionTimeoutMs, { maxSends: 3 }); }
  catch { return { outcome: newMutationBranch('defect', empty, {}, failure(operation, 'LOCAL_DEFECT', 'WeCom dispatch fence is unavailable')) }; }
  const decision = fence.decide();
  const prior = decision.priorSendUpperBound ?? 0;
  return { state: { fence, operation, empty, canResend: prior + 1 < (call.maxSendsOverride ?? 3), hadPriorSend: prior > 0 } };
}
async function fencedExchange<T extends Output>(state: SendState<T>, send: (signal: AbortSignal) => Promise<WeComExchange>): Promise<WeComExchange | MutationAttempt<T>> {
  try { return await state.fence.dispatch(send); }
  catch (error) {
    if (error instanceof CheckpointError || error instanceof InsufficientTimeError || error instanceof DispatchNotAllowedError || error instanceof AggregateError)
      return retry(state, 'TRANSPORT', 'WeCom dispatch was not started');
    return { status: 0, body: new Uint8Array(), headers: {}, error: 'request' };
  }
}
function isAttempt<T extends Output>(value: WeComExchange | MutationAttempt<T>): value is MutationAttempt<T> { return !('status' in value); }
async function classifyResponse<T extends Output>(state: SendState<T>, exchange: WeComExchange, client: WeComClient, accepted: (body: Record<string, unknown>) => Promise<MutationAttempt<T>>, onTokenRejected?: () => void): Promise<MutationAttempt<T>> {
  if (exchange.status === 0) {
    if (exchange.isProvablyNotSent) { await checkpointCanResend(state); return retry(state, 'TRANSPORT', 'WeCom could not be reached before sending'); }
    return resendOrUncertain(state, 'TRANSPORT', 'WeCom message outcome is unknown');
  }
  if (exchange.status === 429 || exchange.status === 503) { await checkpointCanResend(state); return retry(state, exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'WeCom did not process the message', retryAfterMs(exchange, client.now())); }
  if (refusalStatuses.has(exchange.status)) {
    if (!isReadableRefusal(exchange)) return resendOrUncertain(state, 'PROTOCOL', 'WeCom message outcome is unknown after HTTP refusal');
    return rejectOrUncertain(state, 'PROVIDER_REJECTION', `WeCom rejected the message with HTTP ${exchange.status}`);
  }
  if (exchange.status < 200 || exchange.status >= 300) return resendOrUncertain(state, 'AVAILABILITY', `WeCom message outcome is unknown after HTTP ${exchange.status}`, retryAfterMs(exchange, client.now()));
  if (exchange.error === 'read') return uncertain(state, 'TRANSPORT', 'WeCom answered, but its response body broke');
  if (exchange.error === 'tooLarge') return resendOrUncertain(state, 'RESPONSE_TOO_LARGE', 'WeCom response exceeded maxResponseBytes');
  const envelope = decodeEnvelope(exchange.body);
  if (!envelope) return resendOrUncertain(state, 'PROTOCOL', 'WeCom response is malformed');
  if (envelope.errcode === 0) return accepted(envelope);
  if (onTokenRejected && [40014, 41001, 42001].includes(envelope.errcode)) onTokenRejected();
  if (isRetryableCode(envelope.errcode) || (onTokenRejected && [40014, 41001, 42001].includes(envelope.errcode))) {
    await checkpointCanResend(state);
    return retry(state, kindForCode(envelope.errcode), `WeCom deferred the message with errcode ${envelope.errcode}`, envelope.errcode === 45009 && !onTokenRejected ? 60_000 : 0);
  }
  return rejectOrUncertain(state, kindForCode(envelope.errcode), `WeCom rejected the message with errcode ${envelope.errcode}`);
}

/** Post one text or markdown group robot message. */
export class SendGroupRobotMessageOperation implements Mutation<SendGroupRobotMessageInput, SentGroupRobotMessage> {
  /** Bind a WeCom client. */ constructor(private readonly client: WeComClient) {}
  /** Return the manifest definition. */ definition() { return SendGroupRobotMessageDefinition; }
  /** Use stable Dex call identity for the local fence; WeCom offers no deduplication key. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and send one fenced group robot post. */
  async invoke(call: Call, input: SendGroupRobotMessageInput): Promise<MutationAttempt<SentGroupRobotMessage>> {
    const invalid = validateRobot(input);
    if (invalid) return newMutationBranch('defect', emptyRobot, {}, failure('sendGroupRobotMessage', 'VALIDATION', invalid));
    const setup = newFenceState(call, 'sendGroupRobotMessage', emptyRobot);
    if ('outcome' in setup) return setup.outcome;
    const state = setup.state;
    const decision = state.fence.decide();
    if (decision.kind !== 'failClosed' && decision.marker?.providerId === 'errcode-0') return newMutationBranch('sent', { messageType: input.messageType, contentBytes: Buffer.byteLength(input.content) });
    if (decision.kind === 'failClosed') return retry(state, 'AVAILABILITY', 'WeCom dispatch checkpoint is unreadable');
    if (decision.kind === 'readBackRequired') return uncertain(state, 'TRANSPORT', 'WeCom group robot send cap is reached');
    const resolved = await resolveCredentials(this.client, call, state);
    if ('outcome' in resolved) return resolved.outcome;
    const credentials = resolved.credentials;
    if ((credentials.authMethodId ?? 'group-robot') !== 'group-robot') return localFailure(state, 'VALIDATION', 'sendGroupRobotMessage needs the group-robot method');
    const key = credentials.webhookKey instanceof SecretString ? credentials.webhookKey.reveal() : '';
    if (!webhookKeyPattern.test(key)) return localFailure(state, 'AUTHENTICATION', 'webhook_key has an invalid format');
    const body = Buffer.from(JSON.stringify(input.messageType === 'markdown'
      ? { msgtype: 'markdown', markdown: { content: input.content } }
      : { msgtype: 'text', text: { content: input.content, ...(input.mentionedUserIds?.length ? { mentioned_list: input.mentionedUserIds } : {}), ...(input.mentionedMobileNumbers?.length ? { mentioned_mobile_list: input.mentionedMobileNumbers } : {}) } }), 'utf8');
    const exchange = await fencedExchange(state, signal => this.client.exchange('POST', '/cgi-bin/webhook/send', new URLSearchParams({ key }), body, signal));
    if (isAttempt(exchange)) return exchange;
    return classifyResponse(state, exchange, this.client, async () => {
      await checkpointAccepted(state, 'errcode-0');
      return newMutationBranch('sent', { messageType: input.messageType, contentBytes: Buffer.byteLength(input.content) });
    });
  }
}

/** Send one message as a self-built WeCom application. */
export class SendApplicationMessageOperation implements Mutation<SendApplicationMessageInput, SentApplicationMessage> {
  /** Bind a WeCom client. */ constructor(private readonly client: WeComClient) {}
  /** Return the manifest definition. */ definition() { return SendApplicationMessageDefinition; }
  /** Use stable Dex call identity while WeCom deduplicates identical request content. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, fetch a token, and send one fenced application message. */
  async invoke(call: Call, input: SendApplicationMessageInput): Promise<MutationAttempt<SentApplicationMessage>> {
    const invalid = validateApplication(input);
    if (invalid) return newMutationBranch('defect', emptyApplication, {}, failure('sendApplicationMessage', 'VALIDATION', invalid));
    const setup = newFenceState(call, 'sendApplicationMessage', emptyApplication);
    if ('outcome' in setup) return setup.outcome;
    const state = setup.state;
    const decision = state.fence.decide();
    if (decision.kind !== 'failClosed' && decision.marker?.providerId) return newMutationBranch('sent', { ...emptyApplication, messageId: decision.marker.providerId === 'errcode-0' ? '' : decision.marker.providerId });
    if (decision.kind === 'failClosed') return retry(state, 'AVAILABILITY', 'WeCom dispatch checkpoint is unreadable');
    if (decision.kind === 'readBackRequired') return uncertain(state, 'TRANSPORT', 'WeCom application send cap is reached');
    if (state.hadPriorSend && this.client.now().getTime() >= firstAttemptTime(call) + duplicateResendCutoffMs)
      return uncertain(state, 'TRANSPORT', 'WeCom duplicate check no longer safely suppresses a resend');
    const resolved = await resolveCredentials(this.client, call, state);
    if ('outcome' in resolved) return resolved.outcome;
    const credentials = resolved.credentials;
    if (credentials.authMethodId !== 'application') return localFailure(state, 'VALIDATION', 'sendApplicationMessage needs the application method');
    const secret = credentials.corpSecret instanceof SecretString ? credentials.corpSecret.reveal() : '';
    if (!corpIdPattern.test(credentials.corpId ?? '') || !agentIdPattern.test(credentials.agentId ?? '') || !secret || /[\r\n\0]/.test(secret))
      return localFailure(state, 'AUTHENTICATION', 'corp_id, agent_id, or corp_secret has an invalid format');
    const corpId = credentials.corpId!;
    const token = await this.client.accessToken(call, corpId, secret, 'sendApplicationMessage');
    if ('failure' in token) return token.disposition === 'retry'
      ? retryBeforeSend(state, token.failure.kind, token.failure.message)
      : localFailure(state, token.failure.kind, token.failure.message, true);
    const bodyObject: Record<string, unknown> = {};
    if (input.toAllMembers) bodyObject.touser = '@all'; else {
      if (input.toUserIds?.length) bodyObject.touser = input.toUserIds.join('|');
      if (input.toDepartmentIds?.length) bodyObject.toparty = input.toDepartmentIds.join('|');
      if (input.toTagIds?.length) bodyObject.totag = input.toTagIds.join('|');
    }
    bodyObject.msgtype = input.messageType;
    bodyObject.agentid = Number(credentials.agentId);
    bodyObject[input.messageType] = { content: input.content };
    if (input.isConfidential) bodyObject.safe = 1;
    bodyObject.enable_duplicate_check = 1;
    bodyObject.duplicate_check_interval = 14400;
    const body = Buffer.from(JSON.stringify(bodyObject), 'utf8');
    const exchange = await fencedExchange(state, signal => this.client.exchange('POST', '/cgi-bin/message/send', new URLSearchParams({ access_token: token.token }), body, signal));
    if (isAttempt(exchange)) return exchange;
    return classifyResponse(state, exchange, this.client, async envelope => {
      const fields = ['invaliduser', 'invalidparty', 'invalidtag', 'unlicenseduser', 'msgid'] as const;
      if (fields.some(field => envelope[field] !== undefined && typeof envelope[field] !== 'string')) return uncertain(state, 'PROTOCOL', 'WeCom accepted the message, but its response fields are malformed');
      const messageId = envelope.msgid as string | undefined ?? '';
      await checkpointAccepted(state, messageIdPattern.test(messageId) ? messageId : 'errcode-0');
      const split = (field: string): string[] => typeof envelope[field] === 'string' ? (envelope[field] as string).split('|').filter(Boolean) : [];
      return newMutationBranch('sent', { messageId, invalidUserIds: split('invaliduser'), invalidDepartmentIds: split('invalidparty'), invalidTagIds: split('invalidtag'), unlicensedUserIds: split('unlicenseduser') });
    }, () => this.client.invalidateAccessToken(corpId, secret, token.token));
  }
}

function firstAttemptTime(call: Call): number {
  const context = call.context as unknown as { firstAttemptAt?: Date | (() => Date); flowStartedAt?: Date };
  const value = typeof context.firstAttemptAt === 'function' ? context.firstAttemptAt() : context.firstAttemptAt;
  return value?.getTime() ?? context.flowStartedAt?.getTime() ?? Date.now();
}
