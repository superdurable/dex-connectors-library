// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetIssueStep, type Connection, type GetIssueInput, type Issue } from '@superdurable/connector-atlassian-jira';

/** Static Dex Web connection name used by this example. */
export const ConnectionName = 'jira-triage';
/** Nonsecret issue selection persisted for inspection. */
export const selectedIssue = new Attribute<GetIssueInput>('jira-selected-issue', jsonCodec<GetIssueInput>());
/** Safe issue view persisted for inspection. */
export const issueView = new Attribute<Issue>('jira-issue-view', jsonCodec<Issue>());

/** Read one Jira issue through the provider Step. */
export class GetJiraIssueFlow implements Flow<GetIssueInput> {
  private readonly prepare = new PrepareIssueRead();
  private readonly complete = new CompleteIssueRead();
  /** Bind one named Jira connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Jira connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'GetJiraIssueFlow'; }
  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<GetIssueInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newGetIssueStep({ stepType: 'ReadJiraIssue', annotations: { groupId: 'jira-issue', groupLabel: 'Jira Issue', explanation: 'Read one Jira Cloud issue and its standard fields.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, found: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [selectedIssue, issueView] }; }
}

/** Validate the issue selector before the provider Step. */
// dex:group group-id:jira-issue group-label:"Jira Issue"
// dex:explanation text:"Validate the issue key before reading it from Jira."
export class PrepareIssueRead implements Step<GetIssueInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareJiraIssueRead'; }
  /** Persist nonsecret input and route to the provider Step. */
  execute(context: Context, input: GetIssueInput): StepDecision {
    if (!input.issueIdOrKey?.trim()) return forceFail('A Jira issue key or ID is required');
    selectedIssue.set(context, input);
    return goTo(new StepRef('ReadJiraIssue'), input);
  }
}

/** Persist the issue view and complete the Flow. */
// dex:group group-id:jira-issue group-label:"Jira Issue"
// dex:explanation text:"Persist the Jira issue view and complete the Flow."
export class CompleteIssueRead implements Step<QueryResult<Issue>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteJiraIssueRead'; }
  /** Complete with provider fields selected by the application. */
  execute(context: Context, result: QueryResult<Issue>): StepDecision { issueView.set(context, result.value); return gracefulComplete(result.value); }
}
