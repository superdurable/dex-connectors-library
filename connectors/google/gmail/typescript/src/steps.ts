// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationStep, newQueryStep, type MutationStep, type MutationStepConfig, type QueryStep, type QueryStepConfig } from '@superdurable/connectors-sdk';
import { GmailConnection } from './connection.js';
import type { GetMessageInput, Message, ReplyToMessageInput, SendMessageInput, SendMessageOutput } from './models.js';

/** Application wiring for one getMessage Query Step. */
export interface GetMessageStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetMessageInput, Message>, 'operation' | 'connection'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
}

/** Application wiring for one sendMessage Mutation Step. */
export interface SendMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, SendMessageInput, SendMessageOutput>, 'operation' | 'connection'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
}

/** Application wiring for one replyToMessage Mutation Step. */
export interface ReplyToMessageStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, ReplyToMessageInput, SendMessageOutput>, 'operation' | 'connection'> {
  /** Static name declared by the consuming application. */ connectionName: string;
  /** Gmail provider or scripted mock connection. */ connection: GmailConnection;
}

/** Construct the operation-specific Gmail message read Step. */
export function newGetMessageStep<StepInput>(configuration: GetMessageStepConfig<StepInput>): QueryStep<StepInput, GetMessageInput, Message> {
  const { connectionName, connection, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newQueryStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.getMessage() });
}

/** Construct the operation-specific Gmail send Step. */
export function newSendMessageStep<StepInput>(configuration: SendMessageStepConfig<StepInput>): MutationStep<StepInput, SendMessageInput, SendMessageOutput> {
  const { connectionName, connection, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.sendMessage() });
}

/** Construct the operation-specific Gmail reply Step. */
export function newReplyToMessageStep<StepInput>(configuration: ReplyToMessageStepConfig<StepInput>): MutationStep<StepInput, ReplyToMessageInput, SendMessageOutput> {
  const { connectionName, connection, ...stepConfiguration } = configuration;
  validateNamedConnection(connectionName, connection);
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.replyToMessage() });
}

function validateNamedConnection(connectionName: string, connection: GmailConnection): void {
  if (!(connection instanceof GmailConnection) || !connectionName || connection.reference.name !== connectionName) {
    throw new TypeError('Gmail Step connection name must match its typed connection');
  }
}
