// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newCreateChatCompletionStep, type Connection, type CreateChatCompletionInput, type ChatCompletion } from '../../src/index.js';

/** Static Dex Web connection for the IFLYTEK example. */
export const ConnectionName = 'iflytek';
const completionResult = new Attribute<ChatCompletion>('iflytek-chat-completion-result', jsonCodec<ChatCompletion>());

/** Generate a bounded Spark answer for one prompt. */
export class ChatCompletionFlow implements Flow<CreateChatCompletionInput> {
  private readonly read;
  private readonly complete = new CompleteChatCompletion();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('IFLYTEK connection name differs');
    this.read = newCreateChatCompletionStep<CreateChatCompletionInput>({
      stepType: 'CallIFLYTEK',
      annotations: { groupId: 'iflytek', groupLabel: 'iFLYTEK Spark', explanation: 'Generate one answer from the configured Spark model.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, completed: new StepRef('CompleteIFLYTEKChat'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'IFLYTEKChatCompletion'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<CreateChatCompletionInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable answer. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [completionResult] }; }
}

// dex:group group-id:iflytek group-label:"IFLYTEK"
// dex:explanation text:"Persist the assistant answer and complete the Flow."
class CompleteChatCompletion implements Step<QueryResult<ChatCompletion>> {
  getStepType(): string { return 'CompleteIFLYTEKChat'; }
  execute(context: Context, result: QueryResult<ChatCompletion>): StepDecision { completionResult.set(context, result.value); return gracefulComplete(result.value); }
}
