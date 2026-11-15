// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, deadEnd, goTo, gracefulComplete, jsonCodec, rpc, type Context, type Flow, type PersistenceSchema, type RPCResult, type Step, type StepDecision, type StepOptions } from '@superdurable/dex';
import type { MutationResult, TriggerBindingDefinition, TriggerEvent, TriggerFilter } from '@superdurable/connectors-sdk';
import {
  ButtonClickedTriggerConfigurationSchema, ButtonStyleDanger, ButtonStylePrimary, ChannelThreadCreatedTriggerConfigurationSchema, UIChannelPickerPortChannelID, UIMemberPickerPortMemberIDs,
  UITextInputPortText, UIUnitChannelPicker, UIUnitMemberPicker, UIUnitTextInput, defineButtonClickedTriggerBinding, defineChannelThreadCreatedTriggerBinding, newPostButtonMessageStep,
  newPostThreadReplyStep, type ButtonClickEvent, type ButtonClickedTriggerConfiguration, type ChannelThreadCreatedTriggerConfiguration, type Connection, type MessageEvent, type PostMessageOutput,
} from '@superdurable/connector-slack';

/** Static Dex Web connection name. */
export const ConnectionName = 'slack-button-approval';
/** Static root-message Trigger binding that starts an approval. */
export const StartTriggerBinding = 'slack-button-approval-start';
/** Static button click Trigger binding that decides an approval. */
export const ButtonTriggerBinding = 'slack-button-approval-decision';
/** Action ID of the Approve button; a code constant, so a user never types it. */
export const ApproveActionID = 'approve';
/** Action ID of the Reject button; a code constant, so a user never types it. */
export const RejectActionID = 'reject';
const postButtonsStepType = 'PostSlackApprovalButtons';
const postDecisionStepType = 'PostSlackDecision';
const approvalRequestText = 'Approve this request?';

/** Flow start input mapped from the root-message event. */
export interface ApprovalInput { eventId: string; teamId: string; channelId: string; threadTimestamp: string; requesterUserId: string }
/** Lifecycle of one approval. */
export type ApprovalStatus = 'postingButtons' | 'waitingForDecision' | 'postingDecision' | 'completed';
/** Outcome of the deciding click. */
export type ApprovalDecision = 'approved' | 'rejected';
/** Durable approval record. */
export interface ApprovalState {
  input: ApprovalInput; status: ApprovalStatus;
  buttonMessageTimestamp?: string; decision?: ApprovalDecision; decisionEventId?: string; deciderUserId?: string;
}
/** RPC input mapped from a click. */
export interface ReceiveButtonDecisionInput { eventId: string; actionId: string; userId: string; messageTimestamp: string }
/** Whether the click decided the approval. */
export interface DecisionResult { accepted: boolean; duplicate: boolean; status: ApprovalStatus | '' }

/** Approval state, the default home of Flow state. */
export const approvalState = new Attribute<ApprovalState>('slack-button-approval-state', jsonCodec<ApprovalState>());
const approvalLock: StepOptions = { executeLockAttributes: [approvalState.lock()] };

/** A matching root message starts the Flow, which replies with buttons and completes on the first decider's click. */
export class ButtonApprovalFlow implements Flow<ApprovalInput> {
  private readonly initialize = new InitializeApproval();
  private readonly buttonsPosted = new ApprovalButtonsPosted();
  private readonly decisionPosted = new ApprovalDecisionPosted();

  /** Bind the Slack connection. */
  constructor(private readonly connection: Connection) {}

  /** Stable Flow type. */ getFlowType(): string { return 'SlackButtonApprovalFlow'; }

  /** Register the application and connector Steps. */
  getSteps(): StepList<ApprovalInput> {
    return StepList.startStep(this.initialize).otherSteps(
      newPostButtonMessageStep<ApprovalState>({
        stepType: postButtonsStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'request', groupLabel: 'Approval request', explanation: 'Reply in the Slack thread with Approve and Reject buttons.' },
        mapToOperationInput: state => ({
          channelId: state.input.channelId, threadTimestamp: state.input.threadTimestamp, text: approvalRequestText,
          // Each value carries the request's thread, so a click finds its Flow without Slack's thread report.
          buttons: [
            { actionId: ApproveActionID, text: 'Approve', value: state.input.threadTimestamp, style: ButtonStylePrimary },
            { actionId: RejectActionID, text: 'Reject', value: state.input.threadTimestamp, style: ButtonStyleDanger },
          ],
        }),
        sent: this.buttonsPosted,
      }),
      this.buttonsPosted,
      newPostThreadReplyStep<ApprovalState>({
        stepType: postDecisionStepType, connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'decision', groupLabel: 'Decision', explanation: 'Reply in the Slack thread with the decision and who made it.' },
        mapToOperationInput: state => ({
          channelId: state.input.channelId, threadTimestamp: state.input.threadTimestamp,
          text: `${state.decision === 'rejected' ? 'Rejected' : 'Approved'} by <@${state.deciderUserId ?? ''}>.`,
        }),
        sent: this.decisionPosted,
      }),
      this.decisionPosted,
    );
  }

  /** Persist the approval state. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [approvalState] }; }

  /** Static Trigger bindings, with the configuration units Dex Web shows for each. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [
      defineChannelThreadCreatedTriggerBinding({ connectionName: ConnectionName, bindingName: StartTriggerBinding, configurationUI: { units: [
        { id: 'channel', unitId: UIUnitChannelPicker, label: 'Approval channel', description: 'Select the joined Slack channel whose new root messages start an approval; the picker stores its stable channel ID, and the Flow replies there with Approve and Reject buttons.', required: true, bindings: [{ port: UIChannelPickerPortChannelID, jsonPointer: '/channelId' }] },
        { id: 'message', unitId: UIUnitTextInput, label: 'Request message contains', description: 'Enter plain text that the root message must contain, such as request approval; matching ignores case, and blank matches any message content.', required: false, bindings: [{ port: UITextInputPortText, jsonPointer: '/threadTriggerMatcher/messageContains' }] },
        { id: 'members', unitId: UIUnitMemberPicker, label: 'Members allowed to request', description: 'Select workspace members whose root messages may start an approval; leave empty to allow any member posting in the selected channel.', required: false, bindings: [{ port: UIMemberPickerPortMemberIDs, jsonPointer: '/threadTriggerMatcher/posterUserIds' }] },
      ] } }),
      defineButtonClickedTriggerBinding({ connectionName: ConnectionName, bindingName: ButtonTriggerBinding, configurationUI: { units: [
        { id: 'channel', unitId: UIUnitChannelPicker, label: 'Approval channel', description: 'Select the same joined Slack channel as the request Trigger; clicks on Approve and Reject in other channels are ignored, and the picker stores the stable channel ID.', required: true, bindings: [{ port: UIChannelPickerPortChannelID, jsonPointer: '/channelId' }] },
        { id: 'approvers', unitId: UIUnitMemberPicker, label: 'Members allowed to decide', description: 'Select the workspace members whose Approve or Reject click decides; a click by anyone else is ignored, and an empty selection lets any member who can see the message decide.', required: false, bindings: [{ port: UIMemberPickerPortMemberIDs, jsonPointer: '/clickerUserIds' }] },
      ] } }),
    ];
  }

  /**
   * Record the first Approve or Reject click on the posted button message and schedule the decision reply.
   * Later clicks, a redelivery of the deciding click, and clicks on another message change nothing.
   */
  @rpc({ lockAttributes: [approvalState.lock()] })
  receiveButtonDecision(context: Context, input: ReceiveButtonDecisionInput): RPCResult<DecisionResult> {
    const decided = decideButtonClick(approvalState.get(context), input);
    if (!decided.result.accepted) return { output: decided.result };
    approvalState.set(context, decided.state);
    return { output: decided.result, nextSteps: [{ step: new StepRef(postDecisionStepType), input: decided.state }] };
  }

  /** Read the approval state. */
  @rpc({ lockAttributes: [approvalState.lock()] })
  getApprovalStatus(context: Context): RPCResult<ApprovalState> { return { output: approvalState.get(context) }; }
}

/**
 * Return the state after input and whether input decided the approval. It throws while the buttons are still
 * being posted, so the Trigger retries the click until the Flow waits for it.
 */
export function decideButtonClick(state: ApprovalState, input: ReceiveButtonDecisionInput): { state: ApprovalState; result: DecisionResult } {
  if (state.decisionEventId === input.eventId) return { state, result: { accepted: false, duplicate: true, status: state.status } };
  if (state.status === 'postingButtons') throw new Error('Slack approval buttons are still being posted');
  // A button with the same action and value on another message, such as a copy, does not decide.
  if (state.status !== 'waitingForDecision' || input.messageTimestamp !== state.buttonMessageTimestamp) return { state, result: { accepted: false, duplicate: false, status: state.status } };
  const decided: ApprovalState = {
    ...state, status: 'postingDecision', decision: input.actionId === RejectActionID ? 'rejected' : 'approved', decisionEventId: input.eventId, deciderUserId: input.userId,
  };
  return { state: decided, result: { accepted: true, duplicate: false, status: decided.status } };
}

/** Persist the thread identity before posting the buttons. */
// dex:group group-id:request group-label:"Approval request"
// dex:explanation text:"Persist the Slack thread identity before posting the approval buttons."
export class InitializeApproval implements Step<ApprovalInput> {
  /** Stable Step type. */ getStepType(): string { return 'InitializeApproval'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return approvalLock; }
  /** Store the identity and post the buttons. */
  execute(context: Context, input: ApprovalInput): StepDecision {
    const state: ApprovalState = { input, status: 'postingButtons' };
    approvalState.set(context, state);
    return goTo(new StepRef(postButtonsStepType), state);
  }
}

/** Record the posted button message, then wait for a click. */
// dex:group group-id:request group-label:"Approval request"
// dex:explanation text:"Record the posted button message, then wait for an Approve or Reject click."
export class ApprovalButtonsPosted implements Step<MutationResult<PostMessageOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'ApprovalButtonsPosted'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return approvalLock; }
  /** Wait for the decision RPC. */
  execute(context: Context, result: MutationResult<PostMessageOutput>): StepDecision {
    approvalState.set(context, { ...approvalState.get(context), buttonMessageTimestamp: result.value.message.timestamp, status: 'waitingForDecision' });
    return deadEnd();
  }
}

/** Complete after Slack confirms the decision reply. */
// dex:group group-id:decision group-label:"Decision"
// dex:explanation text:"Complete after Slack confirms the decision reply was posted."
export class ApprovalDecisionPosted implements Step<MutationResult<PostMessageOutput>> {
  /** Stable Step type. */ getStepType(): string { return 'ApprovalDecisionPosted'; }
  /** Lock the approval state. */ getStepOptions(): StepOptions { return approvalLock; }
  /** Record completion. */
  execute(context: Context): StepDecision {
    const state: ApprovalState = { ...approvalState.get(context), status: 'completed' };
    approvalState.set(context, state);
    return gracefulComplete(state);
  }
}

/** Name the approval after the thread its root message started. */
export function resolveFlowId(event: TriggerEvent<MessageEvent>): string {
  return approvalFlowId(event.payload.teamId, event.payload.channelId, event.payload.threadTimestamp);
}

/** Find the approval from the request's thread timestamp, which each posted button carries as its value. */
export function resolveButtonFlowId(event: TriggerEvent<ButtonClickEvent>): string {
  return approvalFlowId(event.payload.teamId, event.payload.channelId, event.payload.value ?? '');
}

function approvalFlowId(teamId: string, channelId: string, threadTimestamp: string): string {
  return `slack-button-approval-${teamId}-${channelId}-${threadTimestamp}`;
}

/** Map a root event to the Flow input. */
export function mapToFlowInput(event: TriggerEvent<MessageEvent>): ApprovalInput {
  return { eventId: event.id, teamId: event.payload.teamId, channelId: event.payload.channelId, threadTimestamp: event.payload.threadTimestamp, requesterUserId: event.payload.userId };
}

/** Map a click to the RPC input: the event ID, button, clicker, and clicked message only. */
export function mapToReceiveButtonDecisionInput(event: TriggerEvent<ButtonClickEvent>): ReceiveButtonDecisionInput {
  return { eventId: event.id, actionId: event.payload.actionId, userId: event.payload.userId, messageTimestamp: event.payload.messageTimestamp };
}

/** The application's root-message admission rule, built from the binding configuration. */
export function newStartTriggerFilter(configuration: ChannelThreadCreatedTriggerConfiguration): TriggerFilter<MessageEvent> {
  const parsed = ChannelThreadCreatedTriggerConfigurationSchema.parse(configuration);
  const matcher = parsed.threadTriggerMatcher ?? {};
  return event => {
    const message = event.payload;
    if (!event.id || !message.teamId || message.channelId !== parsed.channelId || !message.userId || message.threadTimestamp !== message.timestamp) return false;
    if (matcher.messageContains && !message.text.toLowerCase().includes(matcher.messageContains.toLowerCase())) return false;
    const posters = matcher.posterUserIds ?? [];
    return posters.length === 0 || posters.includes(message.userId);
  };
}

/** The application's admission rule for Approve and Reject clicks, built from the binding configuration. */
export function newButtonTriggerFilter(configuration: ButtonClickedTriggerConfiguration): TriggerFilter<ButtonClickEvent> {
  const parsed = ButtonClickedTriggerConfigurationSchema.parse(configuration);
  return event => {
    const click = event.payload;
    if (!event.id || !click.teamId || click.channelId !== parsed.channelId || !click.value || !click.userId) return false;
    if (click.actionId !== ApproveActionID && click.actionId !== RejectActionID) return false;
    const deciders = parsed.clickerUserIds ?? [];
    return deciders.length === 0 || deciders.includes(click.userId);
  };
}
