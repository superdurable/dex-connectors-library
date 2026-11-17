// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { defineIssueEventReceivedTriggerBinding, type IssueEvent } from '@superdurable/connector-linear';

/** Static Linear connection in Dex Web. */ export const ConnectionName = 'linear-workspace';
/** Static issueEventReceived binding. */ export const IssueCreatedBinding = 'issue-created';
/** Verified event fields persisted as this Flow's input. */
export interface RecordedIssueEvent { /** Stable Trigger event ID. */ eventId: string; /** Change action. */ action: string; /** Issue UUID. */ issueId: string; /** Team UUID. */ teamId: string; /** Issue title. */ title: string; }
const eventAttribute = new Attribute('linear-issue-event', jsonCodec<RecordedIssueEvent>());

/** Persist each accepted new issue in one durable Flow. */
export class LinearIssueEventIntakeFlow implements Flow<RecordedIssueEvent> {
  private readonly record = new RecordIssueEvent();
  /** Stable Flow identity. */ getFlowType(): string { return 'LinearIssueEventIntake'; }
  /** Register the event Step. */ getSteps(): StepList<RecordedIssueEvent> { return StepList.startStep(this.record); }
  /** Register the persisted event Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [eventAttribute] }; }
  /** Expose the team picker for the binding's teamId filter in Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineIssueEventReceivedTriggerBinding({ connectionName: ConnectionName, bindingName: IssueCreatedBinding, configurationUI: { units: [{
      id: 'team', unitId: 'teamPicker', label: 'Team', required: false,
      description: 'Choose the Linear team whose new issues start this Flow. OAuth lists teams; with a personal API key, copy the team UUID from https://linear.app/settings/teams > the team > Copy team ID. Leave blank for every team sent by the webhook.',
      bindings: [{ port: 'teamId', jsonPointer: '/teamId' }],
    }] } })];
  }
}
/** Admit created issues outside the trash. */
export function acceptNewIssue(event: TriggerEvent<IssueEvent>): boolean { return event.payload.action === 'create' && !event.payload.issue.trashed; }
/** Reuse the event ID for Flow identity across webhook redeliveries. */
export function resolveFlowId(event: TriggerEvent<IssueEvent>): string { return `linear-issue-${event.id.replaceAll(':', '-')}`; }
/** Keep only nonsecret issue fields in the Flow start input. */
export function mapToFlowInput(event: TriggerEvent<IssueEvent>): RecordedIssueEvent {
  return { eventId: event.id, action: event.payload.action, issueId: event.payload.issue.id, teamId: event.payload.issue.team.id, title: event.payload.issue.title };
}

// dex:group group-id:linear group-label:"Linear"
// dex:explanation text:"Persist the signed new issue event and complete the Flow."
class RecordIssueEvent implements Step<RecordedIssueEvent> {
  readonly inputCodec = jsonCodec<RecordedIssueEvent>();
  getStepType(): string { return 'RecordIssueEvent'; }
  execute(context: Context, input: RecordedIssueEvent): StepDecision { eventAttribute.set(context, input); return gracefulComplete(input); }
}
