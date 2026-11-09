// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newCaptureEventStep, type CaptureEventInput, type CapturedEvent, type Connection } from '@superdurable/connector-posthog';

export const ConnectionName = 'posthog';
const eventAttribute = new Attribute<CapturedEvent>('posthog-typescript-captured-event', jsonCodec<CapturedEvent>());

/** Capture one event with a stable timestamp through PostHog. */
export class EventCaptureFlow implements Flow<CaptureEventInput> {
  private readonly complete = new CompleteCapture();
  /** Bind the required static connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('PostHog connection name differs');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'PostHogTypeScriptEventCapture'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<CaptureEventInput> {
    return StepList.startStep(newCaptureEventStep<CaptureEventInput>({
      stepType: 'CallPostHogCaptureEvent',
      annotations: { groupId: 'event-capture', groupLabel: 'Event capture',
        explanation: 'Capture one PostHog event with a stable timestamp and UUID.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, accepted: this.complete,
    })).otherSteps(this.complete);
  }
  /** Register the persisted event identity. */ getPersistenceSchema(): PersistenceSchema {
    return { attributes: [eventAttribute] };
  }
}

// dex:group group-id:event-capture group-label:"Event capture"
// dex:explanation text:"Persist the accepted event identity and complete the Flow."
export class CompleteCapture implements Step<MutationResult<CapturedEvent>> {
  readonly inputCodec = jsonCodec<MutationResult<CapturedEvent>>();
  getStepType(): string { return 'CompletePostHogCapture'; }
  execute(context: Context, result: MutationResult<CapturedEvent>): StepDecision {
    eventAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
