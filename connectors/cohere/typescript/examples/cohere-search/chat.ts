// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newChatStep, type ChatInput, type ChatOutput, type Connection } from '../../src/index.js';
import { ConnectionName } from './workflow.js';

// dex:group group-id:cohere-chat group-label:"Cohere chat"
// dex:explanation text:"Return the generated reply and its completion reason."
class ReturnChat implements Step<QueryResult<ChatOutput>> {
  getStepType() { return 'ReturnCohereReplyTS'; }
  execute(_context: Context, result: QueryResult<ChatOutput>) { return gracefulComplete(result.value); }
}

/** Generate one reply on the cohere connection. */
export class CohereChatFlow implements Flow<ChatInput> {
  private readonly complete = new ReturnChat();
  private readonly chat;

  /** Bind the provider during Worker registration. */
  constructor(connection: Connection) {
    this.chat = newChatStep<ChatInput>({
      stepType: 'GenerateCohereReplyTS',
      annotations: { groupId: 'cohere-chat', groupLabel: 'Cohere chat', explanation: 'Generate a reply.' },
      connection, connectionName: ConnectionName, mapToOperationInput: conversation => conversation,
      answered: this.complete,
    });
  }

  getFlowType() { return 'CohereChatTS'; }
  getSteps() { return StepList.startStep<ChatInput>(this.chat).otherSteps(this.complete); }
}
