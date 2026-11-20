// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, zeroReceipt,
  type Call, type Failure, type Mutation, type MutationAttempt, type ReceiptDraft,
} from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, ProviderStalledError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
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
  messageIdAddress: string;
}

const SendMessageOutcomes: SendOutcomes = {
  operationId: 'sendMessage', requestName: 'send', rejectionMessage: 'Gmail rejected the message',
  sentBranch: SendMessageBranch.Sent, providerRejectedBranch: SendMessageBranch.ProviderRejected,
};
const ReplyToMessageOutcomes: SendOutcomes = {
  operationId: 'replyToMessage', requestName: 'reply', rejectionMessage: 'Gmail rejected the reply',
  sentBranch: ReplyToMessageBranch.Sent, providerRejectedBranch: ReplyToMessageBranch.ProviderRejected,
};
const MissingRunIDMessage = 'the Dex call context has no run ID, so nothing was sent';

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
    const messageIdAddress = this.#messageID(call);
    if (!messageIdAddress) return defect('sendMessage', 'LOCAL_DEFECT', MissingRunIDMessage);
    const fence = newFence(call, SendMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: definitionMaxSends(SendMessageDefinition) });
    if (fence.decide().kind === 'failClosed') return failClosedCheckpointRetry('sendMessage');
    const budget = new GmailSendAttemptBudget(call.context.cancellationSignal, dexDeadline);
    if (!budget.sendSignal()) {
      await recordPreSendRetry(fence);
      return newMutationRetry(failure('sendMessage', 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
    }
    let credentials: Credentials;
    try { credentials = await this.#http.resolveCredentials(call, budget.attemptSignal()); }
    catch (error) { return unavailableCredentials(fence, 'sendMessage', error, this.#receipt(call)); }
    let recipients: string[];
    try {
      if (!SendMessageInputSchema.safeParse(input).success) throw new TypeError();
      recipients = validateSendInput(input, credentials.primaryEmail);
    } catch (error) {
      return defectBeforeAnySend(fence, 'sendMessage', 'VALIDATION', error instanceof TypeError && error.message ? error.message : 'sender, recipients, subject, and text body are required', this.#receipt(call));
    }
    let raw: Uint8Array;
    try {
      raw = buildMIMEMessage(call.id, call.idempotencyKey ?? call.id, messageIdAddress, credentials.primaryEmail, recipients, input.subject, input.textBody, input.htmlBody);
    } catch { return defectBeforeAnySend(fence, 'sendMessage', 'VALIDATION', 'message could not be encoded', this.#receipt(call)); }
    if (raw.length > this.#http.settings().maxMessageBytes) return defectBeforeAnySend(fence, 'sendMessage', 'VALIDATION', 'message exceeds configured size limit', this.#receipt(call));
    return this.#sendAndConfirm(call, fence, budget, credentials, SendMessageOutcomes, { raw, recipients, requestedThreadId: '', hasPreSendSearch: false, messageIdAddress });
  }

  /** Read the source then reply in its Gmail thread under the Dex attempt deadline. */
  async replyToMessage(call: Call, input: ReplyToMessageInput, dexDeadline: Date | undefined): Promise<MutationAttempt<SendMessageOutput>> {
    if (call.context.attempt < 1) return defect('replyToMessage', 'LOCAL_DEFECT', 'Dex Execute attempt number is invalid');
    const messageIdAddress = this.#messageID(call);
    if (!messageIdAddress) return defect('replyToMessage', 'LOCAL_DEFECT', MissingRunIDMessage);
    const fence = newFence(call, ReplyToMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: definitionMaxSends(ReplyToMessageDefinition) });
    if (fence.decide().kind === 'failClosed') return failClosedCheckpointRetry('replyToMessage');
    const budget = new GmailSendAttemptBudget(call.context.cancellationSignal, dexDeadline);
    if (!budget.sendSignal()) {
      await recordPreSendRetry(fence);
      return newMutationRetry(failure('replyToMessage', 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
    }
    let credentials: Credentials;
    try { credentials = await this.#http.resolveCredentials(call, budget.attemptSignal()); }
    catch (error) { return unavailableCredentials(fence, 'replyToMessage', error, this.#receipt(call)); }
    if (!ReplyToMessageInputSchema.safeParse(input).success || !input.messageId.trim() || !input.textBody.trim()) return defectBeforeAnySend(fence, 'replyToMessage', 'VALIDATION', 'message ID and text body are required', this.#receipt(call));
    const priorProviderId = fence.decide().marker?.providerId;
    const hasSavedProviderId = isReadBackDecision(fence) && !!priorProviderId;
    if (hasSavedProviderId) {
      const saved = await this.#readMessageById(call, budget, credentials, priorProviderId!);
      if (saved) {
        return this.#sentMessage(call, budget, credentials, ReplyToMessageOutcomes,
          { raw: new Uint8Array(), recipients: [], requestedThreadId: '', hasPreSendSearch: true, messageIdAddress },
          saved, this.#receipt(call, '', priorProviderId));
      }
    }
    if (call.context.attempt > 1 || fence.decide().kind === 'readBackRequired') {
      let hasSearchFailed = false;
      try {
        const found = await this.#provider.findSentMessage(call, credentials, messageIdAddress, budget.preSendSignal());
        if (found) return this.#sentMessage(call, budget, credentials, ReplyToMessageOutcomes, { raw: new Uint8Array(), recipients: [], requestedThreadId: '', hasPreSendSearch: true, messageIdAddress }, found, this.#receipt(call));
      } catch {
        hasSearchFailed = true;
        // An inconclusive search still permits a resend below the send cap.
        if (fence.decide().kind === 'sendAllowed') {
          await recordPreSendRetry(fence);
          return newMutationRetry(failure('replyToMessage', 'AVAILABILITY', 'Gmail sent-mail search did not answer before the reply'));
        }
      }
      // A saved provider ID proves Gmail accepted an earlier reply, so it is never resent.
      if (fence.decide().kind === 'readBackRequired' || hasSavedProviderId) return uncertainWithoutResend(ReplyToMessageOutcomes, hasSearchFailed, this.#receipt(call, '', priorProviderId ?? ''));
    }
    let source;
    try { source = await this.#provider.readMessage(call, credentials, input.messageId, 'metadata', budget.preSendSignal()); }
    catch (error) {
      if (error instanceof GmailCredentialError) return defectBeforeAnySend(fence, 'replyToMessage', 'AUTHENTICATION', 'connection credentials are unavailable', this.#receipt(call));
      const status = error instanceof BodyTooLargeError ? error.status ?? 0 : 0;
      const headers = error instanceof BodyTooLargeError ? error.headers ?? {} : {};
      const result = classifySourceFailure(call, fence, status, headers, this.#receipt(call));
      if (result.kind === 'retry') await recordPreSendRetry(fence);
      return result;
    }
    credentials = source.credentials;
    if (!source.resource) {
      const result = classifySourceFailure(call, fence, source.response.status, source.response.headers, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId));
      if (result.kind === 'retry') await recordPreSendRetry(fence);
      return result;
    }
    let message: Message;
    try { message = decodeGmailMessage(source.resource); }
    catch { return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.InvalidResponse, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'source message lacks reply metadata')); }
    if (!message.threadId || !message.rfcMessageId) return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.InvalidResponse, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'source message lacks reply metadata'));
    let recipient: string;
    try { recipient = replyRecipient(message); }
    catch { return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.InvalidResponse, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'Gmail returned an invalid source sender')); }
    if (/[\r\n]/.test(message.subject + message.rfcMessageId + (message.references ?? ''))) return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.InvalidResponse, this.#receipt(call, gmailRequestID(source.response.headers), input.messageId), failure('replyToMessage', 'PROTOCOL', 'Gmail returned invalid source reply headers'));
    let raw: Uint8Array;
    try { raw = buildReplyMIMEMessage(call.id, call.idempotencyKey ?? call.id, messageIdAddress, credentials.primaryEmail, message, input); }
    catch { return defectBeforeAnySend(fence, 'replyToMessage', 'VALIDATION', 'reply could not be encoded within the configured limit', this.#receipt(call)); }
    if (raw.length > this.#http.settings().maxMessageBytes) return defectBeforeAnySend(fence, 'replyToMessage', 'VALIDATION', 'reply could not be encoded within the configured limit', this.#receipt(call));
    return this.#sendAndConfirm(call, fence, budget, credentials, ReplyToMessageOutcomes, { raw, recipients: [recipient], requestedThreadId: message.threadId, hasPreSendSearch: call.context.attempt > 1, messageIdAddress });
  }

  async #sendAndConfirm(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage): Promise<MutationAttempt<SendMessageOutput>> {
    const priorProviderId = fence.decide().marker?.providerId;
    const hasSavedProviderId = isReadBackDecision(fence) && !!priorProviderId;
    if (hasSavedProviderId && !message.hasPreSendSearch) {
      const saved = await this.#readMessageById(call, budget, credentials, priorProviderId!);
      if (saved) return this.#sentMessage(call, budget, credentials, operation, message, saved, this.#receipt(call, '', priorProviderId));
    }
    let hasSearchFailed = false;
    if ((call.context.attempt > 1 || fence.decide().kind === 'readBackRequired') && !message.hasPreSendSearch) {
      try {
        const found = await this.#provider.findSentMessage(call, credentials, message.messageIdAddress, budget.preSendSignal());
        if (found) return this.#sentMessage(call, budget, credentials, operation, message, found, this.#receipt(call));
      } catch {
        hasSearchFailed = true;
        // An inconclusive search still permits a resend below the send cap.
        if (fence.decide().kind === 'sendAllowed') {
          await recordPreSendRetry(fence);
          return newMutationRetry(failure(operation.operationId, 'AVAILABILITY', `Gmail sent-mail search did not answer before the ${operation.requestName}`));
        }
      }
    }
    // A saved provider ID proves Gmail accepted an earlier send, so it is never resent.
    if (fence.decide().kind === 'readBackRequired' || hasSavedProviderId) return uncertainWithoutResend(operation, hasSearchFailed, this.#receipt(call, '', priorProviderId ?? ''));
    let response;
    try { response = await fence.dispatch(signal => this.#provider.sendMessage(call, credentials, message.raw, message.requestedThreadId, signal)); }
    catch (error) {
      if (error instanceof InsufficientTimeError || error instanceof CheckpointError) return newMutationRetry(failure(operation.operationId, 'AVAILABILITY', 'too little of the Execute attempt is left to send safely, so nothing was sent'));
      if (error instanceof ProviderTransportError && error.isProvablyNotSent || error instanceof AddressRefusal) {
        await recordSafeResend(fence);
        return newMutationRetry(failure(operation.operationId, 'TRANSPORT', `Gmail ${operation.requestName} request was not dispatched`));
      }
      // Headers that arrived before the body failed keep their status, so an accepted send is never resent.
      if ((error instanceof BodyTooLargeError || error instanceof ProviderTransportError || error instanceof ProviderStalledError) && error.status) {
        return this.#classifySendStatus(call, fence, budget, credentials, operation, message, error.status, error.headers ?? {}, false, undefined);
      }
      return this.#confirmPossiblySent(call, fence, budget, credentials, operation, message, 'TRANSPORT', `Gmail ${operation.requestName} outcome is unknown`, this.#receipt(call));
    }
    const receipt = this.#receipt(call, gmailRequestID(response.response.headers));
    const isAccepted = response.response.status >= 200 && response.response.status < 300;
    const sentResponse = isAccepted ? parseSentResponse(response.response) : undefined;
    if (sentResponse?.id) await recordProviderIdentity(fence, sentResponse.id);
    // A refusal body that still names a message contradicts its status.
    const refusalNamedMessage = isAccepted ? undefined : parseSentResponse(response.response);
    const isRefusalBodyClean = !isAccepted && refusalNamedMessage === undefined;
    return this.#classifySendStatus(call, fence, budget, response.credentials, operation, message, response.response.status, response.response.headers, isRefusalBodyClean, sentResponse, receipt, refusalNamedMessage);
  }

  async #classifySendStatus(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, status: number, headers: Record<string, string | string[] | undefined>, isRefusalBodyClean: boolean, sentResponse?: SentMessageIdentity, receipt: ReceiptDraft = this.#receipt(call), refusalNamedMessage?: SentMessageIdentity): Promise<MutationAttempt<SendMessageOutput>> {
    if (status === 429) {
      await recordSafeResend(fence);
      return retryAfterRefusal(operation.operationId, 'RATE_LIMIT', `Gmail temporarily rejected the ${operation.requestName}`, headers);
    }
    if (status === 503) {
      // Gmail documents 5xx as processing failures and Sent Mail search can lag, so the claim stays.
      const lookup = await this.#pollSentMessage(call, budget, credentials, message);
      if (lookup.sent) return this.#sentAfterDispatch(call, fence, budget, credentials, operation, message, lookup.sent, receipt);
      return retryAfterRefusal(operation.operationId, 'AVAILABILITY', `Gmail ${operation.requestName} is temporarily unavailable`, headers);
    }
    if (status >= 500) return this.#confirmPossiblySent(call, fence, budget, credentials, operation, message, 'AVAILABILITY', `Gmail ${operation.requestName} outcome is unknown`, receipt);
    if (status < 200 || status >= 300) {
      if (refusalNamedMessage) return this.#confirmRefusalNamingMessage(call, fence, budget, credentials, operation, message, refusalNamedMessage.id, receipt);
      if (call.context.attempt > 1) {
        try {
          const found = await this.#provider.findSentMessage(call, credentials, message.messageIdAddress, budget.attemptSignal());
          if (found) return this.#sentMessage(call, budget, credentials, operation, message, found, receipt);
        } catch { /* A conclusive current refusal still selects its branch. */ }
      }
      return rejectRefusedSend(call, fence, refusedBeforeProcessingStatuses.has(status) && isRefusalBodyClean, operation.providerRejectedBranch, receipt, failure(operation.operationId, statusFailureKind(status), operation.rejectionMessage));
    }
    if (!sentResponse) {
      await recordAcceptedWithoutIdentity(fence);
      const lookup = await this.#pollSentMessage(call, budget, credentials, message);
      if (lookup.sent) return this.#sentAfterDispatch(call, fence, budget, credentials, operation, message, lookup.sent, receipt);
      return this.#sentMessage(call, budget, credentials, operation, message, { id: '', threadId: message.requestedThreadId }, receipt);
    }
    return this.#sentMessage(call, budget, credentials, operation, message, sentResponse, receipt);
  }

  async #confirmPossiblySent(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, kind: Failure['kind'], reason: string, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
    const lookup = await this.#pollSentMessage(call, budget, credentials, message);
    if (lookup.sent) return this.#sentAfterDispatch(call, fence, budget, credentials, operation, message, lookup.sent, receipt);
    const suffix = lookup.hadError ? 'the sent-mail search did not answer' : 'the sent-mail search found no message';
    return resendOrSelectUncertain(call, fence, failure(operation.operationId, kind, `${reason}; ${suffix}`), receipt);
  }

  /** Read back a message a refusal body names; the saved ID makes later attempts read back only, never resend. */
  async #confirmRefusalNamingMessage(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, providerId: string, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
    await recordProviderIdentity(fence, providerId);
    const confirmed = await this.#readMessageById(call, budget, credentials, providerId);
    if (confirmed) return this.#sentMessage(call, budget, credentials, operation, message, confirmed, receipt);
    return newMutationRetry(failure(operation.operationId, 'PROTOCOL', `Gmail refused the ${operation.requestName} but named a message that read-back has not confirmed`));
  }

  /** Return the message with this ID, or undefined when Gmail does not confirm it. */
  async #readMessageById(call: Call, budget: GmailSendAttemptBudget, credentials: Credentials, providerId: string): Promise<SentMessageIdentity | undefined> {
    try {
      const read = await this.#provider.readMessage(call, credentials, providerId, 'minimal', budget.attemptSignal());
      return read.resource?.id === providerId ? { id: providerId, threadId: read.resource.threadId ?? '' } : undefined;
    } catch { return undefined; } // An unconfirmed read-back never resends; the caller retries.
  }

  async #sentAfterDispatch(call: Call, fence: Fence, budget: GmailSendAttemptBudget, credentials: Credentials, operation: SendOutcomes, message: OutgoingMessage, sent: SentMessageIdentity, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
    if (sent.id) await recordProviderIdentity(fence, sent.id);
    return this.#sentMessage(call, budget, credentials, operation, message, sent, receipt);
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

  async #pollSentMessage(call: Call, budget: GmailSendAttemptBudget, credentials: Credentials, message: OutgoingMessage): Promise<{ sent?: SentMessageIdentity; hadError: boolean }> {
    let hadError = false;
    for (let lookup = 0; lookup < 4; lookup++) {
      if (lookup > 0) {
        try { await delay(75 * (2 ** (lookup - 1)), undefined, { signal: budget.attemptSignal() }); }
        catch { return { hadError: true }; }
      }
      try {
        const found = await this.#provider.findSentMessage(call, credentials, message.messageIdAddress, budget.attemptSignal());
        if (found) return { sent: found, hadError: false };
        hadError = false;
      } catch { hadError = true; }
      if (Date.now() >= budget.attemptDeadline || call.context.cancellationSignal.aborted) break;
    }
    return { hadError };
  }

  #messageID(call: Call): string | undefined {
    // AsyncContext types both fields as required, but dex-agents tool Step contexts omit flowStartedAt.
    const { runId, flowStartedAt } = call.context as { runId?: unknown; flowStartedAt?: Date | null };
    if (typeof runId !== 'string' || !runId) return undefined;
    return rfcMessageIDAddress(call.idempotencyKey ?? call.id, runId, flowStartedAt);
  }

  #receipt(call: Call, requestId = '', objectId = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'gmail', providerObjectId: objectId, providerRequestId: requestId, observedAt: this.#now().toISOString() };
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'gmail', operation, message }; }
// A saved provider ID proves Gmail accepted an earlier send, so neither read-back decision resends it.
function isReadBackDecision(fence: Fence): boolean {
  const kind = fence.decide().kind;
  return kind === 'readBackRequired' || kind === 'readBackThenResendAllowed';
}
/** Exclude this attempt's send while retaining earlier possible sends. */
async function recordPreSendRetry(fence: Fence): Promise<void> {
  const decision = fence.decide();
  if (decision.kind !== 'sendAllowed' && decision.kind !== 'readBackThenResendAllowed') return;
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
async function recordAcceptedWithoutIdentity(fence: Fence): Promise<void> {
  try { await fence.recordAccepted(); }
  catch { /* A missing checkpoint keeps the claim and prevents an unbounded resend. */ }
}
function emptySend(): SendMessageOutput { return { sender: '', recipients: [], messageId: '', threadId: '' }; }
function defect(operation: 'sendMessage' | 'replyToMessage', kind: Failure['kind'], message: string): MutationAttempt<SendMessageOutput> { return newMutationBranch('defect', emptySend(), zeroReceipt(), failure(operation, kind, message)); }

function retryAfterRefusal(operation: string, kind: Failure['kind'], message: string, headers: Record<string, string | string[] | undefined>): MutationAttempt<SendMessageOutput> {
  try { return newMutationRetry(failure(operation, kind, message), gmailRetryAfterMs(headers)); }
  catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'Gmail returned an invalid Retry-After header')); }
}

/**
 * Classify a credential failure before any send in this attempt. After an earlier possible send, a transient
 * failure returns Retry without notSent, so later attempts still read back, and one that needs reconnection
 * selects uncertain, because no later attempt could read back or resend. On a clean first attempt a transient
 * failure records notSent and returns Retry; only a permanent one selects defect.
 */
async function unavailableCredentials(fence: Fence, operation: 'sendMessage' | 'replyToMessage', error: unknown, receipt: ReceiptDraft): Promise<MutationAttempt<SendMessageOutput>> {
  const isPermanent = !(error instanceof GmailCredentialError) || error.isPermanent;
  if (isEarlierSendPossible(fence)) {
    if (isPermanent) return newMutationUncertain(emptySend(), failure(operation, 'AUTHENTICATION', 'Gmail credentials need reconnection; an earlier send may have been delivered'), receipt);
    return newMutationRetry(failure(operation, 'AUTHENTICATION', 'Gmail credentials are temporarily unavailable; an earlier send may still be delivered'));
  }
  if (!isPermanent) {
    await recordPreSendRetry(fence);
    return newMutationRetry(failure(operation, 'AVAILABILITY', 'Gmail credentials are temporarily unavailable; nothing was sent'));
  }
  return defect(operation, 'AUTHENTICATION', 'connection credentials are unavailable');
}

/**
 * Select defect only when no earlier attempt may have sent; otherwise that send may still be delivered, and
 * invalid input cannot resend it, so it selects uncertain.
 */
function defectBeforeAnySend(fence: Fence, operation: 'sendMessage' | 'replyToMessage', kind: Failure['kind'], message: string, receipt: ReceiptDraft): MutationAttempt<SendMessageOutput> {
  if (isEarlierSendPossible(fence)) return newMutationUncertain(emptySend(), failure(operation, kind, `${message}; an earlier send may still be delivered`), receipt);
  return defect(operation, kind, message);
}

/** The send cap the Fence applies: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operation: string): number {
  const operationMaxSends = definitionMaxSends(operation === SendMessageOutcomes.operationId ? SendMessageDefinition : ReplyToMessageDefinition);
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/**
 * Retry a possibly sent message while the send cap permits a send after this attempt's; the next attempt reads
 * back Sent Mail first. At the cap it selects uncertain.
 */
function resendOrSelectUncertain(call: Call, fence: Fence, unknown: Failure, receipt: ReceiptDraft): MutationAttempt<SendMessageOutput> {
  const canResendUnknownOutcome = (fence.decide().priorSendUpperBound ?? 0) + 1 < effectiveMaxSends(call, unknown.operation);
  if (canResendUnknownOutcome) return newMutationRetry(unknown);
  return newMutationUncertain(emptySend(), unknown, receipt);
}

/** Select uncertain when the send cap or a saved message ID forbids another send. */
function uncertainWithoutResend(operation: SendOutcomes, hasSearchFailed: boolean, receipt: ReceiptDraft): MutationAttempt<SendMessageOutput> {
  const reason = hasSearchFailed ? 'did not answer' : 'found no message';
  return newMutationUncertain(emptySend(), failure(operation.operationId, 'AVAILABILITY', `Gmail sent-mail search ${reason}, and no further ${operation.requestName} is permitted`), receipt);
}

// An unreadable or untrusted checkpoint may hide an earlier send, so the outcome repeats.
function failClosedCheckpointRetry(operation: 'sendMessage' | 'replyToMessage'): MutationAttempt<SendMessageOutput> {
  return newMutationRetry(failure(operation, 'LOCAL_DEFECT', 'Dex send checkpoint is unreadable or untrusted; an earlier send may still be delivered'));
}

function isEarlierSendPossible(fence: Fence): boolean {
  return (fence.decide().priorSendUpperBound ?? 0) > 0;
}

/**
 * Select a rejection branch only when no earlier attempt may have sent. Otherwise that send may still be
 * delivered: a definite refusal proves only that this attempt failed, so it selects uncertain; an indefinite
 * one, which only the source read before any POST produces, Retries while earlier sends stay below the send
 * cap and selects uncertain at it.
 */
function rejectUnlessEarlierSendIsUnconfirmed(call: Call, fence: Fence, branch: string, receipt: ReceiptDraft, rejection: Failure, isDefinite = false): MutationAttempt<SendMessageOutput> {
  if (!isEarlierSendPossible(fence)) return newMutationBranch(branch, emptySend(), receipt, rejection);
  const unknown = { ...rejection, message: `${rejection.message}; an earlier send may still be delivered` };
  if (isDefinite) return newMutationUncertain(emptySend(), unknown, receipt);
  // This attempt sent nothing, so only earlier sends count against the cap.
  if ((fence.decide().priorSendUpperBound ?? 0) < effectiveMaxSends(call, unknown.operation)) return newMutationRetry(unknown);
  return newMutationUncertain(emptySend(), unknown, receipt);
}

// Statuses that prove Gmail refused the POST before processing it.
const refusedBeforeProcessingStatuses: ReadonlySet<number> = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/**
 * Classify a definite Gmail refusal of this attempt's POST. With no earlier possible send, canResend
 * proves nothing was sent, so agent hosts report failure instead of an unknown outcome. Only a status in
 * `refusedBeforeProcessingStatuses` with a readable body naming no message proves that; after an earlier
 * possible send it selects uncertain. Any other refusal,
 * such as 408 or 409, leaves the outcome unknown: below the send cap Retry keeps the claim, so the next attempt
 * reads back; at the cap it selects uncertain.
 */
async function rejectRefusedSend(call: Call, fence: Fence, isDefiniteRefusal: boolean, branch: string, receipt: ReceiptDraft, rejection: Failure): Promise<MutationAttempt<SendMessageOutput>> {
  if (!isDefiniteRefusal) return resendOrSelectUncertain(call, fence, { ...rejection, message: `${rejection.message}; Gmail may still have processed the request` }, receipt);
  if ((fence.decide().priorSendUpperBound ?? 0) === 0) await recordSafeResend(fence);
  return rejectUnlessEarlierSendIsUnconfirmed(call, fence, branch, receipt, rejection, true);
}

function classifySourceFailure(call: Call, fence: Fence, status: number, headers: Record<string, string | string[] | undefined>, receipt: ReceiptDraft): MutationAttempt<SendMessageOutput> {
  if (status === 0 || status === 429 || status >= 500) return retryAfterRefusal('replyToMessage', 'AVAILABILITY', 'Gmail source message is temporarily unavailable', headers);
  if (status >= 200 && status < 300) return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.InvalidResponse, receipt, failure('replyToMessage', 'PROTOCOL', 'Gmail returned an invalid source message response'));
  return rejectUnlessEarlierSendIsUnconfirmed(call, fence, ReplyToMessageBranch.ProviderRejected, receipt, failure('replyToMessage', statusFailureKind(status), 'Gmail rejected the source message lookup'), refusedBeforeProcessingStatuses.has(status));
}
