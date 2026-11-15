// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  UIEventTypePickerPortEventTypeURI, UIUnitEventTypePicker, WebhookEventInviteeCreated, defineInviteeEventReceivedTriggerBinding, newGetScheduledEventStep,
  type Connection, type InviteeEvent, type ScheduledEvent,
} from '@superdurable/connector-calendly';

/** Static Dex Web connection that verifies webhooks and reads scheduled events. */
export const ConnectionName = 'calendly-scheduling';
/** The inviteeEventReceived binding whose invitee.created events start this Flow. */
export const InviteeCreatedTriggerBinding = 'invitee-created';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow. */
export const FlowIDPrefix = 'calendly-';
const readScheduledEventStepType = 'ReadScheduledEvent';

/** The Flow's start input: one verified invitee.created webhook. */
export interface Booking {
  /** Trigger event ID, such as invitee.created:<event id>:<invitee id>. */ eventId: string;
  /** The invitee's stable Calendly URI. */ inviteeUri: string;
  /** The invitee's email address. */ inviteeEmail: string;
  /** The invitee's name. */ inviteeName?: string;
  /** The scheduled event the invitee booked. */ scheduledEventUri: string;
  /** When Calendly created the webhook event, as RFC 3339 UTC. */ bookedAt: string;
}

/** The found getScheduledEvent outcome the Flow stores. */
export interface RecordedScheduledEvent { /** The found branch. */ branch: string; /** The event Calendly returned. */ scheduledEvent: ScheduledEvent }

/** The verified booking. */
export const bookingAttribute = new Attribute<Booking>('calendly-booking', jsonCodec<Booking>());
/** The scheduled event read back from Calendly. */
export const scheduledEventAttribute = new Attribute<RecordedScheduledEvent>('calendly-scheduled-event', jsonCodec<RecordedScheduledEvent>());

/** Records one booked invitee and its scheduled event. */
export class InviteeRecorderFlow implements Flow<Booking> {
  private readonly recordBooking = new RecordBooking();
  private readonly recordScheduledEvent = new RecordScheduledEvent();
  /** Bind the Calendly connection that reads scheduled events. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'CalendlyInviteeRecorder'; }
  /** Register the record, read, and outcome Steps. */
  getSteps(): StepList<Booking> {
    return StepList.startStep(this.recordBooking).otherSteps(
      newGetScheduledEventStep<Booking>({
        stepType: readScheduledEventStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'calendly', groupLabel: 'Calendly', explanation: 'Read the booked scheduled event back from Calendly by its URI.' },
        mapToOperationInput: mapToGetScheduledEventInput,
        found: this.recordScheduledEvent,
      }),
      this.recordScheduledEvent,
    );
  }
  /** Register the booking and scheduled-event Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [bookingAttribute, scheduledEventAttribute] }; }
  /** Declare the inviteeEventReceived binding that starts this Flow, with its event type unit. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineInviteeEventReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: InviteeCreatedTriggerBinding, configurationUI: { units: [{
      id: 'eventType', unitId: UIUnitEventTypePicker, label: 'Event type', required: false,
      description: 'Select the Calendly event type whose new bookings start this Flow; the picker lists the connected user\'s event types and stores the event type URI. Leave it empty to record bookings of every event type.',
      bindings: [{ port: UIEventTypePickerPortEventTypeURI, jsonPointer: '/eventTypeUri' }],
    }] } })];
  }
}

/** The application's admission rule: only an active invitee.created event starts a Flow. */
export function acceptBooking(event: TriggerEvent<InviteeEvent>): boolean {
  return event.payload.event === WebhookEventInviteeCreated && event.payload.invitee.status === 'active';
}

/** Derive the Flow ID from the event ID, so every redelivery maps to one Flow. */
export function resolveFlowId(event: TriggerEvent<InviteeEvent>): string { return FlowIDPrefix + event.id.replaceAll(':', '-'); }

/** Copy the verified booking into the Flow's start input. */
export function mapToFlowInput(event: TriggerEvent<InviteeEvent>): Booking {
  const invitee = event.payload.invitee;
  return { eventId: event.id, inviteeUri: invitee.uri, inviteeEmail: invitee.email, ...(invitee.name ? { inviteeName: invitee.name } : {}), scheduledEventUri: invitee.scheduledEventUri, bookedAt: event.payload.createdAt };
}

/** Read the event the invitee booked. */
export function mapToGetScheduledEventInput(booking: Booking) { return { scheduledEventUri: booking.scheduledEventUri }; }

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Persist the verified booking before reading its scheduled event."
/** Persist the verified booking. */
export class RecordBooking implements Step<Booking> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordBooking'; }
  /** Store the booking and read its scheduled event. */
  execute(context: Context, booking: Booking): StepDecision {
    if (!booking.eventId || !booking.scheduledEventUri) return forceFail('a booking requires its event ID and scheduled event URI');
    bookingAttribute.set(context, booking);
    return goTo(new StepRef(readScheduledEventStepType), booking);
  }
}

// dex:group group-id:calendly group-label:"Calendly"
// dex:explanation text:"Persist the scheduled event Calendly returned and complete the Flow."
/** Persist the found scheduled event and complete. */
export class RecordScheduledEvent implements Step<QueryResult<ScheduledEvent>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordScheduledEvent'; }
  /** Store the event and complete the Flow with it. */
  execute(context: Context, result: QueryResult<ScheduledEvent>): StepDecision {
    const recorded: RecordedScheduledEvent = { branch: result.branch, scheduledEvent: result.value };
    scheduledEventAttribute.set(context, recorded);
    return gracefulComplete(recorded);
  }
}
