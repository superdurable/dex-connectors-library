// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from "@superdurable/dex";
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from "@superdurable/connectors-sdk";
import {
  TopicConversationUserCreated, UIConversationTopicPickerPortTopics, UIUnitConversationTopicPicker, defineConversationEventTriggerBinding, newGetConversationStep,
  type Connection, type ConversationDetails, type ConversationEvent,
} from "../../src/index.js";

/** Static Dex Web connection name for the Intercom workspace. */
export const ConnectionName = "intercom-support-inbox";
/** conversationEvent binding whose events start this Flow. */
export const InboundTriggerBinding = "inbound-conversation";
/** Prefix of every Flow ID; the conversation ID follows it. */
export const FlowIDPrefix = "intercom-inbound-conversation-";
const readInboundConversationStepType = "ReadInboundConversation";

/** Start input mapped from one notification, or a conversation ID entered in Dex Web Start Flow. */
export interface InboundConversation {
  /** Intercom conversation ID, a string of digits. */ conversationId: string;
  /** Intercom notification ID, or empty for Start Flow. */ notificationId?: string;
  /** Webhook topic, or empty for Start Flow. */ topic?: string;
  /** Intercom workspace app_id, when the notification names one. */ workspaceId?: string;
  /** When Intercom created the notification, as RFC 3339. */ notifiedAt?: string;
}

/** The Flow's result: the conversation's identity, state, and starting customer. */
export interface InboundConversationOutcome {
  /** Intercom conversation ID. */ conversationId: string;
  /** Notification that started the Flow, or empty for Start Flow. */ notificationId: string;
  /** Intercom's open, closed, or snoozed state when read. */ conversationState: string;
  /** Email of the contact who started the conversation, or empty when Intercom has none. */ customerEmail: string;
  /** Number of conversation parts Intercom reported. */ partCount: number;
}

/** The inbound notification, recorded before the conversation is read. */
export const inboundConversation = new Attribute<InboundConversation>("intercom-inbound-conversation", jsonCodec<InboundConversation>());

/** A new Intercom conversation starts this Flow, which reads it and completes with its starting customer. */
export class InboundConversationFlow implements Flow<InboundConversation> {
  private readonly record = new RecordInboundConversation();
  private readonly completed = new CompleteInboundConversation();

  /** Bind the static project or scripted connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError("inbound-conversation requires its static Intercom connection");
  }

  /** Stable Flow type. */ getFlowType(): string { return "IntercomInboundConversation"; }

  /** Register the application and connector Steps. */
  getSteps(): StepList<InboundConversation> {
    return StepList.startStep(this.record).otherSteps(
      newGetConversationStep<InboundConversation>({
        stepType: readInboundConversationStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: "intercom", groupLabel: "Intercom", explanation: "Read the new conversation and its latest parts as plain text." },
        mapToOperationInput: mapToGetConversationInput,
        found: this.completed,
      }),
      this.completed,
    );
  }

  /** Persist the inbound notification. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [inboundConversation] }; }

  /** Declare the conversationEvent binding and the topic picker Dex Web shows for it. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineConversationEventTriggerBinding({
      connectionName: ConnectionName, bindingName: InboundTriggerBinding,
      configurationUI: { units: [{
        id: "topics", unitId: UIUnitConversationTopicPicker, label: "Topics that start the Flow", required: false,
        description: "Select conversation.user.created, the topic Intercom sends once for each conversation a customer starts; subscribe the same topic on the Intercom app's Configure > Webhooks page. The Flow itself admits only that topic, so selecting none accepts every supported topic and still starts Flows only for new conversations.",
        bindings: [{ port: UIConversationTopicPickerPortTopics, jsonPointer: "/topics" }],
      }] },
    })];
  }
}

/** Record the notification before reading the conversation. */
// dex:group group-id:inbound group-label:"Inbound"
// dex:explanation text:"Record the inbound notification before reading its conversation."
export class RecordInboundConversation implements Step<InboundConversation> {
  /** Stable Step type. */ getStepType(): string { return "RecordInboundConversation"; }
  /** Store the notification and read the conversation. */
  execute(context: Context, input: InboundConversation): StepDecision {
    if (!/^[0-9]{1,24}$/.test(input.conversationId ?? "")) throw new TypeError("conversationId must be an Intercom conversation ID of digits");
    inboundConversation.set(context, input);
    return goTo(new StepRef(readInboundConversationStepType), input);
  }
}

/** Complete with the conversation's state and starting customer. */
// dex:group group-id:inbound group-label:"Inbound"
// dex:explanation text:"Complete with the conversation state and the customer who started it."
export class CompleteInboundConversation implements Step<QueryResult<ConversationDetails>> {
  /** Stable Step type. */ getStepType(): string { return "CompleteInboundConversation"; }
  /** Build the outcome from the recorded notification and the read conversation. */
  execute(context: Context, result: QueryResult<ConversationDetails>): StepDecision {
    return gracefulComplete(chooseInboundConversationOutcome(inboundConversation.get(context), result.value));
  }
}

/** The application's admission rule: only a new conversation starts a Flow. */
export function acceptInboundConversation(event: TriggerEvent<ConversationEvent>): boolean {
  return event.payload.topic === TopicConversationUserCreated && event.payload.conversation.id !== "";
}

/** Derive the Flow ID from the conversation ID, so a redelivery starts no second Flow. */
export function resolveFlowId(event: TriggerEvent<ConversationEvent>): string {
  return FlowIDPrefix + event.payload.conversation.id;
}

/** Copy the notification's identity into the Flow's start input; no message text enters Flow state. */
export function mapToFlowInput(event: TriggerEvent<ConversationEvent>): InboundConversation {
  return {
    conversationId: event.payload.conversation.id, notificationId: event.payload.notificationId, topic: event.payload.topic,
    ...(event.payload.workspaceId ? { workspaceId: event.payload.workspaceId } : {}), notifiedAt: event.payload.notifiedAt,
  };
}

/** Read the five latest parts of the inbound conversation. */
export function mapToGetConversationInput(input: InboundConversation) {
  return { conversationId: input.conversationId, latestPartLimit: 5 };
}

/** Choose the outcome: the source author's email, or the empty string when Intercom sent none. */
export function chooseInboundConversationOutcome(inbound: InboundConversation, details: ConversationDetails): InboundConversationOutcome {
  return {
    conversationId: details.conversation.id, notificationId: inbound.notificationId ?? "", conversationState: details.conversation.state,
    customerEmail: details.conversation.source.author.email ?? "", partCount: details.partCount,
  };
}
