// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  TicketChangeCreated, TicketChangeUpdated, defineTicketChangedTriggerBinding, newGetTicketStep,
  type Connection, type GetTicketInput, type TicketChangeKind, type TicketChangedEvent, type TicketDetails,
} from '../../../src/index.js';

/** The static Dex Web connection for Zoho Desk. */
export const ConnectionName = 'zoho-desk-ticket-watcher';
/** The stable ticketChanged binding that starts this Flow. */
export const TicketChangedTriggerBinding = 'desk-ticket-changes';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered change finds its existing Flow. */
export const FlowIDPrefix = 'zoho-desk-ticket-change-';
const readChangedTicketStepType = 'ReadChangedTicket';

/** One ticketChanged delivery. */
export interface TicketChange {
  /** The stable Trigger event ID that also names the Flow. */ triggerEventId: string;
  /** The delivered change. */ change: TicketChangedEvent;
}

/** The Flow result and the value of its outcome Attribute. */
export interface ChangeOutcome {
  /** The changed ticket. */ ticketId: string;
  /** The delivered change kind. */ changeKind: TicketChangeKind;
  /** The ticket read back from Zoho Desk with its newest threads and comments. */ current: TicketDetails;
}

/** The delivered change. */
export const ticketChangeAttribute = new Attribute<TicketChange>('zoho-desk-ticket-change', jsonCodec<TicketChange>());
/** The ticket read back from Zoho Desk. */
export const ticketChangeOutcomeAttribute = new Attribute<ChangeOutcome>('zoho-desk-ticket-change-outcome', jsonCodec<ChangeOutcome>());

/** Records one Zoho Desk ticket change: it reads the ticket back and stores its current state. */
export class TicketChangeFlow implements Flow<TicketChange> {
  private readonly recordChange = new RecordTicketChange();
  private readonly completeChange = new CompleteTicketChange();
  /** Bind the Zoho Desk connection that reads changed tickets. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ticket change Flow requires the zoho-desk-ticket-watcher connection');
  }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return 'ZohoDeskTicketChangeTypeScript'; }
  /** Register the record, read, and outcome Steps. */
  getSteps(): StepList<TicketChange> {
    return StepList.startStep(this.recordChange).otherSteps(
      newGetTicketStep<TicketChange>({
        stepType: readChangedTicketStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed ticket back with its newest threads and comments, which the search page leaves out.' },
        mapToOperationInput: mapToGetTicketInput,
        found: this.completeChange,
      }),
      this.completeChange,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [ticketChangeAttribute, ticketChangeOutcomeAttribute] }; }
  /** Declare the ticketChanged binding and its department picker for Dex Web. */
  // Dex reads ConfigurationUI only from literals, so the unit and port IDs are not imported constants.
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineTicketChangedTriggerBinding({ connectionName: ConnectionName, bindingName: TicketChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedDepartment', unitId: 'departmentPicker', label: 'Watched department', required: false,
      description: "Choose the Zoho Desk department whose created and changed tickets start this Flow; the list comes from the connection's organization and saves the department ID and name. Leave it blank to watch every department the connection's agent can see. The first start records recent tickets as a baseline and starts no Flow for them. Restart the Worker after saving; a changed department records a new baseline.",
      bindings: [
        { port: 'departmentId', jsonPointer: '/departmentId' },
        { port: 'departmentName', jsonPointer: '/departmentName' },
      ],
    }] } })];
  }
}

/** Admit every change; an application narrows it, for example to new tickets. */
export function acceptEveryTicketChange(): boolean { return true; }

/** Name one Flow per ticket version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<TicketChangedEvent>): string { return FlowIDPrefix + event.id; }

/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<TicketChangedEvent>): TicketChange { return { triggerEventId: event.id, change: event.payload }; }

/** Read the changed ticket with the default thread and comment limits. */
export function mapToGetTicketInput(input: TicketChange): GetTicketInput { return { ticketId: input.change.ticket.id }; }

/** Validate and persist the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered ticket change, then read the ticket back."
export class RecordTicketChange implements Step<TicketChange> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordTicketChange'; }
  /** Store the change and read the ticket back. */
  execute(context: Context, input: TicketChange): StepDecision {
    if (!input.triggerEventId?.trim()) return forceFail('triggerEventId is required');
    if (!input.change?.orgId?.trim() || !input.change.ticket?.id?.trim()) return forceFail('change.orgId and change.ticket.id are required');
    if (input.change.changeKind !== TicketChangeCreated && input.change.changeKind !== TicketChangeUpdated) return forceFail('change.changeKind must be created or updated');
    ticketChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedTicket'), input);
  }
}

/** Persist the ticket read back and complete. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the ticket read back from Zoho Desk as the outcome and complete the Flow."
export class CompleteTicketChange implements Step<QueryResult<TicketDetails>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteTicketChange'; }
  /** Store the outcome and complete the Flow with it. */
  execute(context: Context, result: QueryResult<TicketDetails>): StepDecision {
    const change = ticketChangeAttribute.get(context);
    const outcome: ChangeOutcome = { ticketId: change.change.ticket.id, changeKind: change.change.changeKind, current: result.value };
    ticketChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
