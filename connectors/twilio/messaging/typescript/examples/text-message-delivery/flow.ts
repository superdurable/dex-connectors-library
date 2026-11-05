// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceFail, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newGetMessageStep, newSendMessageStep, type Connection, type Message, type SendMessageInput } from '../../src/index.js';

/** Static Dex Web connection name for the example. */ export const ConnectionName = 'twilio-messaging';
/** One message send and immediate status read. */
export class TextMessageDeliveryFlow implements Flow<SendMessageInput> {
  private readonly completed = new CompleteDelivery();
  private readonly getMessage;
  private readonly sendMessage;
  /** Bind the same connection to both provider operations. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('text message delivery requires its static Twilio connection');
    this.getMessage = newGetMessageStep<MutationResult<Message>>({
      stepType: 'GetSentMessage', annotations: { groupId: 'twilio-messaging', groupLabel: 'Twilio Messaging', explanation: 'Read the newly accepted message status from Twilio.' },
      connection, connectionName: ConnectionName, mapToOperationInput: result => ({ messageSid: result.value.sid ?? '' }), found: this.completed,
    });
    this.sendMessage = newSendMessageStep<SendMessageInput>({
      stepType: 'SendTextMessage', annotations: { groupId: 'twilio-messaging', groupLabel: 'Twilio Messaging', explanation: 'Send the requested text once through the Twilio Messages API.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input, accepted: this.getMessage,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'TwilioTextMessageDelivery'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<SendMessageInput> { return StepList.startStep<SendMessageInput>(this.sendMessage).otherSteps(this.getMessage, this.completed); }
}

// dex:group group-id:twilio-messaging group-label:"Twilio Messaging"
// dex:explanation text:"Complete after Twilio confirms the newly created message can be read."
class CompleteDelivery implements Step<QueryResult<Message>> {
  getStepType(): string { return 'CompleteTextMessageDelivery'; }
  execute(_context: Context, result: QueryResult<Message>): StepDecision {
    if (!result.value.sid) return forceFail('Twilio did not return a message SID');
    return gracefulComplete(result.value);
  }
}

/** Input codec for a caller starting this Flow. */
export const MessageRequestCodec = jsonCodec<SendMessageInput>();
