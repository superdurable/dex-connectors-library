// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetChatStep, type Chat, type Connection, type GetChatInput } from '@superdurable/connector-telegram';

export const ConnectionName = 'telegram';
const chatAttribute = new Attribute<Chat>('telegram-typescript-chat', jsonCodec<Chat>());

/** Read metadata for one bot-accessible Telegram chat. */
export class ChatLookupFlow implements Flow<GetChatInput> {
  private readonly complete = new CompleteChatLookup();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Telegram connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'TelegramTypeScriptChatLookup'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetChatInput> {
    return StepList.startStep(newGetChatStep<GetChatInput>({
      stepType: 'CallTelegramGetChat',
      annotations: { groupId: 'chat-lookup', groupLabel: 'Chat lookup',
        explanation: 'Read basic metadata for one bot-accessible Telegram chat.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted chat metadata. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [chatAttribute] };
  }
}

// dex:group group-id:chat-lookup group-label:"Chat lookup"
// dex:explanation text:"Persist the chat metadata and complete the Flow."
export class CompleteChatLookup implements Step<QueryResult<Chat>> {
  readonly inputCodec = jsonCodec<QueryResult<Chat>>();
  getStepType(): string { return 'CompleteTelegramChatLookup'; }
  execute(context: Context, result: QueryResult<Chat>): StepDecision {
    chatAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
