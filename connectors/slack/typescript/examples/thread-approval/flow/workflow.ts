// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, deadEnd, goTo, gracefulComplete, jsonCodec, rpc, type Context, type Flow, type PersistenceSchema, type RPCResult, type Step, type StepDecision, type StepOptions } from '@superdurable/dex';
import type { ConnectorConfigurationUI, MutationResult, QueryResult, TriggerBindingDefinition, TriggerEvent, TriggerFilter } from '@superdurable/connectors-sdk';
import {
  ChannelThreadCreatedTriggerConfigurationSchema, ThreadReplyCreatedTriggerConfigurationSchema, UIChannelPickerPortChannelID, UIMemberPickerPortMemberIDs, UITextInputPortText,
  UIUnitChannelPicker, UIUnitMemberPicker, UIUnitTextInput, defineChannelThreadCreatedTriggerBinding, defineThreadReplyCreatedTriggerBinding, newListThreadMessagesStep, newPostThreadReplyStep,
  type ChannelThreadCreatedTriggerConfiguration, type Connection, type ListThreadMessagesOutput, type Message, type MessageEvent, type MessageMatcher, type PostMessageOutput,
  type ThreadReplyCreatedTriggerConfiguration,
} from '@superdurable/connector-slack';

/** Static Dex Web connection name. */
export const ConnectionName = 'slack-workspace';
/** Static root-message Trigger binding. */
export const StartTriggerBinding = 'slack-thread-approval-start';
/** Static reply Trigger binding. */
export const ReplyTriggerBinding = 'slack-thread-approval-reply';
const readThreadStepType = 'ReadSlackThread';
const postCompletionStepType = 'PostSlackCompletion';

/** Flow start input mapped from the root-message event. */
export interface ThreadApprovalInput { eventId: string; teamId: string; channelId: string; threadTimestamp: string }
/** Lifecycle of one approval thread. */
export type ThreadApprovalStatus = '' | 'waitingForReply' | 'postingReply' | 'completed';
/** Durable approval state. */
export interface ThreadState { input: ThreadApprovalInput; messages: Message[]; status: ThreadApprovalStatus; replyEventId?: string; replyUserId?: string }
/** RPC input mapped from the reply event. */
export interface ReceiveThreadReplyInput { eventId: string; userId: string }
/** RPC result. */
export interface ReplyResult { accepted: boolean; duplicate: boolean; status: ThreadApprovalStatus }

/** Dex Web configuration of the completion reply Step. */
export const PostCompletionConfigurationUI: ConnectorConfigurationUI = { units: [{ id: 'completionText', unitId: UIUnitTextInput, label: 'Completion reply', required: true,
  description: 'Enter the Slack message posted to the thread after the configured approval reply arrives.', bindings: [{ port: UITextInputPortText, jsonPointer: '/text' }] }] };

/** Approval state, the default home of Flow state. */
export const threadState = new Attribute<ThreadState>('slack-thread-approval-state', jsonCodec<ThreadState>());
const threadLock: StepOptions = { executeLockAttributes: [threadState.lock()] };

/** A matching root message starts the Flow, a matching reply approves it, and the Flow replies to the thread. */
export class ThreadApprovalFlow implements Flow<ThreadApprovalInput> {
  private readonly initialize = new InitializeSlackThread();
  private readonly loaded = new SlackThreadLoaded();
  private readonly posted = new SlackCompletionPosted();

  /** Bind the Slack connection and the completion reply text. */
  constructor(private readonly connection: Connection, private readonly completionText = 'Processing complete.') {}

  /** Stable Flow type. */ getFlowType(): string { return 'SlackThreadApprovalFlow'; }

  /** Register the application and connector Steps. */
  getSteps(): StepList<ThreadApprovalInput> {
    return StepList.startStep(this.initialize).otherSteps(
      newListThreadMessagesStep<ThreadApprovalInput>({
        stepType: readThreadStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'slack', groupLabel: 'Slack', explanation: 'Read the messages in the newly created Slack thread.' },
        mapToOperationInput: input => ({ channelId: input.channelId, threadTimestamp: input.threadTimestamp, pageSize: 15 }),
        read: this.loaded,
      }),
      this.loaded,
      newPostThreadReplyStep<ThreadState>({
        stepType: postCompletionStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'slack', groupLabel: 'Slack', explanation: 'Reply to the Slack thread after the configured reply Trigger invokes the RPC.' },
        configurationUI: PostCompletionConfigurationUI,
        mapToOperationInput: state => ({ channelId: state.input.channelId, threadTimestamp: state.input.threadTimestamp, text: this.completionText }),
        sent: this.posted,
      }),
      this.posted,
    );
  }

  /** Persist the approval state. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [threadState] }; }

  /** Static Trigger bindings, with the configuration units Dex Web shows for each. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [
      defineChannelThreadCreatedTriggerBinding({ connectionName: ConnectionName, bindingName: StartTriggerBinding, configurationUI: { units: [
        { id: 'channel', unitId: UIUnitChannelPicker, label: 'Approval channel', description: 'Select the joined Slack channel whose new root threads may start this Flow; the picker stores its stable channel ID.', required: true, bindings: [{ port: UIChannelPickerPortChannelID, jsonPointer: '/channelId' }] },
        { id: 'message', unitId: UIUnitTextInput, label: 'Start message contains', description: 'Enter plain text that the root thread message must contain; leave blank to match any message content.', required: false, bindings: [{ port: UITextInputPortText, jsonPointer: '/threadTriggerMatcher/messageContains' }] },
        { id: 'members', unitId: UIUnitMemberPicker, label: 'Members allowed to start', description: 'Select workspace members allowed to start the Flow; leave empty to allow any member posting in the selected channel.', required: false, bindings: [{ port: UIMemberPickerPortMemberIDs, jsonPointer: '/threadTriggerMatcher/posterUserIds' }] },
      ] } }),
      defineThreadReplyCreatedTriggerBinding({ connectionName: ConnectionName, bindingName: ReplyTriggerBinding, configurationUI: { units: [
        { id: 'channel', unitId: UIUnitChannelPicker, label: 'Approval channel', description: 'Select the same joined Slack channel used by the start Trigger; the picker stores its stable channel ID.', required: true, bindings: [{ port: UIChannelPickerPortChannelID, jsonPointer: '/channelId' }] },
        { id: 'message', unitId: UIUnitTextInput, label: 'Approval reply contains', description: 'Enter plain text that an approval reply must contain, such as approved; leave blank to match any reply content.', required: false, bindings: [{ port: UITextInputPortText, jsonPointer: '/threadReplyMatcher/messageContains' }] },
        { id: 'approvers', unitId: UIUnitMemberPicker, label: 'Members allowed to approve', description: 'Select the workspace members whose thread replies count as approval; at least one approver is required.', required: true, bindings: [{ port: UIMemberPickerPortMemberIDs, jsonPointer: '/threadReplyMatcher/posterUserIds' }] },
      ] } }),
    ];
  }

  /** Accept the first matching reply once and schedule the completion reply; a repeated event ID is a duplicate. */
  @rpc({ lockAttributes: [threadState.lock()] })
  receiveThreadReply(context: Context, input: ReceiveThreadReplyInput): RPCResult<ReplyResult> {
    const state = threadState.get(context);
    if (state.replyEventId === input.eventId) return { output: { accepted: false, duplicate: true, status: state.status } };
    // The Flow is still reading its root message. The error keeps the reply pending, so the Trigger
    // retries it after the Flow starts waiting instead of consuming it as a no-op.
    if (state.status === '') throw new Error('Slack thread is still reading its root message');
    if (state.status !== 'waitingForReply') return { output: { accepted: false, duplicate: false, status: state.status } };
    const next: ThreadState = { ...state, status: 'postingReply', replyEventId: input.eventId, replyUserId: input.userId };
    threadState.set(context, next);
    return { output: { accepted: true, duplicate: false, status: next.status }, nextSteps: [{ step: new StepRef(postCompletionStepType), input: next }] };
  }

  /** Read the approval state. */
  @rpc({ lockAttributes: [threadState.lock()] })
  getThreadStatus(context: Context): RPCResult<ThreadState> { return { output: threadState.get(context) }; }
}

/** Persist the thread identity before reading its messages. */
// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Persist the Slack thread identity before reading its messages."
export class InitializeSlackThread implements Step<ThreadApprovalInput> {
  /** Stable Step type. */ getStepType(): string { return 'InitializeSlackThread'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Store the identity and read the thread. */
  execute(context: Context, input: ThreadApprovalInput): StepDecision {
    threadState.set(context, { input, messages: [], status: '' });
    return goTo(new StepRef(readThreadStepType), input);
  }
}

/** Store the thread messages before waiting for a reply. */
// dex:group group-id:approval group-label:"Approval"
// dex:explanation text:"Store the Slack thread messages before waiting for a matching reply Trigger."
export class SlackThreadLoaded implements Step<QueryResult<ListThreadMessagesOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'SlackThreadLoaded'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Wait for the reply RPC. */
  execute(context: Context, result: QueryResult<ListThreadMessagesOutput>): StepDecision {
    threadState.set(context, { ...threadState.get(context), messages: result.value.messages ?? [], status: 'waitingForReply' });
    return deadEnd();
  }
}

/** Complete after Slack confirms the reply. */
// dex:group group-id:slack group-label:"Slack"
// dex:explanation text:"Complete after Slack confirms the thread reply was posted."
export class SlackCompletionPosted implements Step<MutationResult<PostMessageOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'SlackCompletionPosted'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return threadLock; }
  /** Record completion. */
  execute(context: Context): StepDecision {
    const state: ThreadState = { ...threadState.get(context), status: 'completed' };
    threadState.set(context, state);
    return gracefulComplete(state);
  }
}

/** Build the Flow ID from team, channel, and root timestamp, so the root and its replies share it. */
export function resolveFlowId(event: TriggerEvent<MessageEvent>): string {
  return `slack-thread-approval-${event.payload.teamId}-${event.payload.channelId}-${event.payload.threadTimestamp}`;
}

/** Map a root event to the Flow input. */
export function mapToFlowInput(event: TriggerEvent<MessageEvent>): ThreadApprovalInput {
  return { eventId: event.id, teamId: event.payload.teamId, channelId: event.payload.channelId, threadTimestamp: event.payload.threadTimestamp };
}

/** Map a reply event to the RPC input: only the event ID and the approver. */
export function mapToReceiveThreadReplyInput(event: TriggerEvent<MessageEvent>): ReceiveThreadReplyInput {
  return { eventId: event.id, userId: event.payload.userId };
}

/** The application's root-message admission rule, built from the binding configuration. */
export function newStartTriggerFilter(configuration: ChannelThreadCreatedTriggerConfiguration): TriggerFilter<MessageEvent> {
  const parsed = ChannelThreadCreatedTriggerConfigurationSchema.parse(configuration);
  return newMessageTriggerFilter(parsed.channelId, parsed.threadTriggerMatcher ?? {}, false);
}

/** The application's reply admission rule, built from the binding configuration. */
export function newReplyTriggerFilter(configuration: ThreadReplyCreatedTriggerConfiguration): TriggerFilter<MessageEvent> {
  const parsed = ThreadReplyCreatedTriggerConfigurationSchema.parse(configuration);
  return newMessageTriggerFilter(parsed.channelId, parsed.threadReplyMatcher ?? {}, true);
}

function newMessageTriggerFilter(channelId: string, matcher: MessageMatcher, isReplyRequired: boolean): TriggerFilter<MessageEvent> {
  return event => {
    const message = event.payload;
    const isReply = message.threadTimestamp !== '' && message.threadTimestamp !== message.timestamp;
    if (!event.id || !message.teamId || message.channelId !== channelId || !message.threadTimestamp || !message.userId || isReply !== isReplyRequired) return false;
    if (matcher.messageContains && !message.text.toLowerCase().includes(matcher.messageContains.toLowerCase())) return false;
    const posters = matcher.posterUserIds ?? [];
    return posters.length === 0 || posters.includes(message.userId);
  };
}
