// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import { goToBranch, type MutationResult } from '@superdurable/connectors-sdk';
import { GmailConnection, newSendMessageStep, type SendMessageInput, type SendMessageOutput } from '@superdurable/connector-google-gmail';

const ConnectionName = 'gmail-inbox';

/** Runnable Flow that sends one new Gmail message. */
export class GmailSendFlow implements Flow<SendMessageInput> {
  private readonly doneStep = new SendCompletedStep();
  private readonly sendStep;

  /** Bind the Gmail connection before registering the operation Step. */
  constructor(connection: GmailConnection) {
    if (!connection) throw new TypeError('Gmail send connection is required');
    // dex:group group-id:gmail group-label:"Gmail"
    // dex:explanation text:"Send one new Gmail message."
    this.sendStep = newSendMessageStep<SendMessageInput>({
      stepType: 'SendGmailMessage', connectionName: ConnectionName, connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Send one new Gmail message.' },
      mapToOperationInput: input => input, branches: [goToBranch('sent', this.doneStep)],
    });
  }

  /** Stable Flow type registered by the Dex Worker. */
  getFlowType(): string { return 'GmailSendExample'; }
  /** Register the provider Step and its terminal receiver. */
  getSteps() { return StepList.startStep(this.sendStep).otherSteps(this.doneStep); }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail accepts the new message."
class SendCompletedStep implements Step<MutationResult<SendMessageOutput>> {
  getStepType(): string { return 'CompleteGmailSend'; }
  execute(context: Context, result: MutationResult<SendMessageOutput>): StepDecision { void context; return forceComplete(result); }
}
