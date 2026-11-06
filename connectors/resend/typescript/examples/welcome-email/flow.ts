// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendEmailStep, type Connection, type SendEmailInput, type SentEmail } from '../../src/index.js';

/** Static Dex Web connection used by this example. */
export const ConnectionName = 'resend';
/** Durable provider result, containing only the email ID. */
export const sentEmail = new Attribute<SentEmail>('resend-welcome-email-sent', jsonCodec<SentEmail>());

/** Send one welcome email and retain its provider ID. */
export class WelcomeEmailFlow implements Flow<SendEmailInput> {
  private readonly prepare = new PrepareWelcomeEmail();
  private readonly complete = new CompleteWelcomeEmail();
  /** Bind a real or scripted Resend connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('welcome email requires the static Resend connection');
  }
  /** Stable Flow type. */ getFlowType(): string { return 'ResendWelcomeEmail'; }
  /** Register the start, provider, and completion Steps. */
  getSteps(): StepList<SendEmailInput> {
    return StepList.startStep(this.prepare).otherSteps(newSendEmailStep({
      stepType: 'SendEmail', annotations: { groupId: 'welcome-email', groupLabel: 'Welcome email', explanation: 'Send one welcome email through Resend.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, sent: this.complete,
    }), this.complete);
  }
  /** Register the persisted result Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [sentEmail] }; }
}

// dex:group group-id:welcome-email group-label:"Welcome email"
// dex:explanation text:"Validate the welcome email fields before calling Resend."
class PrepareWelcomeEmail implements Step<SendEmailInput> {
  getStepType(): string { return 'PrepareWelcomeEmail'; }
  execute(_context: Context, input: SendEmailInput): StepDecision {
    if (!input.from || !input.to?.length || !input.subject || !input.html) return forceFail('welcome email fields are required');
    return goTo(new StepRef('SendEmail'), input);
  }
}

// dex:group group-id:welcome-email group-label:"Welcome email"
// dex:explanation text:"Persist the Resend email ID and complete the Flow."
class CompleteWelcomeEmail implements Step<MutationResult<SentEmail>> {
  getStepType(): string { return 'CompleteWelcomeEmail'; }
  execute(context: Context, result: MutationResult<SentEmail>): StepDecision {
    sentEmail.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
