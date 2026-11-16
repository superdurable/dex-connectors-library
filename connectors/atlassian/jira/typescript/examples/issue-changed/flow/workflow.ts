// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult, TriggerBindingDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  IssueChangeCreated, IssueChangeUpdated,
  defineIssueChangedTriggerBinding, newGetIssueStep, type Connection, type GetIssueInput, type Issue, type IssueChangeKind, type IssueChangedEvent,
} from '@superdurable/connector-atlassian-jira';

/** Stable Flow identity each issue change starts. */
export const FlowType = 'JiraIssueChangeTypeScript';
/** Static Dex Web connection for Jira. */
export const ConnectionName = 'jira-issue-watcher';
/** The issueChanged binding that starts this Flow. */
export const IssueChangedTriggerBinding = 'project-issue-changes';

/** One issueChanged delivery and the stable Trigger event ID that also names the Flow. */
export interface IssueChangeInput { /** Trigger event ID. */ triggerEventId: string; /** The delivered change. */ change: IssueChangedEvent }
/** The Flow result: the delivered change kind and the issue read back from Jira. */
export interface IssueChangeOutcome { /** The changed issue's key at delivery. */ issueKey: string; /** The delivered change kind. */ changeKind: IssueChangeKind; /** The issue read back from Jira. */ current: Issue }

/** The delivered change. */
export const issueChangeAttribute = new Attribute<IssueChangeInput>('jira-issue-change', jsonCodec<IssueChangeInput>());
/** The issue read back from Jira. */
export const issueChangeOutcomeAttribute = new Attribute<IssueChangeOutcome>('jira-issue-change-outcome', jsonCodec<IssueChangeOutcome>());

/** Records one Jira issue change and reads the issue back. */
export class IssueChangeFlow implements Flow<IssueChangeInput> {
  private readonly record = new RecordIssueChange();
  private readonly complete = new CompleteIssueChange();
  /** Bind the Jira connection at registration time. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Jira connection is required'); }
  /** Stable Flow identity shown in Dex Web. */ getFlowType(): string { return FlowType; }
  /** Register the record, read, and completion Steps. */
  getSteps(): StepList<IssueChangeInput> {
    return StepList.startStep(this.record).otherSteps(
      newGetIssueStep<IssueChangeInput>({
        stepType: 'ReadChangedIssue', connection: this.connection, connectionName: ConnectionName,
        annotations: { groupId: 'change', groupLabel: 'Change', explanation: 'Read the changed issue back with its description, so the record holds its current state.' },
        mapToOperationInput: mapToGetIssueInput, found: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the change and outcome Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [issueChangeAttribute, issueChangeOutcomeAttribute] }; }
  /** Declare the issueChanged binding and its project picker for Dex Web. */
  getConnectorTriggerBindings(): TriggerBindingDefinition[] {
    return [defineIssueChangedTriggerBinding({ connectionName: ConnectionName, bindingName: IssueChangedTriggerBinding, configurationUI: { units: [{
      id: 'watchedProject', unitId: 'projectPicker', label: 'Watched project', required: false,
      description: "Choose the Jira project whose created and updated issues start this Flow; the list comes from this connection's Jira site and shows the projects the account can browse, and the unit saves the project ID, key, and name. Leave it unsaved to watch every project the account can browse. The first start records existing issues as a baseline and starts no Flow for them. Restart the Worker after saving; a changed project records a new baseline.",
      bindings: [
        { port: 'projectId', jsonPointer: '/projectId' },
        { port: 'projectKey', jsonPointer: '/projectKey' },
        { port: 'projectName', jsonPointer: '/projectName' },
      ],
    }] } })];
  }
}

/** Name one Flow per issue version; a redelivered change finds its existing Flow. */
export function resolveFlowId(event: TriggerEvent<IssueChangedEvent>): string { return `jira-issue-change-${event.id}`; }
/** Keep the Trigger event ID beside the delivered change. */
export function mapToFlowInput(event: TriggerEvent<IssueChangedEvent>): IssueChangeInput { return { triggerEventId: event.id, change: event.payload }; }
/** Admit every change; an application narrows it, for example to new issues. */
export function acceptEveryIssueChange(): boolean { return true; }
/** Read the changed issue by its stable ID, which survives a project move. */
export function mapToGetIssueInput(input: IssueChangeInput): GetIssueInput { return { issueIdOrKey: input.change.issue.id }; }

/** Return why a delivered change is unusable, or an empty string. */
export function validateIssueChange(input: IssueChangeInput): string {
  if (!input.triggerEventId?.trim()) return 'triggerEventId is required';
  if (!input.change?.cloudId?.trim() || !input.change.issue?.id?.trim()) return 'change.cloudId and change.issue.id are required';
  if (input.change.changeKind !== IssueChangeCreated && input.change.changeKind !== IssueChangeUpdated) return 'change.changeKind must be created or updated';
  return '';
}

/** Validate and record the delivered change. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered issue change, then read the issue back."
export class RecordIssueChange implements Step<IssueChangeInput> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordIssueChange'; }
  /** Fail an unusable change; otherwise record it and read the issue. */
  execute(context: Context, input: IssueChangeInput): StepDecision {
    const problem = validateIssueChange(input);
    if (problem) return forceFail(problem);
    issueChangeAttribute.set(context, input);
    return goTo(new StepRef('ReadChangedIssue'), input);
  }
}

/** Record the issue read back as the outcome. */
// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the issue read back from Jira as the outcome and complete the Flow."
export class CompleteIssueChange implements Step<QueryResult<Issue>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteIssueChange'; }
  /** Save the outcome and complete with it. */
  execute(context: Context, result: QueryResult<Issue>): StepDecision {
    const change = issueChangeAttribute.get(context);
    const outcome: IssueChangeOutcome = { issueKey: change.change.issue.key, changeKind: change.change.changeKind, current: result.value };
    issueChangeOutcomeAttribute.set(context, outcome);
    return gracefulComplete(outcome);
  }
}
