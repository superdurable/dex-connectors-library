// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { IssueStateClosed, IssueStateReasonCompleted, IssueStateReasonNotPlanned, newCreateIssueCommentStep, newCreateIssueStep, newListIssuesStep, newReplaceIssueLabelsStep, newSetIssueStateStep, type Connection, type CreateIssueCommentInput, type CreatedIssue, type CreatedIssueComment, type Issue, type IssuePage, type ReplaceIssueLabelsInput, type ReplacedIssueLabels, type UpdatedIssueState } from '@superdurable/connector-github';

/** Static GitHub connection selected in Dex Web; it must use the github-oauth-repository method. */
export const ConnectionName = 'github-issue-filing';
/** Open issues read per page. */
export const OpenIssuePageSize = 100;
/** Open issue pages checked for a matching title before creating. */
export const MaxOpenIssuePages = 3;

/**
 * The repository and the issue to file unless an open issue with its title already exists. After creating it, the
 * Flow adds comment when it is not blank, replaces the labels with relabelWith when it is not empty, and closes the
 * issue with closeWithStateReason, completed or not_planned, filing a problem that is already resolved.
 */
export interface IssueFilingInput { owner: string; repository: string; title: string; body?: string; labels?: string[]; comment?: string; relabelWith?: string[]; closeWithStateReason?: string }
/** Durable input of closing the created issue. */
export interface CloseIssueRequest { owner: string; repository: string; issueNumber: number; stateReason: string }
/** Durable input of one open issue page read. */
export interface OpenIssuePageRequest { owner: string; repository: string; labels?: string[]; page: number }
/** Durable record of one issue filing that the Flow returns. */
export interface IssueFilingReport {
  input: IssueFilingInput;
  status: 'checkingOpenIssues' | 'creatingIssue' | 'alreadyTracked' | 'created' | 'commentingOnIssue' | 'relabelingIssue' | 'closingIssue' | 'closed';
  openIssuePagesRead: number;
  /** Open issues beyond MaxOpenIssuePages were not checked. */ hasUncheckedOpenIssues: boolean;
  existingIssue?: Issue;
  createdIssue?: CreatedIssue;
  closedIssue?: UpdatedIssueState;
  issueComment?: CreatedIssueComment;
  relabeledIssue?: ReplacedIssueLabels;
}

const report = new Attribute<IssueFilingReport>('github-issue-filing-report', jsonCodec<IssueFilingReport>());

/** Create an issue unless an open issue with the same title and labels is already tracked. */
export class IssueFilingFlow implements Flow<IssueFilingInput> {
  private readonly start = new StartIssueFiling();
  private readonly record = new RecordOpenIssues();
  private readonly complete = new CompleteIssueFiling();
  private readonly recordClosed = new RecordClosedIssue();
  private readonly recordComment = new RecordIssueComment();
  private readonly recordLabels = new RecordIssueLabels();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('GitHub connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'GitHubIssueFilingFlow'; }
  /** Register the duplicate check, creation, optional closing, and completion Steps. */
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
      newCreateIssueCommentStep({
        stepType: 'CommentOnGitHubIssue',
        annotations: { groupId: 'issue-comment', groupLabel: 'Issue comment', explanation: 'Comment on the created issue once, reading back a retried send instead of posting a duplicate.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: (input: CreateIssueCommentInput) => input,
        created: this.recordComment,
      }),
      this.recordComment,
      newReplaceIssueLabelsStep({
        stepType: 'RelabelGitHubIssue',
        annotations: { groupId: 'issue-labels', groupLabel: 'Issue labels', explanation: "Replace the created issue's labels; a repeat sends the same full set again." },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: (input: ReplaceIssueLabelsInput) => input,
        replaced: this.recordLabels,
      }),
      this.recordLabels,
      newSetIssueStateStep({
        stepType: 'CloseGitHubIssue',
        annotations: { groupId: 'issue-closing', groupLabel: 'Issue closing', explanation: 'Close the created issue with the requested reason; a repeat sets the same state again.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: (request: CloseIssueRequest) => ({ owner: request.owner, repository: request.repository, issueNumber: request.issueNumber, state: IssueStateClosed, stateReason: request.stateReason }),
        updated: this.recordClosed,
      }),
      this.recordClosed,
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
    // Rejecting the reason before filing avoids creating an issue the close Step cannot close.
    if (!isClosingStateReason(input.closeWithStateReason)) return forceFail('closeWithStateReason must be completed, not_planned, or empty');
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
// dex:explanation text:"Record the created issue, then complete the filing or start its requested follow-up."
/** Complete the Flow with the created issue, or start the first requested follow-up. */
export class CompleteIssueFiling implements Step<MutationResult<CreatedIssue>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGitHubIssueFiling'; }
  /** Persist the created issue's identity. */
  execute(context: Context, result: MutationResult<CreatedIssue>): StepDecision {
    const created: IssueFilingReport = { ...report.get(context), createdIssue: result.value };
    const followUp = nextIssueFollowUp(created);
    const updated: IssueFilingReport = { ...created, status: followUp.status };
    report.set(context, updated);
    switch (followUp.kind) {
      case 'comment': return goTo(new StepRef('CommentOnGitHubIssue'), issueCommentInput(updated));
      case 'relabel': return goTo(new StepRef('RelabelGitHubIssue'), issueLabelsInput(updated));
      case 'close': return goTo(new StepRef('CloseGitHubIssue'), closeIssueRequest(updated));
      default: return gracefulComplete(updated);
    }
  }
}

// dex:group group-id:issue-comment group-label:"Issue comment"
// dex:explanation text:"Record the comment, then relabel or close the issue when requested, or complete the filing."
/** Record the created comment, then run the next requested follow-up. */
export class RecordIssueComment implements Step<MutationResult<CreatedIssueComment>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordGitHubIssueComment'; }
  /** Persist the comment's identity. */
  execute(context: Context, result: MutationResult<CreatedIssueComment>): StepDecision {
    const commented: IssueFilingReport = { ...report.get(context), issueComment: result.value };
    const followUp = nextIssueFollowUp(commented);
    const updated: IssueFilingReport = { ...commented, status: followUp.status };
    report.set(context, updated);
    switch (followUp.kind) {
      case 'relabel': return goTo(new StepRef('RelabelGitHubIssue'), issueLabelsInput(updated));
      case 'close': return goTo(new StepRef('CloseGitHubIssue'), closeIssueRequest(updated));
      default: return gracefulComplete(updated);
    }
  }
}

// dex:group group-id:issue-labels group-label:"Issue labels"
// dex:explanation text:"Record the replaced labels, then close the issue when requested or complete the filing."
/** Record the replaced labels, then close the issue when requested. */
export class RecordIssueLabels implements Step<MutationResult<ReplacedIssueLabels>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordGitHubIssueLabels'; }
  /** Persist the label set. */
  execute(context: Context, result: MutationResult<ReplacedIssueLabels>): StepDecision {
    const relabeled: IssueFilingReport = { ...report.get(context), relabeledIssue: result.value };
    const followUp = nextIssueFollowUp(relabeled);
    const updated: IssueFilingReport = { ...relabeled, status: followUp.status };
    report.set(context, updated);
    if (followUp.kind === 'close') return goTo(new StepRef('CloseGitHubIssue'), closeIssueRequest(updated));
    return gracefulComplete(updated);
  }
}

/** Pick the first follow-up still to run, in order: comment, relabel, then close. */
function nextIssueFollowUp(current: IssueFilingReport): { kind: 'comment' | 'relabel' | 'close' | 'none'; status: IssueFilingReport['status'] } {
  const input = current.input;
  if (input.comment?.trim() && !current.issueComment) return { kind: 'comment', status: 'commentingOnIssue' };
  if (input.relabelWith?.length && !current.relabeledIssue) return { kind: 'relabel', status: 'relabelingIssue' };
  if (input.closeWithStateReason) return { kind: 'close', status: 'closingIssue' };
  return { kind: 'none', status: 'created' };
}

function issueCommentInput(current: IssueFilingReport): CreateIssueCommentInput {
  return { owner: current.input.owner, repository: current.input.repository, issueNumber: current.createdIssue?.number ?? 0, body: current.input.comment ?? '' };
}

function issueLabelsInput(current: IssueFilingReport): ReplaceIssueLabelsInput {
  return { owner: current.input.owner, repository: current.input.repository, issueNumber: current.createdIssue?.number ?? 0, labels: current.input.relabelWith ?? [] };
}

function closeIssueRequest(current: IssueFilingReport): CloseIssueRequest {
  return { owner: current.input.owner, repository: current.input.repository, issueNumber: current.createdIssue?.number ?? 0, stateReason: current.input.closeWithStateReason ?? '' };
}

// dex:group group-id:issue-closing group-label:"Issue closing"
// dex:explanation text:"Record the closed issue and complete the filing."
/** Complete the Flow with the closed issue. */
export class RecordClosedIssue implements Step<MutationResult<UpdatedIssueState>> {
  /** Stable Step identity. */ getStepType(): string { return 'RecordClosedGitHubIssue'; }
  /** Persist the closed issue's state. */
  execute(context: Context, result: MutationResult<UpdatedIssueState>): StepDecision {
    const closed: IssueFilingReport = { ...report.get(context), closedIssue: result.value, status: 'closed' };
    report.set(context, closed);
    return gracefulComplete(closed);
  }
}

/** Report whether a close reason is empty, completed, or not_planned. */
export function isClosingStateReason(stateReason: string | undefined): boolean {
  return !stateReason || stateReason === IssueStateReasonCompleted || stateReason === IssueStateReasonNotPlanned;
}

/** Return the first issue whose title matches, ignoring case and surrounding whitespace. */
export function findIssueWithTitle(issues: readonly Issue[], title: string): Issue | undefined {
  return issues.find(issue => issue.title.trim().toLowerCase() === title.trim().toLowerCase());
}

function openIssuePageRequest(input: IssueFilingInput, page: number): OpenIssuePageRequest {
  return { owner: input.owner, repository: input.repository, labels: input.labels ?? [], page };
}
