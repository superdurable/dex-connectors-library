// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetScheduledEventStep, type Connection, type GetScheduledEventInput, type ScheduledEvent } from '@superdurable/connector-calendly';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'calendly-scheduling';
/** The provider-safe scheduled event returned by the Flow. */
export const scheduledEvent = new Attribute<ScheduledEvent>('calendly-scheduled-event', jsonCodec<ScheduledEvent>());

/** Read one scheduled event through Calendly. */
export class ScheduledEventFlow implements Flow<GetScheduledEventInput> {
  private readonly prepare = new PrepareScheduledEvent();
  private readonly complete = new CompleteScheduledEvent();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'CalendlyScheduledEventFlow'; }
  /** Register the preparation, provider query, and completion Steps. */
  getSteps(): StepList<GetScheduledEventInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetScheduledEventStep({
        stepType: 'GetCalendlyScheduledEvent',
        annotations: { groupId: 'calendly-event', groupLabel: 'Calendly Event', explanation: 'Read the requested scheduled event.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the persisted event. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [scheduledEvent] }; }
}

// dex:group group-id:calendly-event group-label:"Calendly Event"
// dex:explanation text:"Validate the event URI before the provider call."
/** Validate the selected event URI. */
export class PrepareScheduledEvent implements Step<GetScheduledEventInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareCalendlyScheduledEvent'; }
  /** Start the provider query with a public resource URI. */
  execute(_context: Context, input: GetScheduledEventInput): StepDecision {
    if (!/^https:\/\/api\.calendly\.com\/scheduled_events\/[A-Za-z0-9_-]{1,64}$/.test(input.scheduledEventUri)) return forceFail('Calendly scheduled event URI is invalid');
    return goTo(new StepRef('GetCalendlyScheduledEvent'), input);
  }
}

// dex:group group-id:calendly-event group-label:"Calendly Event"
// dex:explanation text:"Persist the scheduled event and complete the Flow."
/** Complete the Flow with one scheduled event. */
export class CompleteScheduledEvent implements Step<QueryResult<ScheduledEvent>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCalendlyScheduledEvent'; }
  /** Persist only the provider-safe event. */
  execute(context: Context, result: QueryResult<ScheduledEvent>): StepDecision {
    scheduledEvent.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
