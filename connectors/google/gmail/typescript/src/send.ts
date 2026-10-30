// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, zeroReceipt,
  type Call, type Failure, type Mutation, type MutationAttempt, type ReceiptDraft,
} from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { setTimeout as delay } from 'node:timers/promises';
import { ReplyToMessageBranch, ReplyToMessageDefinition, SendMessageBranch, SendMessageDefinition } from './definitions.js';
import { GmailCredentialError, GmailHTTPClient, gmailRequestID, gmailRetryAfterMs, statusFailureKind } from './http.js';
import { buildMIMEMessage, buildReplyMIMEMessage, decodeGmailMessage, replyRecipient, rfcMessageIDAddress, validateSendInput } from './message.js';
import { ReplyToMessageInputSchema, SendMessageInputSchema, type Credentials, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput } from './models.js';
import { GmailProvider, parseSentResponse, type SentMessageIdentity } from './provider.js';

interface SendOutcomes {
  operationId: 'sendMessage' | 'replyToMessage';
  requestName: 'send' | 'reply';
  rejectionMessage: string;
  sentBranch: string;
  providerRejectedBranch: string;
}

interface OutgoingMessage {
  raw: Uint8Array;
  recipients: string[];
  requestedThreadId: string;
  hasPreSendSearch: boolean;
}

const SendMessageOutcomes: SendOutcomes = {
  operationId: 'sendMessage', requestName: 'send', rejectionMessage: 'Gmail rejected the message',
  sentBranch: SendMessageBranch.Sent, providerRejectedBranch: SendMessageBranch.ProviderRejected,
};
const ReplyToMessageOutcomes: SendOutcomes = {
  operationId: 'replyToMessage', requestName: 'reply', rejectionMessage: 'Gmail rejected the reply',
  sentBranch: ReplyToMessageBranch.Sent, providerRejectedBranch: ReplyToMessageBranch.ProviderRejected,
};

/** One Dex Execute attempt's reserved send and read-back windows. */
export class GmailSendAttemptBudget {
  /** Read-back cutoff in Unix milliseconds. */
  readonly attemptDeadline: number;
  /** POST cutoff in Unix milliseconds. */
  readonly sendDeadline: number;
  /** Minimum remaining POST window in milliseconds. */
  readonly minimumSendMs: number;
  readonly #parentSignal: AbortSignal;

  /** Bound an attempt by both Dex's actual deadline and the manifest's Execute timeout. */
  constructor(parentSignal: AbortSignal, dexDeadline: Date | undefined, now = new Date(), executeTimeoutMs = 30_000) {
    const available = Math.min(executeTimeoutMs, dexDeadline ? dexDeadline.getTime() - now.getTime() : executeTimeoutMs);
    this.attemptDeadline = now.getTime() + available * 5 / 6;
    this.sendDeadline = now.getTime() + available * 7 / 12;
    this.minimumSendMs = available / 10;
    this.#parentSignal = parentSignal;
  }

  /** Bound a read before POST so at least one tenth of the attempt remains to send. */
  preSendSignal(): AbortSignal { return this.#boundedSignal(this.sendDeadline - this.minimumSendMs); }
  /** Bound the POST, or return undefined when too little attempt time remains. */
  sendSignal(): AbortSignal | undefined {
    return this.minimumSendMs > 0 && this.sendDeadline - Date.now() >= this.minimumSendMs ? this.#boundedSignal(this.sendDeadline) : undefined;
  }
  /** Bound read-back and other work before the Result write. */
  attemptSignal(): AbortSignal { return this.#boundedSignal(this.attemptDeadline); }

  #boundedSignal(deadline: number): AbortSignal {
    const remaining = Math.ceil(deadline - Date.now());
    if (remaining <= 0) return AbortSignal.abort();
    return AbortSignal.any([this.#parentSignal, AbortSignal.timeout(remaining)]);
  }
}

/** New-message Mutation bound to a Gmail client. */
export class SendMessageOperation implements Mutation<SendMessageInput, SendMessageOutput> {
  /** Inject the provider sender used by each Dex attempt. */
  constructor(private readonly sender: GmailSender) {
    if (!sender) throw new TypeError('Gmail sender is required');
  }

  /** Return the immutable manifest operation definition. */
  definition() { return SendMessageDefinition; }
  /** Use the stable connector Call ID as the send correlation key. */
  idempotencyKey(callId: string, input: SendMessageInput): string { void input; return callId; }
  /** Classify a send using Dex's public current-attempt deadline. */
  invoke(call: Call, input: SendMessageInput): Promise<MutationAttempt<SendMessageOutput>> {
    return this.sender.sendMessage(call, input, call.context.deadline);
  }
}

/** Thread-reply Mutation bound to a Gmail client. */
export class ReplyToMessageOperation implements Mutation<ReplyToMessageInput, SendMessageOutput> {
  /** Inject the provider sender used by each Dex attempt. */
  constructor(private readonly sender: GmailSender) {
    if (!sender) throw new TypeError('Gmail sender is required');
  }

  /** Return the immutable manifest operation definition. */
  definition() { return ReplyToMessageDefinition; }
  /** Use the stable connector Call ID as the reply correlation key. */
  idempotencyKey(callId: string, input: ReplyToMessageInput): string { void input; return callId; }
  /** Classify a reply using Dex's public current-attempt deadline. */
  invoke(call: Call, input: ReplyToMessageInput): Promise<MutationAttempt<SendMessageOutput>> {
    return this.sender.replyToMessage(call, input, call.context.deadline);
  }
}

/** Gmail send and reply logic with attempt-aware read-back reconciliation. */
export class GmailSender {
  readonly #http: GmailHTTPClient;
  readonly #provider: GmailProvider;
  readonly #now: () => Date;

  /** Inject the shared authenticated provider client and observation clock. */
  constructor(http: GmailHTTPClient, provider: GmailProvider, now: () => Date = () => new Date()) {
    if (!http || !provider || !now) throw new TypeError('Gmail send dependencies are required');
    this.#http = http; this.#provider = provider; this.#now = now;
  }

  /** Send one new message under the Dex attempt deadline. */
  async sendMessage(call: Call, input: SendMessageInput, dexDeadline: Date | undefined): Promise<MutationAttempt<SendMessageOutput>> {
    if (call.context.attempt < 1) return defect('sendMessage', 'LOCAL_DEFECT', 'Dex Execute attempt number is invalid');
    const fence = newFence(call, SendMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000);
    if (fence.decide().kind === 'failClosed') return defect('sendMessage', 'LOCAL_DEFECT', 'Dex send checkpoint is unreadable');
    const budget = new GmailSendAttemptBudget(call.context.cancellationSignal, dexDeadline);
    if (!budget.sendSignal()) {
      await recordPreSendRetry(fence);
      return newMutationRetry(failure('sendMessage', 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
    }
    let credentials: Credentials;
    try { credentials = await this.#http.resolveCredentials(call, budget.attemptSignal()); }
    catch { return defect('sendMessage', 'AUTHENTICATION', 'connection credentials are unavailable'); }
    let recipients: string[];
    try {
      if (!SendMessageInputSchema.safeParse(input).success) throw new TypeError();
      recipients = validateSendInput(input, credentials.primaryEmail);
    } catch (error) {
      return defect('sendMessage', 'VALIDATION', error instanceof TypeError && error.message ? error.message : 'sender, recipients, subject, and text body are required');
    }
    let raw: Uint8Array;
    try {
      raw = buildMIMEMessage(call.id, call.idempotencyKey ?? call.id, this.#messageID(call), credentials.primaryEmail, recipients, input.subject, input.textBody, input.htmlBody);
    } catch { return defect('sendMessage', 'VALIDATION', 'message could not be encoded'); }
    if (raw.length > this.#http.settings().maxMessageBytes) return defect('sendMessage', 'VALIDATION', 'message exceeds configured size limit');
    return this.#sendAndConfirm(call, fence, budget, credentials, SendMessageOutcomes, { raw, recipients, requestedThreadId: '', hasPreSendSearch: false });
  }

  /** Read the source then reply in its Gmail thread under the Dex attempt deadline. */
  async replyToMessage(call: Call, input: ReplyToMessageInput, dexDeadline: Date | undefined): Promise<MutationAttempt<SendMessageOutput>> {
    if (call.context.attempt < 1) return defect('replyToMessage', 'LOCAL_DEFECT', 'Dex Execute attempt number is invalid');
    const fence = newFence(call, ReplyToMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000);
    if (fence.decide().kind === 'failClosed') return defect('replyToMessage', 'LOCAL_DEFECT', 'Dex send checkpoint is unreadable');
    const budget = new GmailSendAttemptBudget(call.context.cancellationSignal, dexDeadline);
    if (!budget.sendSignal()) {
      await recordPreSendRetry(fence);
      return newMutationRetry(failure('replyToMessage', 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
    }
    let credentials: Credentials;
    try { credentials = await this.#http.resolveCredentials(call, budget.attemptSignal()); }
    catch { return defect('replyToMessage', 'AUTHENTICATION', 'connection credentials are unavailable'); }
    if (!ReplyToMessageInputSchema.safeParse(input).success || !input.messageId.trim() || !input.textBody.trim()) return defect('replyToMessage', 'VALIDATION', 'message ID and text body are required');
    const priorProviderId = fence.decide().marker?.providerId;
    if (fence.decide().kind === 'readBackRequired' && priorProviderId) {
      return this.#sentMessage(call, budget, credentials, ReplyToMessageOutcomes,
        { raw: new Uint8Array(), recipients: [], requestedThreadId: '', hasPreSendSearch: true },
        { id: priorProviderId, threadId: '' }, this.#receipt(call, '', priorProviderId));
    }
    if (call.context.attempt > 1 || fence.decide().kind === 'readBackRequired') {
      try {
        const found = await this.#provider.findSentMessage(call, credentials, this.#messageID(call), budget.preSendSignal());
        if (found) return this.#sentMessage(call, budget, credentials, ReplyToMessageOutcomes, { raw: new Uint8Array(), recipients: [], requestedThreadId: '', hasPreSendSearch: true }, found, this.#receipt(call));
      } catch {
        await recordPreSendRetry(fence);
        return newMutationRetry(failure('replyToMessage', 'AVAILABILITY', 'Gmail sent-mail search did not answer before the reply'));
      }
      if (fence.decide().kind === 'readBackRequired') return newMutationRetry(failure('replyToMessage', 'AVAILABILITY', 'Gmail reply outcome is unknown; the sent-mail search found no message'));
    }
    let source;
    try { source = await this.#provider.readMessage(call, credentials, input.messageId, 'metadata', budget.preSendSignal()); }
    catch (error) {
      if (error instanceof GmailCredentialError) return defect('replyToMessage', 'AUTHENTICATION', 'connection credentials are unavailable');
      const status = error instanceof BodyTooLargeError ? error.status ?? 0 : 0;
      const headers = error instanceof BodyTooLargeError ? error.headers ?? {} : {};
      const result = classifySourceFailure(status, headers, this.#receipt(call));
      if (result.kind === 'retry') await recordPreSendRetry(fence);
      return result;
    }
    credentials = source.credentials;
    if (!source.resource) {
      const result = classifySourceFailure(source.response.status, source.response.headers, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId));
      if (result.kind === 'retry') await recordPreSendRetry(fence);
      return result;
    }
    let message: Message;
    try { message = decodeGmailMessage(source.resource); }
    catch { return newMutationBranch(ReplyToMessageBranch.InvalidResponse, emptySend(), this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'source message lacks reply metadata')); }
    if (!message.threadId || !message.rfcMessageId) return newMutationBranch(ReplyToMessageBranch.InvalidResponse, emptySend(), this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'source message lacks reply metadata'));
    let recipient: string;
    try { recipient = replyRecipient(message); }
    catch { return newMutationBranch(ReplyToMessageBranch.InvalidResponse, emptySend(), this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'Gmail returned an invalid source sender')); }
    if (/[\r\n]/.test(message.subject + message.rfcMessageId + (message.references ?? ''))) return newMutationBranch(ReplyToMessageBranch.InvalidResponse, emptySend(), this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'Gmail returned invalid source reply headers'));
    let raw: Uint8Array;
    try { raw = buildReplyMIMEMessage(call.id, call.idempotencyKey ?? call.id, this.#messageID(call), credentials.primaryEmail, message, input); }
    catch { return defect('replyToMessage', 'VALIDATION', 'reply could not be encoded within the configured limit'); }
    if (raw.length > this.#http.settings().maxMessageBytes) return defect('replyToMessage', 'VALIDATION', 'reply could not be encoded within the configured limit');
    return this.#sendAndConfirm(call, fence, budget, credentials, ReplyToMessageOutcomes, { raw, recipients: [recipient], requestedThreadId: message.threadId, hasPreSendSearch: call.context.attempt > 1 });
  }

  async #sendAndConfirm(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage): Promise<MutationAttempt<SendMessageOutput>> {
    const priorProviderId = fence.decide().marker?.providerId;
    if (fence.decide().kind === 'readBackRequired' && priorProviderId) {
      return this.#sentMessage(call, budget, credentials, operation, message,
        { id: priorProviderId, threadId: message.requestedThreadId }, this.#receipt(call, '', priorProviderId));
    }
    if ((call.context.attempt > 1 || fence.decide().kind === 'readBackRequired') && !message.hasPreSendSearch) {
      try {
        const found = await this.#provider.findSentMessage(call, credentials, this.#messageID(call), budget.preSendSignal());
        if (found) return this.#sentMessage(call, budget, credentials, operation, message, found, this.#receipt(call));
      } catch {
        await recordPreSendRetry(fence);
        return newMutationRetry(failure(operation.operationId, 'AVAILABILITY', `Gmail sent-mail search did not answer before the ${operation.requestName}`));
      }
    }
    if (fence.decide().kind === 'readBackRequired') return newMutationRetry(failure(operation.operationId, 'AVAILABILITY', `Gmail ${operation.requestName} outcome is unknown; the sent-mail search found no message`));
    let response;
    try { response = await fence.dispatch(signal => this.#provider.sendMessage(call, credentials, message.raw, message.requestedThreadId, signal)); }
    catch (error) {
      if (error instanceof InsufficientTimeError || error instanceof CheckpointError) return newMutationRetry(failure(operation.operationId, 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
      if (error instanceof ProviderTransportError && error.isProvablyNotSent || error instanceof AddressRefusal) {
        await recordSafeResend(fence);
        return newMutationRetry(failure(operation.operationId, 'TRANSPORT', `Gmail ${operation.requestName} request was not dispatched`));
      }
      if (error instanceof BodyTooLargeError && error.status) return this.#classifySendStatus(call, fence, budget, credentials, operation, message, error.status, error.headers ?? {}, undefined);
      return this.#confirmPossiblySent(call, budget, credentials, operation, message, 'TRANSPORT', `Gmail ${operation.requestName} outcome is unknown`, this.#receipt(call));
    }
    const receipt = this.#receipt(call, gmailRequestID(response.response.headers));
    const isAccepted = response.response.status >= 200 && response.response.status < 300;
    const sentResponse = isAccepted ? parseSentResponse(response.response) : undefined;
    if (sentResponse?.id) await recordProviderIdentity(fence, sentResponse.id);
    return this.#classifySendStatus(call, fence, budget, response.credentials, operation, message, response.response.status, response.response.headers, sentResponse, receipt);
  }

  async #classifySendStatus(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, status: number, headers: Record<string, string | string[] | undefined>, sentResponse?: SentMessageIdentity, receipt: ReceiptDraft = this.#receipt(call)): Promise<MutationAttempt<SendMessageOutput>> {
    if (status === 429) {
      await recordSafeResend(fence);
      return retryAfterRefusal(operation.operationId, 'RATE_LIMIT', `Gmail temporarily rejected the ${operation.requestName}`, headers);
    }
    if (status === 503) {
      const lookup = await this.#pollSentMessage(call, budget, credentials);
      if (lookup.sent) return this.#sentMessage(call, budget, credentials, operation, message, lookup.sent, receipt);
      await recordSafeResend(fence);
      return retryAfterRefusal(operation.operationId, 'AVAILABILITY', `Gmail ${operation.requestName} is temporarily unavailable`, headers);
    }
    if (status >= 500) return this.#confirmPossiblySent(call, budget, credentials, operation, message, 'AVAILABILITY', `Gmail ${operation.requestName} outcome is unknown`, receipt);
    if (status < 200 || status >= 300) {
      if (call.context.attempt > 1) {
        try {
          const found = await this.#provider.findSentMessage(call, credentials, this.#messageID(call), budget.attemptSignal());
          if (found) return this.#sentMessage(call, budget, credentials, operation, message, found, receipt);
        } catch { /* A conclusive current refusal still selects its branch. */ }
      }
      return newMutationBranch(operation.providerRejectedBranch, emptySend(), receipt, failure(operation.operationId, statusFailureKind(status), operation.rejectionMessage));
    }
    if (!sentResponse) {
      const lookup = await this.#pollSentMessage(call, budget, credentials);
      return this.#sentMessage(call, budget, credentials, operation, message, lookup.sent ?? { id: '', threadId: message.requestedThreadId }, receipt);
    }
    return this.#sentMessage(call, budget, credentials, operation, message, sentResponse, receipt);
  }

  async #confirmPossiblySent(call: Call, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, kind: Failure['kind'], reason: string, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
    const lookup = await this.#pollSentMessage(call, budget, credentials);
    if (lookup.sent) return this.#sentMessage(call, budget, credentials, operation, message, lookup.sent, receipt);
    const suffix = lookup.hadError ? 'the sent-mail search did not answer' : 'the sent-mail search found no message';
    return newMutationRetry(failure(operation.operationId, kind, `${reason}; ${suffix}`));
  }

  async #sentMessage(call: Call, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, sent: SentMessageIdentity, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
    let threadId = sent.threadId;
    if (!threadId && sent.id) {
      try {
        const read = await this.#provider.readMessage(call, credentials, sent.id, 'minimal', budget.attemptSignal());
        threadId = read.resource?.threadId ?? message.requestedThreadId;
      } catch { threadId = message.requestedThreadId; }
    }
    return newMutationBranch(operation.sentBranch, { sender: credentials.primaryEmail, recipients: message.recipients, messageId: sent.id, threadId }, { ...receipt, providerObjectId: sent.id });
  }

  async #pollSentMessage(call: Call, budget: GmailSendAttemptBudget, credentials: Credentials): Promise<{ sent?: SentMessageIdentity; hadError: boolean }> {
    let hadError = false;
    for (let lookup = 0; lookup < 4; lookup++) {
      if (lookup > 0) {
        try { await delay(75 * (2 ** (lookup - 1)), undefined, { signal: budget.attemptSignal() }); }
        catch { return { hadError: true }; }
      }
      try {
        const found = await this.#provider.findSentMessage(call, credentials, this.#messageID(call), budget.attemptSignal());
        if (found) return { sent: found, hadError: false };
        hadError = false;
      } catch { hadError = true; }
      if (Date.now() >= budget.attemptDeadline || call.context.cancellationSignal.aborted) break;
    }
    return { hadError };
  }

  #messageID(call: Call): string {
    return rfcMessageIDAddress(call.idempotencyKey ?? call.id, call.context.runId, call.context.flowStartedAt);
  }

  #receipt(call: Call, requestId = '', objectId = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'gmail', providerObjectId: objectId, providerRequestId: requestId, observedAt: this.#now().toISOString() };
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'gmail', operation, message }; }
async function recordPreSendRetry(fence: Fence): Promise<void> {
  if (fence.decide().kind !== 'sendAllowed') return;
  try { await fence.recordNotSent(); }
  catch { /* A missing checkpoint cannot authorize another send. */ }
}
async function recordSafeResend(fence: Fence): Promise<void> {
  try { await fence.recordCanResend(); }
  catch { /* A missing checkpoint forces read-back on retry. */ }
}
async function recordProviderIdentity(fence: Fence, providerId: string): Promise<void> {
  try { await fence.recordProviderId(providerId); }
  catch { /* A missing checkpoint forces read-back on retry. */ }
}
function emptySend(): SendMessageOutput { return { sender: '', recipients: [], messageId: '', threadId: '' }; }
function defect(operation: 'sendMessage' | 'replyToMessage', kind: Failure['kind'], message: string): MutationAttempt<SendMessageOutput> { return newMutationBranch('defect', emptySend(), zeroReceipt(), failure(operation, kind, message)); }

function retryAfterRefusal(operation: string, kind: Failure['kind'], message: string, headers: Record<string, string | string[] | undefined>): MutationAttempt<SendMessageOutput> {
  try { return newMutationRetry(failure(operation, kind, message), gmailRetryAfterMs(headers)); }
  catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'Gmail returned an invalid Retry-After header')); }
}

function classifySourceFailure(status: number, headers: Record<string, string | string[] | undefined>, receipt: ReceiptDraft): MutationAttempt<SendMessageOutput> {
  if (status === 0 || status === 429 || status >= 500) return retryAfterRefusal('replyToMessage', 'AVAILABILITY', 'Gmail source message is temporarily unavailable', headers);
  if (status >= 200 && status < 300) return newMutationBranch(ReplyToMessageBranch.InvalidResponse, emptySend(), receipt, failure('replyToMessage', 'PROTOCOL', 'Gmail returned an invalid source message response'));
  return newMutationBranch(ReplyToMessageBranch.ProviderRejected, emptySend(), receipt, failure('replyToMessage', statusFailureKind(status), 'Gmail rejected the source message lookup'));
}
