// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { GmailConnection, newGetMessageStep, type GetMessageInput, type Message } from '@superdurable/connector-google-gmail';

/** Static Dex Web connection name shared by the Gmail example Flows. */
export const ConnectionName = 'gmail-inbox';

/** Runnable Flow that reads one Gmail message. */
export class GmailReadFlow implements Flow<GetMessageInput> {
  private readonly doneStep = new ReadCompletedStep();
  private readonly readStep;

  /** Bind the Gmail connection before registering the operation Step. */
  constructor(connection: GmailConnection) {
    if (!connection) throw new TypeError('Gmail read connection is required');
    this.readStep = newGetMessageStep<GetMessageInput>({
      stepType: 'ReadGmailMessage', connectionName: 'gmail-inbox', connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Read the selected Gmail message.' },
      mapToOperationInput: input => input, read: this.doneStep,
    });
  }

  /** Stable Flow type registered by the Dex Worker. */
  getFlowType(): string { return 'GmailReadExample'; }
  /** Register the provider Step and its terminal receiver. */
  getSteps() { return StepList.startStep(this.readStep).otherSteps(this.doneStep); }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after the Gmail message is read."
class ReadCompletedStep implements Step<QueryResult<Message>> {
  getStepType(): string { return 'CompleteGmailRead'; }
  execute(context: Context, result: QueryResult<Message>): StepDecision { void context; return forceComplete(result); }
}
