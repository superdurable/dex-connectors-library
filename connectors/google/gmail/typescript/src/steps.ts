// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationStep, newQueryStep, goToBranch, type MutationResult, type MutationStep, type MutationStepConfig, type QueryResult, type QueryStep, type QueryStepConfig } from '@superdurable/connectors-sdk';
import type { StepTarget } from '@superdurable/dex';
import { GmailConnection } from './connection.js';
import type { GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput } from './models.js';

/** Application wiring for one getMessage Query Step. */
export interface GetMessageStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetMessageInput, Message>, 'operation' | 'connection' | 'branches'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
  /** Receiver for a successfully read message. */ read: StepTarget<QueryResult<Message>>;
  /** Optional receiver for a missing message. */ notFound?: StepTarget<QueryResult<Message>>;
  /** Optional receiver for a conclusive provider refusal. */ providerRejected?: StepTarget<QueryResult<Message>>;
  /** Optional receiver for an invalid provider response. */ invalidResponse?: StepTarget<QueryResult<Message>>;
  /** Optional receiver for a local defect. */ defect?: StepTarget<QueryResult<Message>>;
}

/** Application wiring for one sendMessage Mutation Step. */
export interface SendMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, SendMessageInput, SendMessageOutput>, 'operation' | 'connection' | 'branches'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
  /** Receiver after Gmail accepts the message. */ sent: StepTarget<MutationResult<SendMessageOutput>>;
  /** Optional receiver for a conclusive provider refusal. */ providerRejected?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Optional receiver for a local defect. */ defect?: StepTarget<MutationResult<SendMessageOutput>>;
}

/** Application wiring for one replyToMessage Mutation Step. */
export interface ReplyToMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, ReplyToMessageInput, SendMessageOutput>, 'operation' | 'connection' | 'branches'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
  /** Receiver after Gmail accepts the reply. */ sent: StepTarget<MutationResult<SendMessageOutput>>;
  /** Optional receiver for a conclusive provider refusal. */ providerRejected?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Optional receiver for an invalid source response. */ invalidResponse?: StepTarget<MutationResult<SendMessageOutput>>;
  /** Optional receiver for a local defect. */ defect?: StepTarget<MutationResult<SendMessageOutput>>;
}

/** Construct the operation-specific Gmail message read Step. */
export function newGetMessageStep<StepInput>(configuration: GetMessageStepConfig<StepInput>): QueryStep<StepInput, GetMessageInput, Message> {
  const { connectionName, connection, read, notFound, providerRejected, invalidResponse, defect, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newQueryStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.getMessage(), branches: [
    goToBranch('read', read), ...(notFound ? [goToBranch('notFound', notFound)] : []),
    ...(providerRejected ? [goToBranch('providerRejected', providerRejected)] : []),
    ...(invalidResponse ? [goToBranch('invalidResponse', invalidResponse)] : []),
    ...(defect ? [goToBranch('defect', defect)] : []),
  ] });
}

/** Construct the operation-specific Gmail send Step. */
export function newSendMessageStep<StepInput>(configuration: SendMessageStepConfig<StepInput>): MutationStep<StepInput, SendMessageInput, SendMessageOutput> {
  const { connectionName, connection, sent, providerRejected, defect, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.sendMessage(), branches: [
    goToBranch('sent', sent), ...(providerRejected ? [goToBranch('providerRejected', providerRejected)] : []),
    ...(defect ? [goToBranch('defect', defect)] : []),
  ] });
}

/** Construct the operation-specific Gmail reply Step. */
export function newReplyToMessageStep<StepInput>(configuration: ReplyToMessageStepConfig<StepInput>): MutationStep<StepInput, ReplyToMessageInput, SendMessageOutput> {
  const { connectionName, connection, sent, providerRejected, invalidResponse, defect, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.replyToMessage(), branches: [
    goToBranch('sent', sent), ...(providerRejected ? [goToBranch('providerRejected', providerRejected)] : []),
    ...(invalidResponse ? [goToBranch('invalidResponse', invalidResponse)] : []),
    ...(defect ? [goToBranch('defect', defect)] : []),
  ] });
}

function validateNamedConnection(connectionName: string, connection: GmailConnection): void {
  if (!(connection instanceof GmailConnection) || !connectionName || connection.reference.name !== connectionName) {
    throw new TypeError('Gmail Step connection name must match its typed connection');
  }
}
