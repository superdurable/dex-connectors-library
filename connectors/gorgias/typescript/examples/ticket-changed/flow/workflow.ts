// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineTicketChangedTriggerBinding, isTicketEventType, newGetTicketStep, type Connection, type GetTicketInput, type TicketChangedEvent, type TicketDetails } from '@superdurable/connector-gorgias';

/** Static Dex Web connection for Gorgias. */
export const ConnectionName = 'gorgias-ticket-watcher';
/** The stable ticketChanged binding that starts this Flow. */
export const TicketChangedTriggerBinding = 'helpdesk-ticket-events';
const readChangedTicketStepType = 'ReadChangedTicket';

/** One ticketChanged delivery: the stable Trigger event ID beside the delivered event. */
export interface TicketChange { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered event. */ event: TicketChangedEvent }

/** The Flow result: the changed ticket read back from Gorgias. */
export interface TicketChangeOutcome {
  /** The changed ticket. */ ticketId: number;
  /** The delivered Gorgias event type, such as ticket-closed. */ eventType: string;
  /** The ticket read back with its customer and latest messages. */ current: TicketDetails;
}

/** The delivered event. */ export const ticketChangeAttribute = new Attribute<TicketChange>('gorgias-ticket-change', jsonCodec<TicketChange>());
/** The recorded outcome. */ export const ticketChangeOutcomeAttribute = new Attribute<TicketChangeOutcome>('gorgias-ticket-change-outcome', jsonCodec<TicketChangeOutcome>());

/** Records one Gorgias ticket event and reads the ticket back. */
export class TicketChangedFlow implements Flow<TicketChange> {
  private readonly record = new RecordTicketChange();
  private readonly complete = new CompleteTicketChange();
  /** Bind the Gorgias connection at registration time. */ constructor(private readonly connection: Connection) {
    if (connection?.reference.name !== ConnectionName) throw new TypeError('ticket change Flow requires the gorgias-ticket-watcher connection');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'GorgiasTicketChangeTypeScript'; }
  /** Register the record, read, and complete Steps. */
  getSteps(): StepList<TicketChange> {
    return StepList.startStep(this.record).otherSteps(
      newGetTicketStep<TicketChange>({
        stepType: readChangedTicketStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the ticket back with its customer and latest messages, which the event names only by ID.' },
        mapToOperationInput: mapToGetTicketInput,
        found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [ticketChangeAttribute, ticketChangeOutcomeAttribute] }; }
  /** Declare the ticketChanged binding; setup commands cannot send the HTTP Basic API key, so it has no picker units. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineTicketChangedTriggerBinding({ connectionName: ConnectionName, bindingName: TicketChangedTriggerBinding })];
  }
}

/** Admit every event; an application narrows it, for example to rule-made events. */
export function acceptEveryTicketChange(): boolean { return true; }

/** Name one Flow per Gorgias event; a redelivered event finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<TicketChangedEvent>): string { return `gorgias-ticket-change-${event.id}`; }

/** Keep the Trigger event ID beside the delivered event. */
export function mapToFlowInput(event: TriggerEvent<TicketChangedEvent>): TicketChange { return { triggerEventId: event.id, event: event.payload }; }

/** Read the ticket the event names. */
export function mapToGetTicketInput(change: TicketChange): GetTicketInput { return { ticketId: change.event.ticketId }; }

/** Return why a delivered event is unusable, or '' when it is valid. */
export function validateTicketChange(input: TicketChange): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!(input.event?.eventId > 0)) return 'event.eventId is required';
  if (!(input.event.ticketId > 0)) return 'event.ticketId is required';
  if (!isTicketEventType(input.event.eventType)) return 'event.eventType must be a ticket event type that ticketChanged reads';
  return '';
}

/** Validate and persist the delivered event. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered ticket event before reading the ticket back."
export class RecordTicketChange implements Step<TicketChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTicketChange'; }
  /** Store the event and read the ticket. */
  execute(context: Context, input: TicketChange): StepDecision {
    const problem = validateTicketChange(input);
    if (problem) return forceFail(problem);
    ticketChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedTicket'), input);
  }
}

/** Complete with the ticket's current state. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the ticket read back from Gorgias as the outcome and complete the Flow."
export class CompleteTicketChange implements Step<QueryResult<TicketDetails>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteTicketChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<TicketDetails>): StepDecision {
    const change = ticketChangeAttribute.get(context);
    const outcome: TicketChangeOutcome = { ticketId: change.event.ticketId, eventType: change.event.eventType, current: result.value };
    ticketChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
