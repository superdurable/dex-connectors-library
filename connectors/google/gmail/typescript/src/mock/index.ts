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
export { getMessageCustomerQuestion, getMessageDeletedMessage, getMessageRevokedGrant, getMessageMalformedPayload, getMessageEmptyMessageID, getMessageRateLimited, getMessageDefault, getMessageRetry } from '../generated/mock.js';

/** Scripted Gmail connection with the manifest's happy-path defaults. */
export class GmailMock {
  readonly #getMessage: MockQuery<GetMessageInput, Message>;
  readonly #sendMessage: MockMutation<SendMessageInput, SendMessageOutput>;
  readonly #replyToMessage: MockMutation<ReplyToMessageInput, SendMessageOutput>;
  readonly #connection: GmailConnection;

  /** Construct all three operation scripts under one static connection name. */
  constructor(connectionName: string) {
    if (!connectionName.trim()) throw new TypeError('Gmail mock connection name is required');
    this.#getMessage = newQuery<GetMessageInput, Message>(GetMessageDefinition).default(generatedMock.getMessageDefault());
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

function adaptGeneratedSendCase(factory: () => Case<GeneratedSendMessageOutput>): () => Case<SendMessageOutput> {
  return () => publicSendCase(factory());
}

/** Script an arbitrary getMessage branch. */
export function getMessageBranch(branch: string, value: Message, reason?: Failure): Case<Message> { return branchCase(branch, value, reason); }
/** Script an arbitrary sendMessage branch. */
export function sendMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary sendMessage retry. */
export function sendMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's sentToCustomer scenario. */
export const sendMessageSentToCustomer = adaptGeneratedSendCase(generatedMock.sendMessageSentToCustomer);
/** The manifest's invalidRecipient branch. */
export const sendMessageInvalidRecipient = adaptGeneratedSendCase(generatedMock.sendMessageInvalidRecipient);
/** The manifest's connectionLost retry. */
export const sendMessageConnectionLost = adaptGeneratedSendCase(generatedMock.sendMessageConnectionLost);
/** The manifest's noRecipients defect. */
export const sendMessageNoRecipients = adaptGeneratedSendCase(generatedMock.sendMessageNoRecipients);
/** The manifest's unavailable retry. */
export const sendMessageUnavailable = adaptGeneratedSendCase(generatedMock.sendMessageUnavailable);
/** The manifest's default sendMessage scenario. */
export const sendMessageDefault = adaptGeneratedSendCase(generatedMock.sendMessageDefault);

/** Script an arbitrary replyToMessage branch. */
export function replyToMessageBranch(branch: string, value: SendMessageOutput, reason?: Failure): Case<SendMessageOutput> { return branchCase(branch, value, reason); }
/** Script an arbitrary replyToMessage retry. */
export function replyToMessageRetry(reason: Failure, afterMs = 0): Case<SendMessageOutput> { return retryCase(reason, afterMs); }
/** The manifest's repliedInThread scenario. */
export const replyToMessageRepliedInThread = adaptGeneratedSendCase(generatedMock.replyToMessageRepliedInThread);
/** The manifest's sourceMissing branch. */
export const replyToMessageSourceMissing = adaptGeneratedSendCase(generatedMock.replyToMessageSourceMissing);
/** The manifest's malformedSource branch. */
export const replyToMessageMalformedSource = adaptGeneratedSendCase(generatedMock.replyToMessageMalformedSource);
/** The manifest's connectionLost retry. */
export const replyToMessageConnectionLost = adaptGeneratedSendCase(generatedMock.replyToMessageConnectionLost);
/** The manifest's emptyBody defect. */
export const replyToMessageEmptyBody = adaptGeneratedSendCase(generatedMock.replyToMessageEmptyBody);
/** The manifest's default replyToMessage scenario. */
export const replyToMessageDefault = adaptGeneratedSendCase(generatedMock.replyToMessageDefault);
