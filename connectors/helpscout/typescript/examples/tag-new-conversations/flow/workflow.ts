// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  WebhookEventConversationCreated, defineConversationEventTriggerBinding, newGetConversationStep, newUpdateConversationStep,
  type Connection, type ConversationDetails, type ConversationEvent, type UpdateConversationOutput,
} from '@superdurable/connector-helpscout';

/** Static Dex Web connection name. */
export const ConnectionName = 'helpscout-support';
/** The conversationEvent binding whose convo.created events start this Flow. */
export const NewConversationTriggerBinding = 'new-conversations';
/** Precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow. */
export const FlowIDPrefix = 'helpscout-';
/** Marks a conversation this Flow tagged. */
export const TriagedTag = 'dex-triaged';

/** Flow start input mapped from one conversation webhook: identities only, never message bodies. */
export interface NewConversationInput { eventId: string; event: string; conversationId: number; mailboxId: number }
/** Durable tagging record. */
export interface TaggingRecord { request: NewConversationInput; stage: 'reading' | 'tagging' | 'completed'; status?: string; newestThreadTypes?: string[]; tags?: string[] }

/** Tagging record, the default home of Flow state. */
export const taggingRecord = new Attribute<TaggingRecord>('helpscout-new-conversation-tagging', jsonCodec<TaggingRecord>());

/** A new Help Scout conversation starts the Flow, which reads it and tags it dex-triaged. */
export class TagNewConversationFlow implements Flow<NewConversationInput> {
  private readonly record = new RecordNewConversation();
  private readonly inspect = new InspectConversation();
  private readonly complete = new CompleteTagging();

  /** Bind the Help Scout connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Help Scout connection is required'); }

  /** Stable Flow type. */ getFlowType(): string { return 'HelpScoutTagNewConversation'; }

  /** Register the application and connector Steps; only happy-path branches are wired. */
  getSteps(): StepList<NewConversationInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetConversationStep<NewConversationInput>({
        stepType: 'ReadHelpScoutConversation', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'helpscout', groupLabel: 'Help Scout', explanation: 'Read the new conversation with its five newest threads.' },
        mapToOperationInput: input => ({ conversationId: input.conversationId, threadLimit: 5 }),
        found: this.inspect,
      }),
      this.inspect,
      newUpdateConversationStep<TaggingRecord>({
        stepType: 'TagHelpScoutConversation', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'helpscout', groupLabel: 'Help Scout', explanation: 'Add the dex-triaged tag; a repeated attempt finds it applied and writes nothing.' },
        mapToOperationInput: record => ({ conversationId: record.request.conversationId, addTags: [TriagedTag] }),
        updated: this.complete,
      }),
      this.complete,
    );
  }

  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [taggingRecord] }; }

  /** The conversationEvent binding that starts this Flow; literals keep its unit static for Flow Definition tooling. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineConversationEventTriggerBinding({ connectionName: ConnectionName, bindingName: NewConversationTriggerBinding, configurationUI: { units: [{
      id: 'inbox', unitId: 'mailboxPicker', label: 'Inbox',
      description: 'Select the Help Scout inbox whose new conversations start this Flow; the picker lists the inboxes the connected user can access and stores the inbox ID. Leave it empty to tag new conversations of every inbox.',
      required: false, bindings: [{ port: 'mailboxId', jsonPointer: '/mailboxId' }],
    }] } })];
  }
}

/** Store the request before reading the conversation. */
// dex:group group-id:tagging group-label:"Tagging"
// dex:explanation text:"Store the Help Scout webhook identities before reading the conversation."
export class RecordNewConversation implements Step<NewConversationInput> {
  /** Stable Step type. */ getStepType(): string { return 'RecordNewHelpScoutConversation'; }
  /** Record the request and read the conversation. */
  execute(context: Context, input: NewConversationInput): StepDecision {
    taggingRecord.set(context, { request: input, stage: 'reading' });
    return goTo(new StepRef('ReadHelpScoutConversation'), input);
  }
}

/** Record the status and newest thread types before tagging. */
// dex:group group-id:tagging group-label:"Tagging"
// dex:explanation text:"Record the conversation status and newest thread types before tagging it."
export class InspectConversation implements Step<QueryResult<ConversationDetails>> {
  /** Stable Step type. */ getStepType(): string { return 'InspectHelpScoutConversation'; }
  /** Record what was read and tag the conversation. */
  execute(context: Context, result: QueryResult<ConversationDetails>): StepDecision {
    const record: TaggingRecord = { ...taggingRecord.get(context), stage: 'tagging', status: result.value.conversation.status,
      newestThreadTypes: (result.value.threads ?? []).map(thread => thread.type) };
    taggingRecord.set(context, record);
    return goTo(new StepRef('TagHelpScoutConversation'), record);
  }
}

/** Complete with the conversation's tags. */
// dex:group group-id:tagging group-label:"Tagging"
// dex:explanation text:"Store the tags Help Scout returned and complete the Flow."
export class CompleteTagging implements Step<MutationResult<UpdateConversationOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteHelpScoutTagging'; }
  /** Record completion. */
  execute(context: Context, result: MutationResult<UpdateConversationOutput>): StepDecision {
    const record: TaggingRecord = { ...taggingRecord.get(context), stage: 'completed', tags: result.value.conversation.tags ?? [] };
    taggingRecord.set(context, record);
    return gracefulComplete(record);
  }
}

/** Admit only a convo.created event for an active conversation. */
export function acceptNewConversation(event: TriggerEvent<ConversationEvent>): boolean {
  return event.payload.event === WebhookEventConversationCreated && event.payload.conversation.status === 'active';
}

/** Build the Flow ID from the Trigger event ID, so a redelivery of the same body maps to one Flow. */
export function resolveFlowId(event: TriggerEvent<ConversationEvent>): string {
  return FlowIDPrefix + event.id.replaceAll(':', '-');
}

/** Map one conversation webhook to the Flow input. */
export function mapToFlowInput(event: TriggerEvent<ConversationEvent>): NewConversationInput {
  return { eventId: event.id, event: event.payload.event, conversationId: event.payload.conversation.id, mailboxId: event.payload.conversation.mailboxId };
}
