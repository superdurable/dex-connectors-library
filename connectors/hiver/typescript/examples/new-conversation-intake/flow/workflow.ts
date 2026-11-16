// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  defineConversationReceivedTriggerBinding, newGetConversationStep,
  type Connection, type ConversationDetails, type ConversationMessage, type ConversationReceivedEvent, type GetConversationInput,
} from '@superdurable/connector-hiver';

/** Static Dex Web connection whose shared inbox the Trigger polls. */
export const ConnectionName = 'hiver-support';
/** The conversationReceived binding whose conversations start this Flow. */
export const ConversationReceivedTriggerBinding = 'new-support-conversations';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered conversation maps to one Flow. */
export const FlowIDPrefix = 'hiver-new-conversation-';

/** One conversationReceived delivery: the stable Trigger event ID beside the delivered conversation. */
export interface ReceivedConversation {
  /** Names the Flow too. */ triggerEventId: string;
  /** The delivered conversation. */ received: ConversationReceivedEvent;
  /** When the poll found the conversation, as RFC 3339 UTC; Hiver's list carries no timestamp. */ detectedAt: string;
}
/** The Flow result: the conversation's status and message IDs as Hiver reported them on read-back. */
export interface IntakeOutcome {
  /** The polled shared inbox. */ inboxId: string;
  /** The new conversation. */ conversationId: string;
  /** The conversation's Hiver status when the Flow read it. */ status: string;
  /** Opens the conversation in Hiver for a signed-in user. */ privatePermalink?: string;
  /** The conversation's message IDs, which createSharedDraft accepts. */ messages: ConversationMessage[];
}

/** The delivered conversation. */
export const receivedConversationAttribute = new Attribute<ReceivedConversation>('hiver-received-conversation', jsonCodec<ReceivedConversation>());
/** The recorded intake. */
export const intakeOutcomeAttribute = new Attribute<IntakeOutcome>('hiver-conversation-intake', jsonCodec<IntakeOutcome>());

/** Records one new Hiver conversation and its message IDs. */
export class NewConversationIntakeFlow implements Flow<ReceivedConversation> {
  private readonly record = new RecordReceivedConversation();
  private readonly complete = new CompleteConversationIntake();
  /** Bind the static Hiver connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`new conversation intake Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'HiverNewConversationIntakeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<ReceivedConversation> {
    return StepList.startStep(this.record).otherSteps(newGetConversationStep<ReceivedConversation>({
      stepType: 'ReadReceivedConversation', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'intake', groupLabel: 'Intake', explanation: 'Read the new conversation from Hiver for its current status and message IDs.' },
      mapToOperationInput: mapToGetConversationInput, found: this.complete,
    }), this.complete);
  }
  /** Register the delivery and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [receivedConversationAttribute, intakeOutcomeAttribute] }; }
  /** Declare the conversationReceived binding with the inboxPicker unit; Dex Web reads these literals statically. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineConversationReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: ConversationReceivedTriggerBinding, configurationUI: { units: [{
      id: 'sharedInbox', unitId: 'inboxPicker', label: 'Shared inbox', required: true,
      description: "Choose the Hiver shared inbox whose new conversations start this Flow. Load inboxes lists the account's shared inboxes with the connection's API key and saves the chosen inbox's ID. To enter the ID instead, a Hiver admin finds it as the id of the inbox in GET https://api2.hiverhq.com/v1/inboxes; it is a short string of digits such as 105902 and is not secret. Conversations already in the inbox when the Worker first polls never start Flows. Restart the Worker after saving; choosing another inbox records a new baseline. It cannot be blank.",
      bindings: [{ port: 'inboxId', jsonPointer: '/inboxId' }],
    }] } })];
  }
}

/** Admit every new conversation; an application narrows it, for example to open ones. */
export function acceptEveryConversation(): boolean { return true; }
/** Name one Flow per conversation, so a redelivered conversation finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<ConversationReceivedEvent>): string { return FlowIDPrefix + event.id; }
/** Keep the Trigger event ID beside the delivered conversation. */
export function mapToFlowInput(event: TriggerEvent<ConversationReceivedEvent>): ReceivedConversation {
  return { triggerEventId: event.id, received: event.payload, detectedAt: event.occurredAt.toISOString() };
}
/** Read the delivered conversation from its inbox. */
export function mapToGetConversationInput(input: ReceivedConversation): GetConversationInput {
  return { inboxId: input.received.inboxId, conversationId: input.received.conversation.id };
}
/** Return why a delivery is unusable, or '' when it names its event, inbox, and conversation. */
export function validateReceivedConversation(input: ReceivedConversation): string {
  if (!input.triggerEventId?.trim() || !input.received?.inboxId?.trim() || !input.received.conversation?.id?.trim()) return 'a received conversation requires its Trigger event ID, inbox ID, and conversation ID';
  return '';
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the delivered conversation, then read it from Hiver."
class RecordReceivedConversation implements Step<ReceivedConversation> {
  getStepType(): string { return 'RecordReceivedConversation'; }
  execute(context: Context, input: ReceivedConversation): StepDecision {
    const problem = validateReceivedConversation(input);
    if (problem) return forceFail(problem);
    receivedConversationAttribute.set(context, input);
    return goTo(new StepRef('ReadReceivedConversation'), input);
  }
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the conversation's status and message IDs as the outcome and complete the Flow."
class CompleteConversationIntake implements Step<QueryResult<ConversationDetails>> {
  getStepType(): string { return 'CompleteConversationIntake'; }
  execute(context: Context, result: QueryResult<ConversationDetails>): StepDecision {
    const conversation = result.value.conversation;
    const outcome: IntakeOutcome = {
      inboxId: conversation.inboxId, conversationId: conversation.id, status: conversation.status,
      ...(conversation.privatePermalink ? { privatePermalink: conversation.privatePermalink } : {}), messages: result.value.messages ?? [],
    };
    intakeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
