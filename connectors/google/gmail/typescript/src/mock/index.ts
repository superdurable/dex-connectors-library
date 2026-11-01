// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Failure } from '@superdurable/connectors-sdk';
import { branchCase, newMutation, newQuery, retryCase, uncertainCase, type Case, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import {
  GmailConnection, SendMessageOutputSchema, type GetMessageInput, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput,
} from '../index.js';
import { GetMessageDefinition, ReplyToMessageDefinition, SendMessageDefinition } from '../definitions.js';
import type { SendMessageOutput as GeneratedSendMessageOutput } from '../generated/models.js';
import * as generatedMock from '../generated/mock.js';

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

function publicSendCase(scripted: Case<GeneratedSendMessageOutput>): Case<SendMessageOutput> {
  if (scripted.kind === 'retry') return retryCase(scripted.failure, scripted.retryAfterMs);
  const value = SendMessageOutputSchema.parse(scripted.value);
  if (scripted.kind === 'uncertain') return uncertainCase(value, scripted.failure);
  return branchCase(scripted.branch, value, scripted.failure);
}

/** Script an arbitrary getMessage branch. */
export function getMessageBranch(branch: string, value: Message, reason?: Failure): Case<Message> { return branchCase(branch, value, reason); }
/** Script an arbitrary getMessage retry. */
export function getMessageRetry(reason: Failure, afterMs = 0): Case<Message> { return retryCase(reason, afterMs); }
/** The manifest's customerQuestion read scenario. */
export function getMessageCustomerQuestion(): Case<Message> {
  return generatedMock.getMessageCustomerQuestion();
}
/** The manifest's deletedMessage branch. */
export function getMessageDeletedMessage(): Case<Message> { return generatedMock.getMessageDeletedMessage(); }
/** The manifest's revokedGrant branch. */
export function getMessageRevokedGrant(): Case<Message> { return generatedMock.getMessageRevokedGrant(); }
/** The manifest's malformedPayload branch. */
export function getMessageMalformedPayload(): Case<Message> { return generatedMock.getMessageMalformedPayload(); }
/** The manifest's emptyMessageID branch. */
export function getMessageEmptyMessageID(): Case<Message> { return generatedMock.getMessageEmptyMessageID(); }
/** The manifest's rateLimited retry. */
export function getMessageRateLimited(): Case<Message> { return generatedMock.getMessageRateLimited(); }
/** The manifest's default getMessage scenario. */
export function getMessageDefault(): Case<Message> { return generatedMock.getMessageDefault(); }

/** Script an arbitrary sendMessage branch. */
export function sendMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary sendMessage retry. */
export function sendMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's sentToCustomer scenario. */
export function sendMessageSentToCustomer(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageSentToCustomer()); }
/** The manifest's invalidRecipient branch. */
export function sendMessageInvalidRecipient(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageInvalidRecipient()); }
/** The manifest's connectionLost retry. */
export function sendMessageConnectionLost(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageConnectionLost()); }
/** The manifest's noRecipients defect. */
export function sendMessageNoRecipients(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageNoRecipients()); }
/** The manifest's unavailable retry. */
export function sendMessageUnavailable(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageUnavailable()); }
/** The manifest's default sendMessage scenario. */
export function sendMessageDefault(): Case<SendMessageOutput> { return publicSendCase(generatedMock.sendMessageDefault()); }

/** Script an arbitrary replyToMessage branch. */
export function replyToMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary replyToMessage retry. */
export function replyToMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's repliedInThread scenario. */
export function replyToMessageRepliedInThread(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageRepliedInThread()); }
/** The manifest's sourceMissing branch. */
export function replyToMessageSourceMissing(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageSourceMissing()); }
/** The manifest's malformedSource branch. */
export function replyToMessageMalformedSource(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageMalformedSource()); }
/** The manifest's connectionLost retry. */
export function replyToMessageConnectionLost(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageConnectionLost()); }
/** The manifest's emptyBody defect. */
export function replyToMessageEmptyBody(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageEmptyBody()); }
/** The manifest's default replyToMessage scenario. */
export function replyToMessageDefault(): Case<SendMessageOutput> { return publicSendCase(generatedMock.replyToMessageDefault()); }
