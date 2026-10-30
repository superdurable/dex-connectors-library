// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newQueryBranch, newQueryRetry, zeroReceipt,
  type Call, type CredentialRefreshDriver, type Failure,
  type Query, type QueryAttempt, type ReceiptDraft,
} from '@superdurable/connectors-sdk';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { GetMessageDefinition, GetMessageBranch } from './definitions.js';
import { GmailCredentialError, GmailHTTPClient, gmailRequestID, gmailRetryAfterMs, statusFailureKind, type GmailTransport } from './http.js';
import { decodeGmailMessage } from './message.js';
import { GetMessageInputSchema, type Config, type CredentialSource, type Credentials, type GetMessageInput, type Message } from './models.js';
import { GmailProvider, type GmailReadResult } from './provider.js';
import { GmailCredentialRefreshDriver } from './refresh.js';
import { GmailSender, ReplyToMessageOperation, SendMessageOperation } from './send.js';
import type { GmailOperations } from './connection.js';

/** Constructor-injected transport and credential refresh collaborators. */
export interface GmailClientOptions {
  /** Provider-specific renewal driver; defaults to Google OAuth and Workspace support. */
  refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Bounded HTTP transport override for provider fakes and controlled gateways. */
  transport?: GmailTransport;
  /** Observation clock for receipts. */
  now?: () => Date;
}

/** Gmail connection with operation-specific provider interfaces. */
export class GmailClient implements GmailOperations {
  readonly #http: GmailHTTPClient;
  readonly #provider: GmailProvider;
  readonly #sender: GmailSender;
  readonly #now: () => Date;

  /** Validate settings and inject a host-owned credential source. */
  constructor(config: Config, credentials: CredentialSource, options: GmailClientOptions = {}) {
    this.#http = new GmailHTTPClient(config, credentials, options.refreshDriver ?? new GmailCredentialRefreshDriver(), options.transport);
    this.#provider = new GmailProvider(this.#http);
    this.#now = options.now ?? (() => new Date());
    this.#sender = new GmailSender(this.#http, this.#provider, this.#now);
  }

  /** Return the getMessage Query bound to this connection. */
  getMessage(): GetMessageOperation { return new GetMessageOperation(this.#http, this.#provider, this.#now); }
  /** Return the sendMessage Mutation bound to this connection. */
  sendMessage(): SendMessageOperation { return new SendMessageOperation(this.#sender); }
  /** Return the replyToMessage Mutation bound to this connection. */
  replyToMessage(): ReplyToMessageOperation { return new ReplyToMessageOperation(this.#sender); }
}

/** Read and decode one Gmail message. */
export class GetMessageOperation implements Query<GetMessageInput, Message> {
  readonly #http: GmailHTTPClient;
  readonly #provider: GmailProvider;
  readonly #now: () => Date;

  /** Inject authenticated transport and observation clock. */
  constructor(http: GmailHTTPClient, provider: GmailProvider, now: () => Date) {
    if (!http || !provider || !now) throw new TypeError('Gmail query dependencies are required');
    this.#http = http; this.#provider = provider; this.#now = now;
  }

  /** Return the immutable manifest operation definition. */
  definition() { return GetMessageDefinition; }

  /** Classify one Gmail message read as a branch or safe retry. */
  async invoke(call: Call, input: GetMessageInput): Promise<QueryAttempt<Message>> {
    if (!GetMessageInputSchema.safeParse(input).success || !input.messageId.trim()) {
      return newQueryBranch(GetMessageBranch.Defect, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', 'VALIDATION', 'message ID is required'));
    }
    let credentials: Credentials;
    try { credentials = await this.#http.resolveCredentials(call, call.context.cancellationSignal); }
    catch { return newQueryBranch(GetMessageBranch.Defect, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    let result: GmailReadResult;
    try { result = await this.#provider.readMessage(call, credentials, input.messageId, 'full', call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof GmailCredentialError) return newQueryBranch(GetMessageBranch.Defect, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', 'AUTHENTICATION', 'connection credentials are unavailable'));
      if (error instanceof BodyTooLargeError && error.status && error.status >= 200 && error.status < 300) {
        return newQueryBranch(GetMessageBranch.InvalidResponse, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', 'PROTOCOL', 'Gmail returned an invalid or oversized message response'));
      }
      const status = error instanceof BodyTooLargeError ? error.status ?? 0 : 0;
      const headers = error instanceof BodyTooLargeError ? error.headers ?? {} : {};
      return classifyReadFailure(status, headers);
    }
    if (!result.resource) return classifyReadFailure(result.response.status, result.response.headers);
    let message: Message;
    try { message = decodeGmailMessage(result.resource); }
    catch {
      return newQueryBranch(GetMessageBranch.InvalidResponse, emptyMessage(), receiptFor(call, this.#now(), gmailRequestID(result.response.headers), input.messageId), gmailFailure('getMessage', 'PROTOCOL', 'Gmail returned an invalid message'));
    }
    return newQueryBranch(GetMessageBranch.Read, message, {
      provider: 'gmail', providerObjectId: message.messageId, providerRequestId: gmailRequestID(result.response.headers), observedAt: this.#now().toISOString(),
    });
  }
}

/** Safe Gmail failure with no request or credential values. */
export function gmailFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'gmail', operation, message };
}

/** Provider-safe receipt from one observed response. */
export function receiptFor(call: Call, observedAt: Date, requestId = '', objectId = ''): ReceiptDraft {
  return {
    callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
    provider: 'gmail', providerObjectId: objectId, providerRequestId: requestId, observedAt: observedAt.toISOString(),
  };
}

/** Go-compatible zero value used on failed operation branches. */
export function emptyMessage(): Message {
  return { messageId: '', threadId: '', from: '', subject: '', receivedAt: '0001-01-01T00:00:00Z' };
}

function classifyReadFailure(status: number, headers: Record<string, string | string[] | undefined>): QueryAttempt<Message> {
  if (status === 429 || status >= 500 || status === 0) {
    let retryAfterMs: number;
    try { retryAfterMs = gmailRetryAfterMs(headers); }
    catch { return newQueryRetry(gmailFailure('getMessage', 'PROTOCOL', 'Gmail returned an invalid Retry-After header')); }
    return newQueryRetry(gmailFailure('getMessage', 'AVAILABILITY', 'Gmail message is temporarily unavailable'), retryAfterMs);
  }
  if (status === 404) return newQueryBranch(GetMessageBranch.NotFound, emptyMessage(), zeroReceipt());
  if (status >= 200 && status < 300) {
    return newQueryBranch(GetMessageBranch.InvalidResponse, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', 'PROTOCOL', 'Gmail returned an invalid or oversized message response'));
  }
  return newQueryBranch(GetMessageBranch.ProviderRejected, emptyMessage(), zeroReceipt(), gmailFailure('getMessage', statusFailureKind(status), 'Gmail message lookup failed'));
}
