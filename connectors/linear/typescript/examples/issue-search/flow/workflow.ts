// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchIssuesStep, type Connection, type SearchIssuesInput, type SearchIssuesOutput } from '@superdurable/connector-linear';

/** Static connection selected in Dex Web. */
export const ConnectionName = 'linear-issue-search';
/** Persisted issue page returned by the query. */
export const issuePage = new Attribute<SearchIssuesOutput>('linear-issue-page', jsonCodec<SearchIssuesOutput>());

/** Read one bounded page of Linear issues. */
export class IssueSearchFlow implements Flow<SearchIssuesInput> {
  private readonly prepare = new PrepareIssueSearch();
  private readonly complete = new CompleteIssueSearch();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'LinearIssueSearchFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<SearchIssuesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchIssuesStep({
        stepType: 'SearchLinearIssues',
        annotations: { groupId: 'linear-issue-search', groupLabel: 'Linear Issue Search', explanation: 'Read one bounded page of issues.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        searched: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [issuePage] }; }
}

/** Check the page bound before the provider call. */
// dex:group group-id:linear-issue-search group-label:"Linear Issue Search"
// dex:explanation text:"Validate the issue search page before invoking Linear."
export class PrepareIssueSearch implements Step<SearchIssuesInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareLinearIssueSearch'; }
  /** Continue only for a bounded page. */
  execute(_context: Context, input: SearchIssuesInput): StepDecision {
    if (input.pageSize !== undefined && (input.pageSize < 1 || input.pageSize > 100)) return forceFail('Linear issue page size is invalid');
    return goTo(new StepRef('SearchLinearIssues'), input);
  }
}

/** Complete the Flow with the issue page. */
// dex:group group-id:linear-issue-search group-label:"Linear Issue Search"
// dex:explanation text:"Persist the bounded Linear issue page and complete the Flow."
export class CompleteIssueSearch implements Step<QueryResult<SearchIssuesOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteLinearIssueSearch'; }
  /** Keep only safe provider metadata. */
  execute(context: Context, result: QueryResult<SearchIssuesOutput>): StepDecision {
    issuePage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
