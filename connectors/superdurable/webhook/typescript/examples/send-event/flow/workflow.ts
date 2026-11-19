// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, goTo, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newSendEventStep, type Connection, type SendEventInput, type SendEventOutput } from '@superdurable/connector-superdurable-webhook';

/** Static Dex Web connection name for the Worker and starter. */
export const ConnectionName = 'webhook-events';

/** Send one signed event to the configured receiver. */
export class SendEventFlow implements Flow<SendEventInput> {
  private readonly prepare = new PrepareEvent();
  private readonly complete = new CompleteEvent();

  /** Bind the named webhook connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('webhook connection is required');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WebhookSendEventFlow'; }
  /** Register the provider Step and completion. */
  getSteps(): StepList<SendEventInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSendEventStep({
        stepType: 'SendWebhookEvent',
        annotations: { groupId: 'webhook-delivery', groupLabel: 'Webhook Delivery', explanation: 'Send the signed event to the configured receiver.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        delivered: this.complete,
      }),
      this.complete,
    );
  }
  /** No Attributes or Streams are persisted. */ getPersistenceSchema(): PersistenceSchema { return {}; }
}

/** Start the signed delivery Step. */
// dex:group group-id:webhook-delivery group-label:"Webhook Delivery"
// dex:explanation text:"Pass the event to the signed delivery Step."
export class PrepareEvent implements Step<SendEventInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareWebhookEvent'; }
  /** Route the event without storing its payload. */
  execute(_context: Context, input: SendEventInput): StepDecision { return goTo(new StepRef('SendWebhookEvent'), input); }
}

/** Complete after the receiver accepts delivery. */
// dex:group group-id:webhook-delivery group-label:"Webhook Delivery"
// dex:explanation text:"Complete after the receiver accepts the event."
export class CompleteEvent implements Step<MutationResult<SendEventOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteWebhookEvent'; }
  /** Return only the safe delivery receipt fields. */
  execute(_context: Context, result: MutationResult<SendEventOutput>): StepDecision { return gracefulComplete(result.value); }
}
