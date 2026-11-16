// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  defineConversationCreatedTriggerBinding, newGetConversationStep,
  type Connection, type ConversationCreatedEvent, type ConversationDetails, type GetConversationInput,
} from '@superdurable/connector-reamaze';

/** Static Dex Web connection for the Re:amaze brand. */
export const ConnectionName = 'reamaze-brand';
/** The conversationCreated binding whose conversations start this Flow. */
export const ConversationCreatedTriggerBinding = 'new-conversations';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered conversation maps to one Flow. */
export const FlowIDPrefix = 'reamaze-new-conversation-';
/** Re:amaze statuses 3 (Spam) and 9 (Spam identified by AI). */
export const SpamStatuses: ReadonlySet<number> = new Set([3, 9]);

/** One conversationCreated delivery: the stable Trigger event ID beside the delivered event. */
export interface NewConversationInput {
  /** Names the Flow too. */ triggerEventId: string;
  /** The delivered event. */ created: ConversationCreatedEvent;
}
/** The Flow result: the conversation as read back from Re:amaze. */
export interface Intake {
  /** The new conversation's slug. */ conversationId: string;
  /** The conversation's channel slug. */ channel: string;
  /** The conversation subject. */ subject?: string;
  /** The customer's email, omitted for a contact without one. */ requesterEmail?: string;
  /** The conversation's Re:amaze status when the Flow read it back. */ status: number;
  /** The messages and notes read back, at most the default message limit. */ messageCount: number;
}

/** The delivered conversation. */
export const newConversationAttribute = new Attribute<NewConversationInput>('reamaze-new-conversation', jsonCodec<NewConversationInput>());
/** The recorded intake. */
export const intakeAttribute = new Attribute<Intake>('reamaze-new-conversation-intake', jsonCodec<Intake>());

/** Records one new Re:amaze conversation and reads it back with its newest messages. */
export class NewConversationFlow implements Flow<NewConversationInput> {
  private readonly record = new RecordNewConversation();
  private readonly complete = new CompleteNewConversation();
  /** Bind the static Re:amaze connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`new conversation Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'ReamazeNewConversationTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<NewConversationInput> {
    return StepList.startStep(this.record).otherSteps(newGetConversationStep<NewConversationInput>({
      stepType: 'ReadNewConversation', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'intake', groupLabel: 'Intake', explanation: 'Read the new conversation back with its newest messages, so the intake reflects its current state.' },
      mapToOperationInput: mapToGetConversationInput, found: this.complete,
    }), this.complete);
  }
  /** Register the delivery and intake Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [newConversationAttribute, intakeAttribute] }; }
  /** Declare the conversationCreated binding with the channelSlugEntry unit; Dex Web reads these literals statically. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineConversationCreatedTriggerBinding({ connectionName: ConnectionName, bindingName: ConversationCreatedTriggerBinding, configurationUI: { units: [{
      id: 'watchedChannel', unitId: 'channelSlugEntry', label: 'Watched channel', required: false,
      description: 'Enter the slug of the Re:amaze channel whose new conversations start this Flow, such as support; find it in the slug field of GET https://{brand}.reamaze.io/api/v1/channels or under Settings > Channels in Re:amaze. Leave it blank to start a Flow for new conversations in every channel of the brand. The first start records existing conversations as a baseline and starts no Flow for them. Restart the Worker after saving; a changed channel records a new baseline.',
      bindings: [{ port: 'channel', jsonPointer: '/channel' }],
    }] } })];
  }
}

/** Start no Flow for spam; the Trigger lists every view, so archived conversations still arrive. */
export function acceptCustomerConversation(event: TriggerEvent<ConversationCreatedEvent>): boolean { return !SpamStatuses.has(event.payload.conversation.status); }
/** Name one Flow per conversation, so a redelivered conversation finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<ConversationCreatedEvent>): string { return FlowIDPrefix + event.id; }
/** Keep the Trigger event ID beside the delivered conversation. */
export function mapToFlowInput(event: TriggerEvent<ConversationCreatedEvent>): NewConversationInput { return { triggerEventId: event.id, created: event.payload }; }
/** Read the new conversation by its slug. */
export function mapToGetConversationInput(input: NewConversationInput): GetConversationInput { return { conversationId: input.created.conversation.id }; }
/** Summarize a conversation read back from Re:amaze. */
export function newIntake(details: ConversationDetails): Intake {
  const conversation = details.conversation;
  return {
    conversationId: conversation.id, channel: conversation.channel.slug, ...(conversation.subject ? { subject: conversation.subject } : {}),
    ...(conversation.requester?.email ? { requesterEmail: conversation.requester.email } : {}), status: conversation.status, messageCount: details.messages?.length ?? 0,
  };
}
/** Return why a delivery is unusable, or '' when it names its event, brand, and conversation. */
export function validateNewConversation(input: NewConversationInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.created?.brand?.trim() || !input.created.conversation?.id?.trim()) return 'created.brand and created.conversation.id are required';
  return '';
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the delivered conversation, then read it back from Re:amaze."
class RecordNewConversation implements Step<NewConversationInput> {
  getStepType(): string { return 'RecordNewConversation'; }
  execute(context: Context, input: NewConversationInput): StepDecision {
    const problem = validateNewConversation(input);
    if (problem) return forceFail(problem);
    newConversationAttribute.set(context, input);
    return goTo(new StepRef('ReadNewConversation'), input);
  }
}

// dex:group group-id:intake group-label:"Intake"
// dex:explanation text:"Record the intake summary from the conversation read back and complete the Flow."
class CompleteNewConversation implements Step<QueryResult<ConversationDetails>> {
  getStepType(): string { return 'CompleteNewConversation'; }
  execute(context: Context, result: QueryResult<ConversationDetails>): StepDecision {
    const intake = newIntake(result.value);
    intakeAttribute.set(context, intake);
    return gracefulComplete(intake);
  }
}
