// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  EventChangeKind, defineEventChangedTriggerBinding, newGetEventStep,
  type Connection, type Event, type EventChangedEvent, type GetEventInput,
} from '@superdurable/connector-google-calendar';

/** Stable Flow identity each calendar change starts. */
export const FlowType = 'GoogleCalendarEventChangeTypeScript';
/** Static Dex Web connection for Google Calendar. */
export const ConnectionName = 'google-calendar-watcher';
/** The eventChanged binding that starts this Flow. */
export const EventChangedTriggerBinding = 'calendar-event-changes';
const readChangedEventStepType = 'ReadChangedEvent';

/** One eventChanged delivery with the Trigger event ID that also names the Flow. */
export interface EventChangeInput { /** Stable Trigger event ID. */ triggerEventId: string; /** The delivered change. */ change: EventChangedEvent }

/** The Flow result: the change and, unless it was cancelled, the event read back from Google. */
export interface ChangeOutcome {
  /** The watched calendar. */ calendarId: string;
  /** The changed event. */ eventId: string;
  /** The delivered change kind. */ changeKind: EventChangeKind;
  /** The event read back from Google Calendar; omitted for a cancelled event, which is not read. */ current?: Event;
}

/** The delivered change. */
export const eventChangeAttribute = new Attribute<EventChangeInput>('google-calendar-event-change', jsonCodec<EventChangeInput>());
/** The recorded outcome. */
export const eventChangeOutcomeAttribute = new Attribute<ChangeOutcome>('google-calendar-event-change-outcome', jsonCodec<ChangeOutcome>());

/** Name one Flow per change; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<EventChangedEvent>): string { return `google-calendar-event-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<EventChangedEvent>): EventChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to cancellations. */
export function acceptEveryEventChange(): boolean { return true; }
/** Read the changed event from its calendar. */
export function mapToGetEventInput(input: EventChangeInput): GetEventInput { return { calendarId: input.change.calendarId, eventId: input.change.event.id }; }

/** Return why a delivered change cannot be recorded, or an empty string. */
export function invalidInputReason(input: EventChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.calendarId?.trim() || !input.change.event?.id?.trim()) return 'change.calendarId and change.event.id are required';
  if (!Object.values(EventChangeKind).includes(input.change.changeKind)) return 'change.changeKind must be created, updated, or cancelled';
  return '';
}

/** Records one Google Calendar event change started by the eventChanged Trigger. */
export class EventChangeFlow implements Flow<EventChangeInput> {
  private readonly record = new RecordEventChange();
  private readonly complete = new CompleteEventChange();
  /** Bind the Google Calendar connection before Worker registration. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'GoogleCalendarEventChangeTypeScript'; }
  /** Record the change, read it back, and complete. */
  getSteps(): StepList<EventChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetEventStep<EventChangeInput>({
        stepType: readChangedEventStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed event back, so the record holds its current state rather than the sync snapshot.' },
        mapToOperationInput: mapToGetEventInput,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [eventChangeAttribute, eventChangeOutcomeAttribute] }; }
  /** Declare the eventChanged binding that starts this Flow, with its calendar picker. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineEventChangedTriggerBinding({ connectionName: ConnectionName, bindingName: EventChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedCalendar', unitId: 'calendarPicker', label: 'Watched calendar', required: false,
      description: 'Choose the calendar whose created, updated, and cancelled events start this Flow; the list comes from this connection\'s Google Calendar list and saves the calendar ID and name. Leave it unsaved to watch the authorized account\'s primary calendar. The first start records existing events as a baseline and starts no Flow for them. Restart the Worker after saving; a changed calendar records a new baseline.',
      bindings: [{ port: 'calendarId', jsonPointer: '/calendarId' }, { port: 'calendarName', jsonPointer: '/calendarName' }],
    }] } })];
  }
}

/** Validate and persist the change, then complete or read the event back. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered change, completing at once for a cancellation and otherwise reading the event back."
export class RecordEventChange implements Step<EventChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordEventChange'; }
  /** Complete a cancellation without a read; read any other change back from Google. */
  execute(context: Context, input: EventChangeInput): StepDecision {
    const reason = invalidInputReason(input);
    if (reason) return forceFail(reason);
    eventChangeAttribute.set(context, input);
    if (input.change.changeKind === EventChangeKind.Cancelled) {
      const outcome: ChangeOutcome = { calendarId: input.change.calendarId, eventId: input.change.event.id, changeKind: input.change.changeKind };
      eventChangeOutcomeAttribute.set(context, outcome);
      return gracefulComplete(outcome);
    }
    return goTo(new StepRef('ReadChangedEvent'), input);
  }
}

/** Record the current event as the outcome. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the event read back from Google Calendar as the outcome and complete the Flow."
export class CompleteEventChange implements Step<QueryResult<Event>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteEventChange'; }
  /** Complete the Flow with the change and the current event. */
  execute(context: Context, result: QueryResult<Event>): StepDecision {
    const { change } = eventChangeAttribute.get(context);
    const outcome: ChangeOutcome = { calendarId: change.calendarId, eventId: change.event.id, changeKind: change.changeKind, current: result.value };
    eventChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
