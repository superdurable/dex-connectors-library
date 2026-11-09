// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendMessageStep, type Connection, type SendMessageInput, type SentMessage } from '@superdurable/connector-telegram';

export const ConnectionName = 'telegram';
const sentMessageAttribute = new Attribute<SentMessage>('telegram-typescript-sent-message', jsonCodec<SentMessage>());

/** Send one plain text message through a named Telegram connection. */
export class MessageSendFlow implements Flow<SendMessageInput> {
  private readonly complete = new CompleteMessageSend();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Telegram connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'TelegramTypeScriptMessageSend'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<SendMessageInput> {
    return StepList.startStep(newSendMessageStep<SendMessageInput>({
      stepType: 'CallTelegramSendMessage',
      annotations: { groupId: 'message-send', groupLabel: 'Message send',
        explanation: 'Send one plain text message to a bot-accessible chat.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, sent: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted message identity. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [sentMessageAttribute] };
  }
}

// dex:group group-id:message-send group-label:"Message send"
// dex:explanation text:"Persist the sent message identity and complete the Flow."
export class CompleteMessageSend implements Step<MutationResult<SentMessage>> {
  readonly inputCodec = jsonCodec<MutationResult<SentMessage>>();
  getStepType(): string { return 'CompleteTelegramMessageSend'; }
  execute(context: Context, result: MutationResult<SentMessage>): StepDecision {
    sentMessageAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
