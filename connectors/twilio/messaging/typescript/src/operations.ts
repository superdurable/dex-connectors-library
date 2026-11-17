// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, Fence, InsufficientTimeError, type Marker } from '@superdurable/connectors-sdk/singledispatch';
import { GetMessageDefinition, SendMessageDefinition } from './generated/definitions.js';
import type { GetMessageInput, Message, SendMessageInput } from './generated/models.js';
import { buildSendRequest, errorCode, retryAfter, twilioFailure, zeroMessage, type TwilioClient, type TwilioExchange } from './client.js';

const messagePattern = /^(SM|MM)[0-9a-f]{32}$/;
// Only these statuses prove Twilio refused a send; other 4xx statuses leave the outcome unknown.
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
function requestedMessage(client: TwilioClient, to: string, sender?: { fromAddress: string; messagingServiceSid: string }): Message {
  return { accountSid: client.accountSid, to,
    createdAt: zeroMessage.createdAt, sentAt: zeroMessage.sentAt, updatedAt: zeroMessage.updatedAt,
    ...(sender?.fromAddress ? { from: sender.fromAddress } : {}), ...(sender?.messagingServiceSid ? { messagingServiceSid: sender.messagingServiceSid } : {}) };
}
function rejection(operation: string, subject: string, status: number, code: number): Failure {
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : 'PROVIDER_REJECTION';
  return twilioFailure(operation, kind, `Twilio rejected the ${subject}${code > 0 ? ` with error ${code}` : ''}`);
}

/** Send one Twilio message behind a durable single-dispatch fence. */
export class SendMessageOperation implements Mutation<SendMessageInput, Message> {
  /** Bind one client. */ constructor(private readonly client: TwilioClient) {}
  /** Stable manifest definition. */ definition() { return SendMessageDefinition; }
  /** Receipt identity only; Twilio does not accept an idempotency header. */ idempotencyKey(callId: string): string { return callId; }
  /**
   * Classify one fenced provider attempt. Twilio accepts no idempotency key, so an unknown outcome
   * retries while possible sends stay below the send cap and selects uncertain at the cap. A saved
   * message SID is read back instead of resent, and a definite refusal is never resent.
   */
  async invoke(call: Call, input: SendMessageInput): Promise<MutationAttempt<Message>> {
    let sendRequest: ReturnType<typeof buildSendRequest>;
    try { sendRequest = buildSendRequest(input, this.client.defaultSender); }
    catch (cause) { return newMutationBranch('defect', zeroMessage, {}, twilioFailure('sendMessage', 'VALIDATION', cause instanceof Error ? cause.message : 'invalid message')); }
    let requested = requestedMessage(this.client, sendRequest.recipient, sendRequest.sender);
    const operationMaxSends = definitionMaxSends(SendMessageDefinition);
    let fence: Fence;
    try { fence = new Fence(call, SendMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000, { maxSends: operationMaxSends }); }
    catch { return newMutationBranch('defect', zeroMessage, {}, twilioFailure('sendMessage', 'LOCAL_DEFECT', 'Twilio dispatch fence could not be initialized')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationRetry(twilioFailure('sendMessage', 'AVAILABILITY', 'Twilio send checkpoint is unreadable; no message was sent in this attempt'));
    // A saved SID proves Twilio created the message, so it is read back and never resent.
    if (decision.kind === 'readBackRequired' || (decision.kind === 'readBackThenResendAllowed' && decision.marker?.providerId))
      return this.readBack(call, requested, decision.marker);
    const priorSendUpperBound = decision.priorSendUpperBound ?? 0;
    // This attempt's send would be the last one the cap allows when no further send remains.
    const canResendUnknownOutcome = priorSendUpperBound + 1 < effectiveMaxSends(call, operationMaxSends);
    // An earlier send may have created the message, so a refusal of a resend proves nothing.
    const hasPossibleEarlierSend = priorSendUpperBound > 0;
    const resendOrSelectUncertain = (kind: Failure['kind'], message: string, response?: ProviderResponse): MutationAttempt<Message> =>
      // The claim stays the last marker, so the next attempt counts this send.
      canResendUnknownOutcome ? newMutationRetry(twilioFailure('sendMessage', kind, message), response ? retryAfter(response, this.client.currentTime()) : 0)
        : newMutationUncertain(requested, twilioFailure('sendMessage', kind, message), this.client.receipt(call, response));
    const auth = await this.client.authorization(call, 'sendMessage');
    if ('failure' in auth) {
      // notSent would hide the earlier possible send, so nothing is recorded.
      if (hasPossibleEarlierSend) return newMutationRetry(auth.failure);
      try { await fence.recordNotSent(); } catch { return newMutationRetry(auth.failure); }
      return auth.isTransient ? newMutationRetry(auth.failure) : newMutationBranch('defect', zeroMessage, {}, auth.failure);
    }
    let exchange: TwilioExchange;
    try {
      exchange = await fence.dispatch(signal => this.client.exchange(call, auth.authorization, 'POST', `${this.client.messagesURL}.json`, sendRequest.form, signal));
    } catch (cause) {
      if (cause instanceof CheckpointError || cause instanceof InsufficientTimeError || cause instanceof AggregateError) return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio dispatch checkpoint or attempt time is unavailable'));
      return resendOrSelectUncertain('TRANSPORT', 'Twilio message outcome is unknown');
    }
    if ('error' in exchange) {
      if (exchange.isProvablyNotSent) {
        try { await fence.recordCanResend(); } catch { return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio could not be reached and its dispatch checkpoint is unavailable')); }
        return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio could not be reached, so no message was sent'));
      }
      return resendOrSelectUncertain('TRANSPORT', 'Twilio message outcome is unknown');
    }
    const response = exchange.response;
    if (response.status >= 200 && response.status < 300) {
      if (exchange.isTooLarge) return resendOrSelectUncertain('RESPONSE_TOO_LARGE', 'Twilio message response exceeds the configured size limit', response);
      if (exchange.isBodyReadFailed) return resendOrSelectUncertain('TRANSPORT', 'Twilio message response was interrupted', response);
      let message: Message;
      try { message = this.client.decodeMessage(response.body, auth.authorization); }
      catch {
        // A returned message SID means Twilio created the message, so a resend could duplicate it.
        if (messagePattern.test(messageSid(response.body))) return newMutationUncertain(requested, twilioFailure('sendMessage', 'PROTOCOL', 'Twilio accepted the request but returned an unusable message'), this.client.receipt(call, response));
        return resendOrSelectUncertain('PROTOCOL', 'Twilio message response is malformed', response);
      }
      try { await fence.recordProviderId(message.sid ?? ''); } catch { /* The accepted provider response determines the branch. */ }
      return newMutationBranch('accepted', message, this.client.receipt(call, response, message.sid));
    }
    if (response.status === 429 || response.status === 503) {
      let checkpointFailed = false; try { await fence.recordCanResend(); } catch { checkpointFailed = true; }
      const message = response.status === 429 ? `Twilio rate limited the message before creating it${checkpointFailed ? '; retry checkpoint is unavailable' : ''}` : checkpointFailed ? 'Twilio service and retry checkpoint are temporarily unavailable' : 'Twilio service is temporarily unavailable';
      return newMutationRetry(twilioFailure('sendMessage', response.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', message), retryAfter(response, this.client.currentTime()));
    }
    if (response.status === 401) {
      const refreshed = await this.client.authorizationAfterRejection(call);
      if ('authorization' in refreshed && (refreshed.authorization.username !== auth.authorization.username || refreshed.authorization.password !== auth.authorization.password)) {
        try { await fence.recordCanResend(); }
        catch { return newMutationRetry(twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio credential changed but retry checkpoint is unavailable')); }
        return newMutationRetry(twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio credential changed after its rejection'));
      }
      const code = errorCode(response.body); requested = { ...requested, ...(code ? { errorCode: code } : {}) };
      return this.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, response, code, twilioFailure('sendMessage', 'AUTHENTICATION', 'failure' in refreshed ? refreshed.failure.message : 'Twilio rejected the message credentials; update the connection'));
    }
    if (definiteRefusalStatuses.has(response.status) && !messagePattern.test(messageSid(response.body))) {
      const code = errorCode(response.body); requested = { ...requested, ...(code ? { errorCode: code } : {}) };
      return this.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, response, code, rejection('sendMessage', 'message', response.status, code));
    }
    return resendOrSelectUncertain('AVAILABILITY', 'Twilio message outcome is unknown', response);
  }

  /** Select providerRejected only when no earlier send of this call may have created the message. */
  private async rejectOrSelectUncertain(call: Call, fence: Fence, hasPossibleEarlierSend: boolean, requested: Message,
    response: ProviderResponse, code: number, refusal: Failure): Promise<MutationAttempt<Message>> {
    const receipt = this.client.receipt(call, response, '', code);
    if (hasPossibleEarlierSend) return newMutationUncertain(requested, refusal, receipt);
    try { await fence.recordCanResend(); } catch { /* The branch is terminal, so a lost canResend checkpoint cannot cause a resend. */ }
    return newMutationBranch('providerRejected', requested, receipt, refusal);
  }

  /** Confirm a saved message SID without resending; an unidentifiable earlier send stays uncertain. */
  private async readBack(call: Call, requested: Message, marker: Marker | undefined): Promise<MutationAttempt<Message>> {
    const providerId = marker?.providerId ?? '';
    if (marker?.callId !== call.id || !messagePattern.test(providerId)) return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio send outcome cannot be confirmed'), this.client.receipt(call));
    const auth = await this.client.authorization(call, 'sendMessage');
    if ('failure' in auth) {
      if (auth.isTransient) return newMutationRetry(auth.failure);
      return newMutationUncertain(requested, twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio accepted message cannot be read; reconnect the connection'), this.client.receipt(call, undefined, providerId));
    }
    const exchange = await this.client.exchange(call, auth.authorization, 'GET', `${this.client.messagesURL}/${providerId}.json`);
    if ('error' in exchange || exchange.isBodyReadFailed || exchange.response.status === 408 || exchange.response.status === 429 || exchange.response.status >= 500) return newMutationRetry(twilioFailure('sendMessage', 'AVAILABILITY', 'Twilio accepted message read-back is temporarily unavailable'));
    if (exchange.response.status !== 200 || exchange.isTooLarge) return newMutationUncertain(requested, twilioFailure('sendMessage', 'PROTOCOL', 'Twilio accepted message could not be confirmed'), this.client.receipt(call, exchange.response, providerId));
    try {
      const message = this.client.decodeMessage(exchange.response.body, auth.authorization);
      if (message.sid !== providerId || message.to !== requested.to) throw new TypeError('read-back mismatch');
      return newMutationBranch('accepted', message, this.client.receipt(call, exchange.response, providerId));
    } catch { return newMutationUncertain(requested, twilioFailure('sendMessage', 'PROTOCOL', 'Twilio accepted message could not be confirmed'), this.client.receipt(call, exchange.response, providerId)); }
  }
}

/** The Fence's own rule: a cap of 1 never resends, and otherwise an application override wins. */
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

/** Return the top-level sid of a response body, or empty when it has none. */
function messageSid(body: Uint8Array): string {
  try {
    const resource: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (typeof resource !== 'object' || resource === null || Array.isArray(resource)) return '';
    const sid = (resource as Record<string, unknown>).sid;
    return typeof sid === 'string' ? sid : '';
  } catch { return ''; }
}

/** Read one Twilio message status. */
export class GetMessageOperation implements Query<GetMessageInput, Message> {
  /** Bind one client. */ constructor(private readonly client: TwilioClient) {}
  /** Stable manifest definition. */ definition() { return GetMessageDefinition; }
  /** Classify one provider read. */
  async invoke(call: Call, input: GetMessageInput): Promise<QueryAttempt<Message>> {
    const sid = input.messageSid?.trim() ?? '';
    if (!messagePattern.test(sid)) return newQueryBranch('defect', zeroMessage, {}, twilioFailure('getMessage', 'VALIDATION', 'message SID must be SM or MM followed by 32 lowercase hexadecimal characters'));
    const auth = await this.client.authorization(call, 'getMessage');
    if ('failure' in auth) return auth.isTransient ? newQueryRetry(auth.failure) : newQueryBranch('defect', zeroMessage, {}, auth.failure);
    const exchange = await this.client.exchange(call, auth.authorization, 'GET', `${this.client.messagesURL}/${sid}.json`);
    if ('error' in exchange) return newQueryRetry(twilioFailure('getMessage', 'TRANSPORT', 'Twilio message status is temporarily unavailable'));
    const response = exchange.response;
    if (response.status >= 200 && response.status < 300) {
      if (exchange.isBodyReadFailed) return newQueryRetry(twilioFailure('getMessage', 'TRANSPORT', 'Twilio message response was interrupted'));
      if (exchange.isTooLarge) return newQueryBranch('invalidResponse', zeroMessage, this.client.receipt(call, response, sid), twilioFailure('getMessage', 'RESPONSE_TOO_LARGE', 'Twilio message response exceeds the configured size limit'));
      try {
        const message = this.client.decodeMessage(response.body, auth.authorization);
        if (message.sid !== sid) throw new TypeError('message SID mismatch');
        return newQueryBranch('found', message, this.client.receipt(call, response, sid));
      } catch { return newQueryBranch('invalidResponse', zeroMessage, this.client.receipt(call, response, sid), twilioFailure('getMessage', 'PROTOCOL', 'Twilio returned an invalid message')); }
    }
    if (response.status === 404) return newQueryBranch('notFound', zeroMessage, this.client.receipt(call, response, sid, errorCode(response.body)), twilioFailure('getMessage', 'NOT_FOUND', 'Twilio has no message with that SID'));
    if (response.status === 429) return newQueryRetry(twilioFailure('getMessage', 'RATE_LIMIT', 'Twilio rate limited the message query'), retryAfter(response, this.client.currentTime()));
    if (response.status === 408 || response.status >= 500) return newQueryRetry(twilioFailure('getMessage', 'AVAILABILITY', 'Twilio message status is temporarily unavailable'));
    if (response.status >= 400 && response.status < 500) {
      const code = errorCode(response.body);
      return newQueryBranch('providerRejected', zeroMessage, this.client.receipt(call, response, sid, code), rejection('getMessage', 'message query', response.status, code));
    }
    return newQueryBranch('invalidResponse', zeroMessage, this.client.receipt(call, response, sid), twilioFailure('getMessage', 'PROTOCOL', 'Twilio returned an unexpected HTTP status'));
  }
}
