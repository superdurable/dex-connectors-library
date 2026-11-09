// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendTextMessageStep, type Connection, type SendTextMessageInput, type SentMessage } from '@superdurable/connector-meta-whatsapp-business';

/** Static Dex Web connection for the WhatsApp Business example. */
export const ConnectionName = 'whatsapp-business';
const resultAttribute = new Attribute<SentMessage>('whatsapp-business-text-message', jsonCodec<SentMessage>());

/** Send one text message with the connected system-user token. */
export class TextMessageFlow implements Flow<SendTextMessageInput> {
  private readonly complete = new CompletePublishPost();
  private readonly send;
  /** Bind the required static connection. */ constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('WhatsApp Business connection name differs');
    this.send = newSendTextMessageStep<SendTextMessageInput>({
      stepType: 'CallSendTextMessage',
      annotations: { groupId: 'provider', groupLabel: 'WhatsApp Business', explanation: 'Send one WhatsApp text message using the system-user token.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, sent: this.complete,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WhatsAppBusinessTextMessage'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<SendTextMessageInput> {
    return StepList.startStep(this.send).otherSteps(this.complete);
  }
  /** Register the persisted provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the sent message ID and complete the Flow."
class CompletePublishPost implements Step<MutationResult<SentMessage>> {
  readonly inputCodec = jsonCodec<MutationResult<SentMessage>>();
  getStepType(): string { return 'CompleteSendTextMessage'; }
  execute(context: Context, result: MutationResult<SentMessage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
