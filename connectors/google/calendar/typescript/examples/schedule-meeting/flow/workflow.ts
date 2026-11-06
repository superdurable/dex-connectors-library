// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetEventStep, type Connection, type Event, type GetEventInput } from '@superdurable/connector-google-calendar';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'google-calendar';
/** Persisted, nonsecret Calendar event. */
export const selectedEvent = new Attribute<Event>('calendar-selected-event', jsonCodec<Event>());

/** Read an event for a meeting workflow. */
export class ScheduleMeetingFlow implements Flow<GetEventInput> {
  private readonly prepare = new PrepareMeeting();
  private readonly complete = new CompleteMeeting();
  /** Bind the static Calendar connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'GoogleCalendarScheduleMeetingFlow'; }
  /** Register the provider Step and completion Step. */
  getSteps(): StepList<GetEventInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetEventStep({
        stepType: 'GetCalendarMeeting',
        annotations: { groupId: 'calendar-meeting', groupLabel: 'Google Calendar Meeting', explanation: 'Read one existing Calendar meeting.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the event output in the Flow schema. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [selectedEvent] }; }
}

/** Check nonsecret input before the provider call. */
// dex:group group-id:calendar-meeting group-label:"Google Calendar Meeting"
// dex:explanation text:"Validate the selected calendar and meeting before reading Google."
export class PrepareMeeting implements Step<GetEventInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareCalendarMeeting'; }
  /** Continue with a valid event selection. */
  execute(_context: Context, input: GetEventInput): StepDecision {
    if (!input.calendarId?.trim() || !input.eventId?.trim()) return forceFail('Calendar and event IDs are required');
    return goTo(new StepRef('GetCalendarMeeting'), input);
  }
}

/** Complete a meeting lookup with safe event metadata. */
// dex:group group-id:calendar-meeting group-label:"Google Calendar Meeting"
// dex:explanation text:"Persist the selected meeting and complete the Flow."
export class CompleteMeeting implements Step<QueryResult<Event>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCalendarMeeting'; }
  /** Persist and return the event. */
  execute(context: Context, result: QueryResult<Event>): StepDecision {
    selectedEvent.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
