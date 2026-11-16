// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  MeetingChangeKindSchema, MeetingChangeRemoved, defineMeetingChangedTriggerBinding, newGetMeetingStep,
  type Connection, type GetMeetingInput, type Meeting, type MeetingChangedEvent,
} from '@superdurable/connector-zoom';

/** Static Dex Web connection for Zoom. */
export const ConnectionName = 'zoom-meeting-watcher';
/** The stable meetingChanged binding that starts this Flow. */
export const MeetingChangedTriggerBinding = 'host-meeting-changes';

/** One meetingChanged delivery: the stable Trigger event ID beside the delivered change. */
export interface MeetingChangeInput { /** Names the Flow too. */ triggerEventId: string; /** The delivered change. */ change: MeetingChangedEvent }
/** The Flow result: the changed meeting and, unless it is gone, its state read back from Zoom. */
export interface MeetingChangeOutcome { /** The changed meeting. */ meetingId: number; /** The delivered kind. */ changeKind: string; /** Read back from Zoom; absent for a removed or deleted meeting. */ current?: Meeting }

/** The delivered change. */
export const meetingChangeAttribute = new Attribute<MeetingChangeInput>('zoom-meeting-change', jsonCodec<MeetingChangeInput>());
/** The recorded outcome. */
export const meetingChangeOutcomeAttribute = new Attribute<MeetingChangeOutcome>('zoom-meeting-change-outcome', jsonCodec<MeetingChangeOutcome>());

/** Records one Zoom meeting change and reads a scheduled or updated meeting back. */
export class MeetingChangeFlow implements Flow<MeetingChangeInput> {
  private readonly record = new RecordMeetingChange();
  private readonly complete = new CompleteMeetingChange();
  /** Bind the static Zoom connection. */ constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError(`meeting change Flow requires the ${ConnectionName} connection`);
  }
  /** Stable Flow type. */ getFlowType(): string { return 'ZoomMeetingChangeTypeScript'; }
  /** Start at the record Step, which the Trigger starts the Flow at. */
  getSteps(): StepList<MeetingChangeInput> {
    return StepList.startStep(this.record).otherSteps(newGetMeetingStep<MeetingChangeInput>({
      stepType: 'ReadChangedMeeting', connection: this.connection, connectionName: ConnectionName,
      annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed meeting back, so the record holds its full agenda and settings rather than the listed summary.' },
      mapToOperationInput: mapToGetMeetingInput, found: this.complete,
      // Business reaction: a meeting deleted after the poll is recorded without a current state.
      notFound: this.complete,
    }), this.complete);
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [meetingChangeAttribute, meetingChangeOutcomeAttribute] }; }
  /** Declare the meetingChanged binding; Dex Web shows no field for it. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineMeetingChangedTriggerBinding({ connectionName: ConnectionName, bindingName: MeetingChangedTriggerBinding })];
  }
}

/** Name one Flow per change, so a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<MeetingChangedEvent>): string { return `zoom-meeting-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<MeetingChangedEvent>): MeetingChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to reschedules. */
export function acceptEveryMeetingChange(): boolean { return true; }
/** Read the changed meeting by its ID. */
export function mapToGetMeetingInput(input: MeetingChangeInput): GetMeetingInput { return { meetingId: input.change.meeting.id }; }

/** Return why a delivered change is unusable, or '' when it is valid. */
export function validateMeetingChangeInput(input: MeetingChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!(input.change?.meeting?.id >= 1)) return 'change.meeting.id is required';
  if (!MeetingChangeKindSchema.safeParse(input.change.changeKind).success) return 'change.changeKind must be scheduled, updated, or removed';
  return '';
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the change; complete at once for a removed meeting, otherwise read it back."
class RecordMeetingChange implements Step<MeetingChangeInput> {
  getStepType(): string { return 'RecordMeetingChange'; }
  execute(context: Context, input: MeetingChangeInput): StepDecision {
    const problem = validateMeetingChangeInput(input);
    if (problem) return forceFail(problem);
    meetingChangeAttribute.set(context, input);
    if (input.change.changeKind === MeetingChangeRemoved) {
      const outcome: MeetingChangeOutcome = { meetingId: input.change.meeting.id, changeKind: input.change.changeKind };
      meetingChangeOutcomeAttribute.set(context, outcome);
      return gracefulComplete(outcome);
    }
    return goTo(new StepRef('ReadChangedMeeting'), input);
  }
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the meeting read back, or none when deleted before the read, and complete."
class CompleteMeetingChange implements Step<QueryResult<Meeting>> {
  getStepType(): string { return 'CompleteMeetingChange'; }
  execute(context: Context, result: QueryResult<Meeting>): StepDecision {
    const change = meetingChangeAttribute.get(context);
    const outcome: MeetingChangeOutcome = { meetingId: change.change.meeting.id, changeKind: change.change.changeKind, ...(result.branch === 'found' ? { current: result.value } : {}) };
    meetingChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
