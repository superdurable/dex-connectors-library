// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendMessageStep, type Connection, type SendMessageInput, type SentMessage } from '../../src/index.js';

/** Static Dex Web connection name used by this example. */
export const ConnectionName = 'email';

/** Submit one plain-text email and complete after SMTP acceptance. */
export class SendMessageFlow implements Flow<SendMessageInput> {
  private readonly completed = new MessageAccepted();
  private readonly send;
  /** Bind the connector's named connection at Worker startup. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('email connection name differs from the example');
    this.send = newSendMessageStep<SendMessageInput>({ stepType: 'SendEmailMessage', annotations: { groupId: 'email', groupLabel: 'Email', explanation: 'Submit one plain-text message over TLS with a Dex submission checkpoint.' }, connection, connectionName: ConnectionName, mapToOperationInput: input => input, sent: this.completed });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'EmailSendMessage'; }
  /** Register the SMTP Step and completion Step. */ getSteps() { return StepList.startStep<SendMessageInput>(this.send).otherSteps(this.completed); }
}

// dex:group group-id:email group-label:"Email"
// dex:explanation text:"Complete the Flow after the SMTP server accepts the message."
class MessageAccepted implements Step<MutationResult<SentMessage>> {
  getStepType(): string { return 'MessageAccepted'; }
  execute(_context: Context, result: MutationResult<SentMessage>): StepDecision { return gracefulComplete(result.value); }
}
