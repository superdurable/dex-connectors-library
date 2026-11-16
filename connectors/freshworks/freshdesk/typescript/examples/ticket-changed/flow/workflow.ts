// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  TicketChangeCreated, TicketChangeUpdated, defineTicketChangedTriggerBinding, newGetTicketStep,
  type Connection, type GetTicketInput, type TicketChangeKind, type TicketChangedEvent, type TicketDetails,
} from '@superdurable/connector-freshdesk';

/** Static Dex Web connection for Freshdesk. */
export const ConnectionName = 'freshdesk-ticket-watcher';
/** The stable ticketChanged binding that starts this Flow. */
export const TicketChangedTriggerBinding = 'helpdesk-ticket-changes';
const readChangedTicketStepType = 'ReadChangedTicket';

/** One ticketChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface TicketChange { /** The Trigger event ID that also names the Flow. */ triggerEventId: string; /** The delivered change. */ change: TicketChangedEvent }

/** The Flow result: the changed ticket read back from Freshdesk. */
export interface TicketChangeOutcome {
  /** The changed ticket. */ ticketId: number;
  /** The delivered change kind. */ changeKind: TicketChangeKind;
  /** The ticket with its requester and first conversations. */ current: TicketDetails;
}

/** The delivered change. */ export const ticketChangeAttribute = new Attribute<TicketChange>('freshdesk-ticket-change', jsonCodec<TicketChange>());
/** The recorded outcome. */ export const ticketChangeOutcomeAttribute = new Attribute<TicketChangeOutcome>('freshdesk-ticket-change-outcome', jsonCodec<TicketChangeOutcome>());

/** Records one Freshdesk ticket change. */
export class TicketChangedFlow implements Flow<TicketChange> {
  private readonly record = new RecordTicketChange();
  private readonly complete = new CompleteTicketChange();
  /** Bind the Freshdesk connection at registration time. */ constructor(private readonly connection: Connection) {
    if (connection?.reference.name !== ConnectionName) throw new TypeError('ticket change Flow requires the freshdesk-ticket-watcher connection');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FreshdeskTicketChange'; }
  /** Register the record, read, and complete Steps. */
  getSteps(): StepList<TicketChange> {
    return StepList.startStep(this.record).otherSteps(
      newGetTicketStep<TicketChange>({
        stepType: readChangedTicketStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed ticket back with its requester and first conversations, which the ticket list omits.' },
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

/** Admit every change; an application narrows it, for example to urgent tickets. */
export function acceptEveryTicketChange(): boolean { return true; }

/** Name one Flow per ticket version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<TicketChangedEvent>): string { return `freshdesk-ticket-change-${event.id}`; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<TicketChangedEvent>): TicketChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read the changed ticket. */
export function mapToGetTicketInput(change: TicketChange): GetTicketInput { return { ticketId: change.change.ticket.id }; }

/** Validate and persist the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered ticket change before reading the ticket back."
export class RecordTicketChange implements Step<TicketChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTicketChange'; }
  /** Store the change and read the ticket. */
  execute(context: Context, input: TicketChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!(input.change?.ticket?.id > 0)) return forceFail('change.ticket.id is required');
    if (input.change.changeKind !== TicketChangeCreated && input.change.changeKind !== TicketChangeUpdated) return forceFail('change.changeKind must be created or updated');
    ticketChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedTicket'), input);
  }
}

/** Complete with the ticket's current state. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the ticket read back from Freshdesk as the outcome and complete the Flow."
export class CompleteTicketChange implements Step<QueryResult<TicketDetails>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteTicketChange'; }
  /** Save and return the outcome. */
  execute(context: Context, result: QueryResult<TicketDetails>): StepDecision {
    const change = ticketChangeAttribute.get(context);
    const outcome: TicketChangeOutcome = { ticketId: change.change.ticket.id, changeKind: change.change.changeKind, current: result.value };
    ticketChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
