// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListIssuesStep, type Connection, type IssuePage, type ListIssuesInput } from '@superdurable/connector-sentry';

/** Static Dex Web connection name used by Worker and starter. */
export const ConnectionName = 'sentry-issue-review';
/** Safe search input retained for inspection. */
export const issueSearch = new Attribute<ListIssuesInput>('sentry-issue-search', jsonCodec<ListIssuesInput>());
/** Safe provider result retained for inspection. */
export const issuePage = new Attribute<IssuePage>('sentry-issue-page', jsonCodec<IssuePage>());

/** List one Sentry issue page through a Dex connector Step. */
export class IssueReviewFlow implements Flow<ListIssuesInput> {
  private readonly prepare = new PrepareIssueSearch();
  private readonly complete = new CompleteIssueReview();
  /** Bind a named Sentry connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Sentry connection is required'); }
  /** Stable Dex Flow type. */ getFlowType(): string { return 'SentryIssueReviewFlow'; }
  /** Register the preparation, provider, and completion Steps. */
  getSteps(): StepList<ListIssuesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListIssuesStep({
        stepType: 'ListSentryIssues',
        annotations: { groupId: 'sentry-issues', groupLabel: 'Sentry Issues', explanation: 'List one organization issue page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }), this.complete,
    );
  }
  /** Register every persisted Attribute. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [issueSearch, issuePage] }; }
}

/** Validate and retain one safe issue search. */
// dex:group group-id:sentry-issues group-label:"Sentry Issues"
// dex:explanation text:"Validate the organization before listing its issues."
export class PrepareIssueSearch implements Step<ListIssuesInput> {
  /** Stable Dex Step type. */ getStepType(): string { return 'PrepareSentryIssueSearch'; }
  /** Start the provider Step. */
  execute(context: Context, input: ListIssuesInput): StepDecision {
    if (!input.organization?.trim()) return forceFail('Sentry organization is required');
    issueSearch.set(context, input);
    return goTo(new StepRef('ListSentryIssues'), input);
  }
}

/** Persist the returned page and complete the Flow. */
// dex:group group-id:sentry-issues group-label:"Sentry Issues"
// dex:explanation text:"Persist one issue page and complete the review Flow."
export class CompleteIssueReview implements Step<QueryResult<IssuePage>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteSentryIssueReview'; }
  /** Retain only safe issue metadata. */
  execute(context: Context, result: QueryResult<IssuePage>): StepDecision {
    issuePage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
