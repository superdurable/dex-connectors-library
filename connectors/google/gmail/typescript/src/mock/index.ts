// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Failure } from '@superdurable/connectors-sdk';
import { branchCase, retryCase, type Case, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import {
  GmailConnection, type GetMessageInput, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput,
} from '../index.js';
import { GeneratedMock } from '../generated/mock.js';
import * as generatedMock from '../generated/mock.js';

/** Scripted Gmail connection with the manifest's happy-path defaults. */
export class GmailMock {
  readonly #generated: GeneratedMock;
  readonly #connection: GmailConnection;

  /** Construct all three operation scripts under one static connection name. */
  constructor(connectionName: string) {
    if (!connectionName.trim()) throw new TypeError('Gmail mock connection name is required');
    this.#generated = new GeneratedMock(connectionName);
    this.#connection = new GmailConnection(this, { provider: 'google', name: connectionName });
  }

  /** Return the typed connection for Gmail Step factories. */
  connection(): GmailConnection { return this.#connection; }
  /** Script getMessage calls. */
  getMessage(): MockQuery<GetMessageInput, Message> { return this.#generated.getMessage(); }
  /** Script sendMessage calls. */
  sendMessage(): MockMutation<SendMessageInput, SendMessageOutput> { return this.#generated.sendMessage(); }
  /** Script replyToMessage calls. */
  replyToMessage(): MockMutation<ReplyToMessageInput, SendMessageOutput> { return this.#generated.replyToMessage(); }
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
export function sendMessageSentToCustomer(): Case<SendMessageOutput> { return generatedMock.sendMessageSentToCustomer(); }
/** The manifest's invalidRecipient branch. */
export function sendMessageInvalidRecipient(): Case<SendMessageOutput> { return generatedMock.sendMessageInvalidRecipient(); }
/** The manifest's connectionLost retry. */
export function sendMessageConnectionLost(): Case<SendMessageOutput> { return generatedMock.sendMessageConnectionLost(); }
/** The manifest's noRecipients defect. */
export function sendMessageNoRecipients(): Case<SendMessageOutput> { return generatedMock.sendMessageNoRecipients(); }
/** The manifest's unavailable retry. */
export function sendMessageUnavailable(): Case<SendMessageOutput> { return generatedMock.sendMessageUnavailable(); }
/** The manifest's default sendMessage scenario. */
export function sendMessageDefault(): Case<SendMessageOutput> { return generatedMock.sendMessageDefault(); }

/** Script an arbitrary replyToMessage branch. */
export function replyToMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary replyToMessage retry. */
export function replyToMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's repliedInThread scenario. */
export function replyToMessageRepliedInThread(): Case<SendMessageOutput> { return generatedMock.replyToMessageRepliedInThread(); }
/** The manifest's sourceMissing branch. */
export function replyToMessageSourceMissing(): Case<SendMessageOutput> { return generatedMock.replyToMessageSourceMissing(); }
/** The manifest's malformedSource branch. */
export function replyToMessageMalformedSource(): Case<SendMessageOutput> { return generatedMock.replyToMessageMalformedSource(); }
/** The manifest's connectionLost retry. */
export function replyToMessageConnectionLost(): Case<SendMessageOutput> { return generatedMock.replyToMessageConnectionLost(); }
/** The manifest's emptyBody defect. */
export function replyToMessageEmptyBody(): Case<SendMessageOutput> { return generatedMock.replyToMessageEmptyBody(); }
/** The manifest's default replyToMessage scenario. */
export function replyToMessageDefault(): Case<SendMessageOutput> { return generatedMock.replyToMessageDefault(); }
