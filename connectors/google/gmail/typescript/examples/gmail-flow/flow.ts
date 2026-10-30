// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, forceComplete, goTo, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import { goToBranch, type MutationResult, type QueryResult } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import {
  GmailConnection, newGetMessageStep, newReplyToMessageStep, newSendMessageStep,
  type GetMessageInput, type Message, type ReplyToMessageInput, type SendMessageInput, type SendMessageOutput,
} from '../../src/index.js';

/** Static Dex Web connection name used by every Gmail Step in this Flow. */
export const ConnectionName = 'gmail-inbox';

/** Three explicit example actions, each routed through its operation Step. */
export const GmailExampleInputSchema = z.discriminatedUnion('action', [
  z.strictObject({ action: z.literal('read'), messageId: z.string() }),
  z.strictObject({ action: z.literal('send'), to: z.array(z.string()), subject: z.string(), textBody: z.string(), htmlBody: z.string().optional() }),
  z.strictObject({ action: z.literal('reply'), messageId: z.string(), textBody: z.string(), htmlBody: z.string().optional() }),
]);
/** JSON input for the example's one starting Step. */
export type GmailExampleInput = z.infer<typeof GmailExampleInputSchema>;

/** Runnable Flow showing every Gmail Query and Mutation operation. */
export class GmailExampleFlow implements Flow<GmailExampleInput> {
  readonly #readDone = new ReadCompletedStep();
  readonly #sendDone = new SendCompletedStep();
  readonly #replyDone = new ReplyCompletedStep();
  readonly #read;
  readonly #send;
  readonly #reply;
  readonly #start;

  /** Bind one provider or mock connection before registering Steps. */
  constructor(connection: GmailConnection) {
    if (!connection) throw new TypeError('Gmail example connection is required');
    // dex:group group-id:gmail group-label:"Gmail"
    // dex:explanation text:"Read the selected Gmail message."
    this.#read = newGetMessageStep<GmailExampleInput>({
      stepType: 'ReadGmailMessage', connectionName: ConnectionName, connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Read the selected Gmail message.' },
      mapToOperationInput: readInput, branches: [goToBranch('read', this.#readDone)],
    });
    // dex:group group-id:gmail group-label:"Gmail"
    // dex:explanation text:"Send one new Gmail message."
    this.#send = newSendMessageStep<GmailExampleInput>({
      stepType: 'SendGmailMessage', connectionName: ConnectionName, connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Send one new Gmail message.' },
      mapToOperationInput: sendInput, branches: [goToBranch('sent', this.#sendDone)],
    });
    // dex:group group-id:gmail group-label:"Gmail"
    // dex:explanation text:"Reply in the selected Gmail thread."
    this.#reply = newReplyToMessageStep<GmailExampleInput>({
      stepType: 'ReplyToGmailMessage', connectionName: ConnectionName, connection,
      annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Reply in the selected Gmail thread.' },
      mapToOperationInput: replyInput, branches: [goToBranch('sent', this.#replyDone)],
    });
    this.#start = new StartGmailOperationStep(this.#read, this.#send, this.#reply);
  }

  /** Stable Flow type registered by the Dex Worker. */
  getFlowType(): string { return 'GmailOperationsExample'; }

  /** Register the starting Step, all provider Steps, and their terminal receivers. */
  getSteps() {
    return StepList.startStep(this.#start).otherSteps(this.#read, this.#send, this.#reply, this.#readDone, this.#sendDone, this.#replyDone);
  }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Choose the requested Gmail operation."
class StartGmailOperationStep implements Step<GmailExampleInput> {
  constructor(
    private readonly read: Step<GmailExampleInput>,
    private readonly send: Step<GmailExampleInput>,
    private readonly reply: Step<GmailExampleInput>,
  ) {}

  getStepType(): string { return 'StartGmailOperation'; }

  execute(context: Context, input: GmailExampleInput): StepDecision {
    void context;
    const selected = GmailExampleInputSchema.parse(input);
    switch (selected.action) {
      case 'read': return goTo(this.read, selected);
      case 'send': return goTo(this.send, selected);
      case 'reply': return goTo(this.reply, selected);
    }
  }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after the Gmail message is read."
class ReadCompletedStep implements Step<QueryResult<Message>> {
  getStepType(): string { return 'CompleteGmailRead'; }
  execute(context: Context, result: QueryResult<Message>): StepDecision { void context; return forceComplete(result); }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail accepts the new message."
class SendCompletedStep implements Step<MutationResult<SendMessageOutput>> {
  getStepType(): string { return 'CompleteGmailSend'; }
  execute(context: Context, result: MutationResult<SendMessageOutput>): StepDecision { void context; return forceComplete(result); }
}

// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail accepts the thread reply."
class ReplyCompletedStep implements Step<MutationResult<SendMessageOutput>> {
  getStepType(): string { return 'CompleteGmailReply'; }
  execute(context: Context, result: MutationResult<SendMessageOutput>): StepDecision { void context; return forceComplete(result); }
}

function readInput(input: GmailExampleInput): GetMessageInput {
  if (input.action !== 'read') throw new TypeError('read Step received another Gmail action');
  return { messageId: input.messageId };
}

function sendInput(input: GmailExampleInput): SendMessageInput {
  if (input.action !== 'send') throw new TypeError('send Step received another Gmail action');
  return { to: input.to, subject: input.subject, textBody: input.textBody, ...(input.htmlBody ? { htmlBody: input.htmlBody } : {}) };
}

function replyInput(input: GmailExampleInput): ReplyToMessageInput {
  if (input.action !== 'reply') throw new TypeError('reply Step received another Gmail action');
  return { messageId: input.messageId, textBody: input.textBody, ...(input.htmlBody ? { htmlBody: input.htmlBody } : {}) };
}
