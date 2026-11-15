// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepMovement, StepRef, deadEnd, goTo, gracefulComplete, jsonCodec, rpc, type Context, type Flow, type PersistenceSchema, type RPCResult, type Step, type StepDecision, type StepOptions } from '@superdurable/dex';
import type { ConnectorConfigurationUI, MutationResult, QueryResult, TriggerBindingDefinition, TriggerEvent, TriggerFilter } from '@superdurable/connectors-sdk';
import {
  MessageReceivedTriggerConfigurationSchema, ReplyReceivedTriggerConfigurationSchema, defineMessageReceivedTriggerBinding, defineReplyReceivedTriggerBinding, newGetMessageStep, newReplyToMessageStep,
  parseMailbox, type GmailConnection, type Message, type MessageEvent, type MessageMatcher, type MessageReceivedTriggerConfiguration, type ReplyReceivedTriggerConfiguration,
  type SendMessageOutput,
} from '@superdurable/connector-google-gmail';
import type { Configuration } from '@superdurable/connectors-sdk/projectconfig';
import { z } from 'zod';

/** Static Dex Web connection name. */
export const ConnectionName = 'gmail-inbox';
/** Static root-message Trigger binding. */
export const StartTriggerBinding = 'gmail-thread-reply-start';
/** Static reply Trigger binding. */
export const ReplyTriggerBinding = 'gmail-thread-reply-received';
const flowType = 'GmailThreadReplyFlow';
const readMessageStepType = 'ReadReceivedEmail';
const replyMessageStepType = 'ReplyToReceivedEmail';

/** Flow start input mapped from the root-message event. */
export interface ThreadReplyInput { eventId: string; primaryEmail: string; messageId: string; threadId: string }
/** Lifecycle of one Gmail thread; empty while the Flow reads its root message. */
export type ThreadReplyStatus = '' | 'waitingForReply' | 'replying' | 'completed';
/** Durable thread state. */
export interface ThreadState { input: ThreadReplyInput; rootMessage?: Message; replyEventId?: string; replyMessageId?: string; status: ThreadReplyStatus }
/** RPC input mapped from the reply event. */
export interface ReceiveEmailReplyInput { eventId: string; messageId: string }
/** RPC result. */
export interface ReplyResult { accepted: boolean; duplicate: boolean; status: ThreadReplyStatus }

/** Dex Web configuration of the completion reply Step. */
export const ReplyMessageConfigurationUI: ConnectorConfigurationUI = { units: [{ id: 'replyBody', unitId: 'textInput', label: 'Completion reply', required: true,
  description: 'Enter the plain-text email body sent after the Flow receives the configured reply.', bindings: [{ port: 'text', jsonPointer: '/textBody' }] }] };

/** The completion reply body that Dex Web saves for the ReplyToReceivedEmail Step. */
export const ReplyMessageConfigurationSchema = z.strictObject({ textBody: z.string() });
/** Saved settings of the completion reply Step. */
export type ReplyMessageConfiguration = z.infer<typeof ReplyMessageConfigurationSchema>;

/**
 * Load the completion reply body saved for this Flow's replyToMessage Step, as the Go example's
 * LoadOperationConfiguration does. It throws when Dex Web saved none or the saved value is invalid.
 */
export function loadReplyMessageConfiguration(configuration: Configuration): ReplyMessageConfiguration {
  const saved = configuration.operationConfigurations.find(operation => operation.connectorId === 'gmail' && operation.connectionName === ConnectionName
    && operation.operationId === 'replyToMessage' && operation.flowType === flowType && operation.stepType === replyMessageStepType);
  if (!saved) throw new Error(`load the saved reply message: no configuration for ${flowType} Step ${replyMessageStepType}`);
  const parsed = ReplyMessageConfigurationSchema.safeParse(saved.configuration);
  if (!parsed.success) throw new Error(`load the saved reply message: ${parsed.error.issues.map(issue => issue.message).join('; ')}`);
  return parsed.data;
}

/** Thread state, the default home of Flow state. */
export const threadState = new Attribute<ThreadState>('gmail-thread-reply-state', jsonCodec<ThreadState>());
/** The completion reply's provider result, for operator inspection. */
export const replyResult = new Attribute<MutationResult<SendMessageOutput>>('gmail-thread-reply-result', jsonCodec<MutationResult<SendMessageOutput>>());
const threadLock: StepOptions = { executeLockAttributes: [threadState.lock()] };

/** A matching root message starts the Flow, a matching reply invokes the RPC, and the Flow replies in the thread. */
export class GmailThreadReplyFlow implements Flow<ThreadReplyInput> {
  private readonly initialize = new InitializeThread();
  private readonly loaded = new MessageLoaded();
  private readonly sent = new ReplySent();

  /** Bind the Gmail connection and the saved completion reply, which defaults to Processing complete. */
  constructor(private readonly connection: GmailConnection, private readonly replyMessageConfiguration: ReplyMessageConfiguration = { textBody: 'Processing complete.' }) {}

  /** Stable Flow type. */ getFlowType(): string { return flowType; }

  /** Register the application and connector Steps. */
  getSteps(): StepList<ThreadReplyInput> {
    return StepList.startStep(this.initialize).otherSteps(
      newGetMessageStep<ThreadReplyInput>({
        stepType: readMessageStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Read the received Gmail message that started the Flow.' },
        mapToOperationInput: input => ({ messageId: input.messageId }),
        read: this.loaded,
      }),
      this.loaded,
      newReplyToMessageStep<ThreadState>({
        stepType: replyMessageStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'gmail', groupLabel: 'Gmail', explanation: 'Reply after the received email Trigger invokes the typed RPC.' },
        configurationUI: ReplyMessageConfigurationUI,
        mapToOperationInput: state => ({ messageId: state.replyMessageId ?? '', textBody: this.replyMessageConfiguration.textBody }),
        sent: this.sent,
        resultAttribute: replyResult,
      }),
      this.sent,
    );
  }

  /** Persist the thread state and the reply result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [threadState, replyResult] }; }

  /** Static Trigger bindings, with the configuration units Dex Web shows for each. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [
      defineMessageReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: StartTriggerBinding, configurationUI: { units: [
        { id: 'query', unitId: 'searchQueryInput', label: 'Gmail search query', required: false, description: 'Enter Gmail search syntax such as label:inbox is:unread; leave blank to inspect the most recent inbox messages.', bindings: [{ port: 'query', jsonPointer: '/searchQuery' }] },
        { id: 'message', unitId: 'textInput', label: 'Start message contains', required: false, description: 'Enter plain text that the root message must contain; leave blank to match any message content.', bindings: [{ port: 'text', jsonPointer: '/messageMatcher/messageContains' }] },
        { id: 'senders', unitId: 'emailListInput', label: 'Allowed root senders', required: false, description: 'Enter complete sender email addresses; leave blank to allow any sender matched by the Gmail query.', bindings: [{ port: 'emails', jsonPointer: '/messageMatcher/senderEmails' }] },
      ] } }),
      defineReplyReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: ReplyTriggerBinding, configurationUI: { units: [
        { id: 'query', unitId: 'searchQueryInput', label: 'Gmail search query', required: false, description: 'Enter Gmail search syntax that can find thread replies; leave blank to inspect recent inbox messages.', bindings: [{ port: 'query', jsonPointer: '/searchQuery' }] },
        { id: 'message', unitId: 'textInput', label: 'Reply message contains', required: false, description: 'Enter plain text that a reply must contain; leave blank to match any reply content.', bindings: [{ port: 'text', jsonPointer: '/replyMatcher/messageContains' }] },
        { id: 'senders', unitId: 'emailListInput', label: 'Allowed reply senders', required: false, description: 'Enter complete sender email addresses; leave blank to allow any sender matched by the Gmail query.', bindings: [{ port: 'emails', jsonPointer: '/replyMatcher/senderEmails' }] },
      ] } }),
    ];
  }

  /** Accept the first matching reply once and schedule the completion reply; a repeated event ID is a duplicate. */
  @rpc({ lockAttributes: [threadState.lock()] })
  receiveEmailReply(context: Context, input: ReceiveEmailReplyInput): RPCResult<ReplyResult> {
    const state = threadState.get(context);
    if (state.replyEventId === input.eventId) return { output: { accepted: false, duplicate: true, status: state.status } };
    // The Flow is still reading its root message. The error keeps the reply pending, so the Trigger
    // retries it after the Flow starts waiting instead of consuming it as a no-op.
    if (state.status === '') throw new Error('Gmail thread is still reading its root message');
    if (state.status !== 'waitingForReply') return { output: { accepted: false, duplicate: false, status: state.status } };
    const next: ThreadState = { ...state, status: 'replying', replyEventId: input.eventId, replyMessageId: input.messageId };
    threadState.set(context, next);
    return { output: { accepted: true, duplicate: false, status: next.status }, nextSteps: [StepMovement.of(new StepRef('ReplyToReceivedEmail'), next)] };
  }

  /** Read the thread state. */
  @rpc({ lockAttributes: [threadState.lock()] })
  getThreadStatus(context: Context): RPCResult<ThreadState> { return { output: threadState.get(context) }; }
}

/** Persist the thread identity before reading its root message. */
// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Persist the Gmail thread identity before reading its root message."
export class InitializeThread implements Step<ThreadReplyInput> {
  /** Stable Step type. */ getStepType(): string { return 'InitializeThread'; }
  /** Lock the thread state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Store the identity and read the root message. */
  execute(context: Context, input: ThreadReplyInput): StepDecision {
    threadState.set(context, { input, status: '' });
    return goTo(new StepRef('ReadReceivedEmail'), input);
  }
}

/** Store the root message before waiting for a reply. */
// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Store the received Gmail message before waiting for a reply Trigger."
export class MessageLoaded implements Step<QueryResult<Message>> {
  /** Stable Step type. */ getStepType(): string { return 'MessageLoaded'; }
  /** Lock the thread state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Wait for the reply RPC. */
  execute(context: Context, result: QueryResult<Message>): StepDecision {
    threadState.set(context, { ...threadState.get(context), rootMessage: result.value, status: 'waitingForReply' });
    return deadEnd();
  }
}

/** Complete after Gmail confirms the reply. */
// dex:group group-id:gmail group-label:"Gmail"
// dex:explanation text:"Complete after Gmail confirms the thread reply was sent."
export class ReplySent implements Step<MutationResult<SendMessageOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'ReplySent'; }
  /** Lock the thread state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Record completion. */
  execute(context: Context): StepDecision {
    const state: ThreadState = { ...threadState.get(context), status: 'completed' };
    threadState.set(context, state);
    return gracefulComplete(state);
  }
}

/** Build a stable, PII-safe Flow ID from the connection name and Gmail thread ID, so a root and its replies share it. */
export function resolveFlowId(event: TriggerEvent<MessageEvent>): string {
  return `gmail-thread-reply-${ConnectionName}-${event.payload.threadId}`;
}

/** Map a root event to the Flow input. */
export function mapToFlowInput(event: TriggerEvent<MessageEvent>): ThreadReplyInput {
  return { eventId: event.id, primaryEmail: event.payload.primaryEmail, messageId: event.payload.messageId, threadId: event.payload.threadId };
}

/** Map a reply event to the RPC input: only the event and message IDs. */
export function mapToReceiveEmailReplyInput(event: TriggerEvent<MessageEvent>): ReceiveEmailReplyInput {
  return { eventId: event.id, messageId: event.payload.messageId };
}

/** The application's root-message admission rule, built from the binding configuration. */
export function newStartTriggerFilter(configuration: MessageReceivedTriggerConfiguration): TriggerFilter<MessageEvent> {
  return newMessageTriggerFilter(MessageReceivedTriggerConfigurationSchema.parse(configuration).messageMatcher ?? {}, false);
}

/** The application's reply admission rule, built from the binding configuration. */
export function newReplyTriggerFilter(configuration: ReplyReceivedTriggerConfiguration): TriggerFilter<MessageEvent> {
  return newMessageTriggerFilter(ReplyReceivedTriggerConfigurationSchema.parse(configuration).replyMatcher ?? {}, true);
}

function newMessageTriggerFilter(matcher: MessageMatcher, isReplyRequired: boolean): TriggerFilter<MessageEvent> {
  return event => {
    const message = event.payload;
    if (!event.id || !message.primaryEmail || !message.messageId || !message.threadId || message.isReply !== isReplyRequired) return false;
    if (matcher.messageContains && !`${message.subject}\n${message.snippet ?? ''}`.toLowerCase().includes(matcher.messageContains.toLowerCase())) return false;
    const senders = matcher.senderEmails ?? [];
    if (senders.length === 0) return true;
    const sender = parseMailbox(message.from)?.address.toLowerCase();
    return sender !== undefined && senders.some(allowed => parseMailbox(allowed)?.address.toLowerCase() === sender);
  };
}
