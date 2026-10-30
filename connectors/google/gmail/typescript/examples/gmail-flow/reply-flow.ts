// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { GmailConnection, newReplyToMessageStep, type ReplyToMessageInput, type SendMessageOutput } from '@superdurable/connector-google-gmail';

/** Runnable Flow that replies in one Gmail thread. */
export class GmailReplyFlow implements Flow<ReplyToMessageInput> {
  private readonly doneStep = new ReplyCompletedStep();
  private readonly replyStep;

  /** Bind the Gmail connection before registering the operation Step. */
  constructor(connection: GmailConnection) {
    if (!connection) throw new TypeError('Gmail reply connection is required');
    this.replyStep = newReplyToMessageStep<ReplyToMessageInput>({
      stepType: 'ReplyToGmailMessage', connectionName: 'gmail-inbox', connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Reply in the selected Gmail thread.' },
      mapToOperationInput: input => input, sent: this.doneStep,
    });
  }

  /** Stable Flow type registered by the Dex Worker. */
  getFlowType(): string { return 'GmailReplyExample'; }
  /** Register the provider Step and its terminal receiver. */
  getSteps() { return StepList.startStep(this.replyStep).otherSteps(this.doneStep); }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail accepts the thread reply."
class ReplyCompletedStep implements Step<MutationResult<SendMessageOutput>> {
  getStepType(): string { return 'CompleteGmailReply'; }
  execute(context: Context, result: MutationResult<SendMessageOutput>): StepDecision { void context; return forceComplete(result); }
}
