// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  type MutationResult, type MutationStep, type MutationStepConfig,
  type QueryResult, type QueryStep, type QueryStepConfig,
} from '@superdurable/connectors-sdk';
import type { StepTarget } from '@superdurable/dex';
import type { GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput } from './models.js';
import { GmailConnection } from './connection.js';
import {
  newGetMessageStep as newGeneratedGetMessageStep,
  newSendMessageStep as newGeneratedSendMessageStep,
  newReplyToMessageStep as newGeneratedReplyToMessageStep,
} from './generated/steps.js';

/** Configuration for the public getMessage Step. */
export interface GetMessageStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetMessageInput, Message>, 'operation' | 'connection' | 'branches'> {
  /** Named Gmail connection. */ connection: GmailConnection;
  /** Name matching the Gmail connection. */ connectionName: string;
  /** Successful read destination. */ read: StepTarget<QueryResult<Message>>;
  /** Missing message destination. */ notFound?: StepTarget<QueryResult<Message>>;
  /** Provider rejection destination. */ providerRejected?: StepTarget<QueryResult<Message>>;
  /** Invalid response destination. */ invalidResponse?: StepTarget<QueryResult<Message>>;
  /** Local defect destination. */ defect?: StepTarget<QueryResult<Message>>;
}

/** Configuration for the public sendMessage Step. */
export interface SendMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, SendMessageInput, SendMessageOutput>, 'operation' | 'connection' | 'branches'> {
  /** Named Gmail connection. */ connection: GmailConnection;
  /** Name matching the Gmail connection. */ connectionName: string;
  /** Successful send destination. */ sent: StepTarget<MutationResult<SendMessageOutput>>;
  /** Provider rejection destination. */ providerRejected?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Local defect destination. */ defect?: StepTarget<MutationResult<SendMessageOutput>>;
}

/** Configuration for the public replyToMessage Step. */
export interface ReplyToMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, ReplyToMessageInput, SendMessageOutput>, 'operation' | 'connection' | 'branches'> {
  /** Named Gmail connection. */ connection: GmailConnection;
  /** Name matching the Gmail connection. */ connectionName: string;
  /** Successful reply destination. */ sent: StepTarget<MutationResult<SendMessageOutput>>;
  /** Provider rejection destination. */ providerRejected?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Invalid response destination. */ invalidResponse?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Local defect destination. */ defect?: StepTarget<MutationResult<SendMessageOutput>>;
}

function requireGmailConnection(connection: GmailConnection, connectionName: string): void {
  if (!(connection instanceof GmailConnection) || !connectionName || connection.reference.name !== connectionName) {
    throw new TypeError('Gmail Step connection name must match its typed connection');
  }
}

/** Build a Gmail read Step with its typed connection. */
export function newGetMessageStep<StepInput>(configuration: GetMessageStepConfig<StepInput>): QueryStep<StepInput, GetMessageInput, Message> {
  requireGmailConnection(configuration.connection, configuration.connectionName);
  return newGeneratedGetMessageStep<StepInput, GetMessageInput, Message>(configuration);
}

/** Build a Gmail send Step with its typed connection. */
export function newSendMessageStep<StepInput>(configuration: SendMessageStepConfig<StepInput>): MutationStep<StepInput, SendMessageInput, SendMessageOutput> {
  requireGmailConnection(configuration.connection, configuration.connectionName);
  return newGeneratedSendMessageStep<StepInput, SendMessageInput, SendMessageOutput>(configuration);
}

/** Build a Gmail reply Step with its typed connection. */
export function newReplyToMessageStep<StepInput>(configuration: ReplyToMessageStepConfig<StepInput>): MutationStep<StepInput, ReplyToMessageInput, SendMessageOutput> {
  requireGmailConnection(configuration.connection, configuration.connectionName);
  return newGeneratedReplyToMessageStep<StepInput, ReplyToMessageInput, SendMessageOutput>(configuration);
}
