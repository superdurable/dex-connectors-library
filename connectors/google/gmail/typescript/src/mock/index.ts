// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Failure, FailureKind } from '@superdurable/connectors-sdk';
import { branchCase, newMutation, newQuery, retryCase, type Case, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import {
  GetMessageBranch, GetMessageDefinition, GmailConnection, ReplyToMessageBranch, ReplyToMessageDefinition, SendMessageBranch, SendMessageDefinition,
  MessageSchema, SendMessageOutputSchema,
  type GetMessageInput, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput,
} from '../index.js';

const EmptyMessage: Message = {
  messageId: '', threadId: '', from: '', subject: '', receivedAt: '0001-01-01T00:00:00Z',
};
const EmptySend: SendMessageOutput = { sender: '', recipients: [], messageId: '', threadId: '' };

/** Scripted Gmail connection with the manifest's happy-path defaults. */
export class GmailMock {
  readonly #getMessage: MockQuery<GetMessageInput, Message>;
  readonly #sendMessage: MockMutation<SendMessageInput, SendMessageOutput>;
  readonly #replyToMessage: MockMutation<ReplyToMessageInput, SendMessageOutput>;
  readonly #connection: GmailConnection;

  /** Construct all three operation scripts under one static connection name. */
  constructor(connectionName: string) {
    if (!connectionName.trim()) throw new TypeError('Gmail mock connection name is required');
    this.#getMessage = newQuery<GetMessageInput, Message>(GetMessageDefinition).default(getMessageDefault());
    this.#sendMessage = newMutation<SendMessageInput, SendMessageOutput>(SendMessageDefinition).default(sendMessageDefault());
    this.#replyToMessage = newMutation<ReplyToMessageInput, SendMessageOutput>(ReplyToMessageDefinition).default(replyToMessageDefault());
    this.#connection = new GmailConnection(this, { provider: 'google', name: connectionName });
  }

  /** Return the typed connection for Gmail Step factories. */
  connection(): GmailConnection { return this.#connection; }
  /** Script getMessage calls. */
  getMessage(): MockQuery<GetMessageInput, Message> { return this.#getMessage; }
  /** Script sendMessage calls. */
  sendMessage(): MockMutation<SendMessageInput, SendMessageOutput> { return this.#sendMessage; }
  /** Script replyToMessage calls. */
  replyToMessage(): MockMutation<ReplyToMessageInput, SendMessageOutput> { return this.#replyToMessage; }
}

function failure(operation: string, kind: FailureKind, message: string): Failure {
  return { kind, provider: 'gmail', operation, message };
}

/** Script an arbitrary getMessage branch. */
export function getMessageBranch(branch: string, value: Message, reason?: Failure): Case<Message> { return branchCase(branch, value, reason); }
/** Script an arbitrary getMessage retry. */
export function getMessageRetry(reason: Failure, afterMs = 0): Case<Message> { return retryCase(reason, afterMs); }
/** The manifest's customerQuestion read scenario. */
export function getMessageCustomerQuestion(): Case<Message> {
  return branchCase(GetMessageBranch.Read, MessageSchema.parse({
    messageId: '18c0a1b2c3d4e5f6', threadId: '18c0a1b2c3d4e5f0', rfcMessageId: '<question-1@mail.example.com>',
    inReplyTo: '<welcome-1@mail.example.com>', references: '<welcome-1@mail.example.com>',
    from: 'Ada Customer <ada@example.com>', replyTo: 'ada.support@example.com', to: ['support@example.org'],
    subject: 'Re: Order 1042 has not arrived', textBody: 'Hello, my order 1042 has not arrived yet. Can you check?',
    htmlBody: '<p>Hello, my order 1042 has not arrived yet. Can you check?</p>',
    snippet: 'Hello, my order 1042 has not arrived yet.', receivedAt: '2026-03-04T09:15:00Z', labelIds: ['INBOX', 'UNREAD'],
  }));
}
/** The manifest's deletedMessage branch. */
export function getMessageDeletedMessage(): Case<Message> { return branchCase(GetMessageBranch.NotFound, EmptyMessage, failure('getMessage', 'NOT_FOUND', 'The Gmail message does not exist.')); }
/** The manifest's revokedGrant branch. */
export function getMessageRevokedGrant(): Case<Message> { return branchCase(GetMessageBranch.ProviderRejected, EmptyMessage, failure('getMessage', 'AUTHORIZATION', 'Gmail rejected the message query for the granted scopes.')); }
/** The manifest's malformedPayload branch. */
export function getMessageMalformedPayload(): Case<Message> { return branchCase(GetMessageBranch.InvalidResponse, EmptyMessage, failure('getMessage', 'PROTOCOL', 'Gmail returned an invalid message payload.')); }
/** The manifest's emptyMessageID branch. */
export function getMessageEmptyMessageID(): Case<Message> { return branchCase(GetMessageBranch.Defect, EmptyMessage, failure('getMessage', 'VALIDATION', 'The message ID is required.')); }
/** The manifest's rateLimited retry. */
export function getMessageRateLimited(): Case<Message> { return retryCase(failure('getMessage', 'RATE_LIMIT', 'Gmail rate limit was reached.'), 1000); }
/** The manifest's default getMessage scenario. */
export function getMessageDefault(): Case<Message> { return getMessageCustomerQuestion(); }

/** Script an arbitrary sendMessage branch. */
export function sendMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary sendMessage retry. */
export function sendMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's sentToCustomer scenario. */
export function sendMessageSentToCustomer(): Case<SendMessageOutput> { return branchCase(SendMessageBranch.Sent, SendMessageOutputSchema.parse({ sender: 'support@example.org', recipients: ['ada@example.com'], messageId: '18c0a1b2c3d4e600', threadId: '18c0a1b2c3d4e600' })); }
/** The manifest's invalidRecipient branch. */
export function sendMessageInvalidRecipient(): Case<SendMessageOutput> { return branchCase(SendMessageBranch.ProviderRejected, EmptySend, failure('sendMessage', 'PROVIDER_REJECTION', 'Gmail rejected the message recipients.')); }
/** The manifest's connectionLost retry. */
export function sendMessageConnectionLost(): Case<SendMessageOutput> { return retryCase(failure('sendMessage', 'TRANSPORT', 'The connection closed before Gmail answered.')); }
/** The manifest's noRecipients defect. */
export function sendMessageNoRecipients(): Case<SendMessageOutput> { return branchCase(SendMessageBranch.Defect, EmptySend, failure('sendMessage', 'VALIDATION', 'At least one recipient is required.')); }
/** The manifest's unavailable retry. */
export function sendMessageUnavailable(): Case<SendMessageOutput> { return retryCase(failure('sendMessage', 'AVAILABILITY', 'Gmail is temporarily unavailable.')); }
/** The manifest's default sendMessage scenario. */
export function sendMessageDefault(): Case<SendMessageOutput> { return sendMessageSentToCustomer(); }

/** Script an arbitrary replyToMessage branch. */
export function replyToMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary replyToMessage retry. */
export function replyToMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's repliedInThread scenario. */
export function replyToMessageRepliedInThread(): Case<SendMessageOutput> { return branchCase(ReplyToMessageBranch.Sent, SendMessageOutputSchema.parse({ sender: 'support@example.org', recipients: ['ada.support@example.com'], messageId: '18c0a1b2c3d4e700', threadId: '18c0a1b2c3d4e5f0' })); }
/** The manifest's sourceMissing branch. */
export function replyToMessageSourceMissing(): Case<SendMessageOutput> { return branchCase(ReplyToMessageBranch.ProviderRejected, EmptySend, failure('replyToMessage', 'NOT_FOUND', 'The source Gmail message does not exist.')); }
/** The manifest's malformedSource branch. */
export function replyToMessageMalformedSource(): Case<SendMessageOutput> { return branchCase(ReplyToMessageBranch.InvalidResponse, EmptySend, failure('replyToMessage', 'PROTOCOL', 'Gmail returned an invalid source message.')); }
/** The manifest's connectionLost retry. */
export function replyToMessageConnectionLost(): Case<SendMessageOutput> { return retryCase(failure('replyToMessage', 'TRANSPORT', 'The connection closed before Gmail answered.')); }
/** The manifest's emptyBody defect. */
export function replyToMessageEmptyBody(): Case<SendMessageOutput> { return branchCase(ReplyToMessageBranch.Defect, EmptySend, failure('replyToMessage', 'VALIDATION', 'The reply body is required.')); }
/** The manifest's default replyToMessage scenario. */
export function replyToMessageDefault(): Case<SendMessageOutput> { return replyToMessageRepliedInThread(); }
