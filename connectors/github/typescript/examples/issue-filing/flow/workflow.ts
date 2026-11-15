// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newCreateIssueStep, newListIssuesStep, type Connection, type CreatedIssue, type Issue, type IssuePage } from '@superdurable/connector-github';

/** Static GitHub connection selected in Dex Web; it must use the github-oauth-repository method. */
export const ConnectionName = 'github-issue-filing';
/** Open issues read per page. */
export const OpenIssuePageSize = 100;
/** Open issue pages checked for a matching title before creating. */
export const MaxOpenIssuePages = 3;

/** The repository and the issue to file unless an open issue with its title already exists. */
export interface IssueFilingInput { owner: string; repository: string; title: string; body?: string; labels?: string[] }
/** Durable input of one open issue page read. */
export interface OpenIssuePageRequest { owner: string; repository: string; labels?: string[]; page: number }
/** Durable record of one issue filing that the Flow returns. */
export interface IssueFilingReport {
  input: IssueFilingInput;
  status: 'checkingOpenIssues' | 'creatingIssue' | 'alreadyTracked' | 'created';
  openIssuePagesRead: number;
  /** Open issues beyond MaxOpenIssuePages were not checked. */ hasUncheckedOpenIssues: boolean;
  existingIssue?: Issue;
  createdIssue?: CreatedIssue;
}

const report = new Attribute<IssueFilingReport>('github-issue-filing-report', jsonCodec<IssueFilingReport>());

/** Create an issue unless an open issue with the same title and labels is already tracked. */
export class IssueFilingFlow implements Flow<IssueFilingInput> {
  private readonly start = new StartIssueFiling();
  private readonly record = new RecordOpenIssues();
  private readonly complete = new CompleteIssueFiling();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('GitHub connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'GitHubIssueFilingFlow'; }
  /** Register the duplicate check, creation, and completion Steps. */
  getSteps(): StepList<IssueFilingInput> {
    return StepList.startStep(this.start).otherSteps(
      newListIssuesStep({
        stepType: 'ListOpenGitHubIssues',
        annotations: { groupId: 'duplicate-check', groupLabel: 'Duplicate check', explanation: 'List one page of open issues that carry the requested labels.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: (request: OpenIssuePageRequest) => ({ owner: request.owner, repository: request.repository, state: 'open', labels: request.labels ?? [], pageSize: OpenIssuePageSize, page: request.page }),
        listed: this.record,
      }),
      this.record,
      newCreateIssueStep({
        stepType: 'CreateGitHubIssue',
        annotations: { groupId: 'issue-creation', groupLabel: 'Issue creation', explanation: 'Create the issue once, reading back a retried send instead of filing a duplicate.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: (input: IssueFilingInput) => ({ owner: input.owner, repository: input.repository, title: input.title, body: input.body ?? '', labels: input.labels ?? [] }),
        created: this.complete,
      }),
      this.complete,
    );
  }
  /** Register the filing report. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [report] }; }
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the requested issue before checking the open issues."
/** Record the request and read the first open issue page. */
export class StartIssueFiling implements Step<IssueFilingInput> {
  /** Stable Step identity. */ getStepType(): string { return 'StartGitHubIssueFiling'; }
  /** Persist the request before any provider call. */
  execute(context: Context, input: IssueFilingInput): StepDecision {
    report.set(context, { input, status: 'checkingOpenIssues', openIssuePagesRead: 0, hasUncheckedOpenIssues: false });
    return goTo(new StepRef('ListOpenGitHubIssues'), openIssuePageRequest(input, 1));
  }
}

// dex:group group-id:duplicate-check group-label:"Duplicate check"
// dex:explanation text:"Complete when an open issue has the title; otherwise read the next page or create the issue."
/** Decide from one open issue page. */
export class RecordOpenIssues implements Step<QueryResult<IssuePage>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordOpenGitHubIssues'; }
  /** Complete with a matching issue, read the next page, or create the issue. */
  execute(context: Context, result: QueryResult<IssuePage>): StepDecision {
    const current = report.get(context);
    const updated: IssueFilingReport = { ...current, openIssuePagesRead: current.openIssuePagesRead + 1 };
    const existing = findIssueWithTitle(result.value.issues ?? [], current.input.title);
    if (existing) {
      const tracked: IssueFilingReport = { ...updated, existingIssue: existing, status: 'alreadyTracked' };
      report.set(context, tracked);
      return gracefulComplete(tracked);
    }
    if (result.value.nextPage !== 0 && updated.openIssuePagesRead < MaxOpenIssuePages) {
      report.set(context, updated);
      return goTo(new StepRef('ListOpenGitHubIssues'), openIssuePageRequest(current.input, result.value.nextPage));
    }
    report.set(context, { ...updated, hasUncheckedOpenIssues: result.value.nextPage !== 0, status: 'creatingIssue' });
    return goTo(new StepRef('CreateGitHubIssue'), current.input);
  }
}

// dex:group group-id:issue-creation group-label:"Issue creation"
// dex:explanation text:"Record the created issue and complete the filing."
/** Complete the Flow with the created issue. */
export class CompleteIssueFiling implements Step<MutationResult<CreatedIssue>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGitHubIssueFiling'; }
  /** Persist the created issue's identity. */
  execute(context: Context, result: MutationResult<CreatedIssue>): StepDecision {
    const created: IssueFilingReport = { ...report.get(context), createdIssue: result.value, status: 'created' };
    report.set(context, created);
    return gracefulComplete(created);
  }
}

/** Return the first issue whose title matches, ignoring case and surrounding whitespace. */
export function findIssueWithTitle(issues: readonly Issue[], title: string): Issue | undefined {
  return issues.find(issue => issue.title.trim().toLowerCase() === title.trim().toLowerCase());
}

function openIssuePageRequest(input: IssueFilingInput, page: number): OpenIssuePageRequest {
  return { owner: input.owner, repository: input.repository, labels: input.labels ?? [], page };
}
