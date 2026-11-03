// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CheckpointError, Fence, InsufficientTimeError } from '@superdurable/connectors-sdk/singledispatch';
import { GetMessageDefinition, SendMessageDefinition } from './generated/definitions.js';
import type { GetMessageInput, Message, SendMessageInput } from './generated/models.js';
import { buildSendRequest, errorCode, retryAfter, twilioFailure, zeroMessage, type TwilioClient, type TwilioExchange } from './client.js';

const messagePattern = /^(SM|MM)[0-9a-f]{32}$/;
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
  /** Classify one fenced provider attempt. */
  async invoke(call: Call, input: SendMessageInput): Promise<MutationAttempt<Message>> {
    let fence: Fence;
    try { fence = new Fence(call, SendMessageDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zeroMessage, {}, twilioFailure('sendMessage', 'LOCAL_DEFECT', 'Twilio dispatch fence could not be initialized')); }
    const decision = fence.decide();
    let requested = requestedMessage(this.client, input.to?.trim() ?? '');
    if (decision.kind === 'failClosed') return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio send checkpoint cannot confirm its outcome'), this.client.receipt(call));
    if (decision.kind === 'readBackRequired') {
      const providerId = decision.marker?.providerId ?? '';
      if (decision.marker?.callId !== call.id || !messagePattern.test(providerId)) return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio send outcome cannot be confirmed'), this.client.receipt(call));
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
    let sendRequest: ReturnType<typeof buildSendRequest>;
    try { sendRequest = buildSendRequest(input, this.client.defaultSender); }
    catch (cause) { return newMutationBranch('defect', zeroMessage, {}, twilioFailure('sendMessage', 'VALIDATION', cause instanceof Error ? cause.message : 'invalid message')); }
    requested = requestedMessage(this.client, sendRequest.recipient, sendRequest.sender);
    const auth = await this.client.authorization(call, 'sendMessage');
    if ('failure' in auth) {
      try { await fence.recordNotSent(); } catch { return newMutationRetry(auth.failure); }
      return auth.isTransient ? newMutationRetry(auth.failure) : newMutationBranch('defect', zeroMessage, {}, auth.failure);
    }
    let exchange: TwilioExchange;
    try {
      exchange = await fence.dispatch(signal => this.client.exchange(call, auth.authorization, 'POST', `${this.client.messagesURL}.json`, sendRequest.form, signal));
    } catch (cause) {
      if (cause instanceof CheckpointError || cause instanceof InsufficientTimeError || cause instanceof AggregateError) return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio dispatch checkpoint or attempt time is unavailable'));
      return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio message outcome is unknown'), this.client.receipt(call));
    }
    if ('error' in exchange) {
      if (exchange.isProvablyNotSent) {
        try { await fence.recordCanResend(); } catch { return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio could not be reached and its dispatch checkpoint is unavailable')); }
        return newMutationRetry(twilioFailure('sendMessage', 'TRANSPORT', 'Twilio could not be reached, so no message was sent'));
      }
      return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio message outcome is unknown'), this.client.receipt(call));
    }
    const response = exchange.response;
    if (response.status >= 200 && response.status < 300) {
      if (exchange.isTooLarge) return newMutationUncertain(requested, twilioFailure('sendMessage', 'RESPONSE_TOO_LARGE', 'Twilio accepted the request but its response exceeds the configured size limit'), this.client.receipt(call, response));
      if (exchange.isBodyReadFailed) return newMutationUncertain(requested, twilioFailure('sendMessage', 'TRANSPORT', 'Twilio accepted the request but its response was interrupted'), this.client.receipt(call, response));
      try {
        const message = this.client.decodeMessage(response.body, auth.authorization);
        try { await fence.recordProviderId(message.sid ?? ''); } catch { /* The accepted provider response determines the branch. */ }
        return newMutationBranch('accepted', message, this.client.receipt(call, response, message.sid));
      } catch { return newMutationUncertain(requested, twilioFailure('sendMessage', 'PROTOCOL', 'Twilio accepted the request but returned an unusable message'), this.client.receipt(call, response)); }
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
      return newMutationBranch('providerRejected', requested, this.client.receipt(call, response, '', code), 'failure' in refreshed ? refreshed.failure : twilioFailure('sendMessage', 'AUTHENTICATION', 'Twilio rejected the message credentials; update the connection'));
    }
    if (response.status >= 400 && response.status < 500 && response.status !== 408 && response.status !== 409) {
      const code = errorCode(response.body); requested = { ...requested, ...(code ? { errorCode: code } : {}) };
      return newMutationBranch('providerRejected', requested, this.client.receipt(call, response, '', code), rejection('sendMessage', 'message', response.status, code));
    }
    return newMutationUncertain(requested, twilioFailure('sendMessage', 'AVAILABILITY', 'Twilio message outcome is unknown'), this.client.receipt(call, response));
  }
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
