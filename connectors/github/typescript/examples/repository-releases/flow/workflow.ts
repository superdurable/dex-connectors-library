// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListReleasesStep, type Connection, type ListReleasesInput, type ReleasePage } from '@superdurable/connector-github';

/** Static GitHub connection selected in Dex Web. */
export const ConnectionName = 'github-repository-releases';
/** Persisted release page for this Flow. */
export const releasePage = new Attribute<ReleasePage>('github-release-page', jsonCodec<ReleasePage>());

/** Read one bounded page of GitHub releases. */
export class RepositoryReleasesFlow implements Flow<ListReleasesInput> {
  private readonly prepare = new PrepareRepositoryReleases();
  private readonly complete = new CompleteRepositoryReleases();
  /** Bind the static provider connection. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow identity. */ getFlowType(): string { return 'GitHubRepositoryReleasesFlow'; }
  /** Register the query and completion Steps. */
  getSteps(): StepList<ListReleasesInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newListReleasesStep({
        stepType: 'ListGitHubReleases',
        annotations: { groupId: 'github-releases', groupLabel: 'GitHub Releases', explanation: 'Read one bounded repository release page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        listed: this.complete,
      }),
      this.complete,
    );
  }
  /** Register persisted provider output. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [releasePage] }; }
}

// dex:group group-id:github-releases group-label:"GitHub Releases"
// dex:explanation text:"Check the repository selection before reading its releases."
/** Check the repository selection before provider access. */
export class PrepareRepositoryReleases implements Step<ListReleasesInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareGitHubRepositoryReleases'; }
  /** Forward nonsecret input to the provider query. */
  execute(_context: Context, input: ListReleasesInput): StepDecision {
    if (!input.owner || !input.repository) return forceFail('GitHub repository owner and name are required');
    return goTo(new StepRef('ListGitHubReleases'), input);
  }
}

// dex:group group-id:github-releases group-label:"GitHub Releases"
// dex:explanation text:"Persist the bounded release page and complete the Flow."
/** Complete the Flow with a bounded release page. */
export class CompleteRepositoryReleases implements Step<QueryResult<ReleasePage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteGitHubRepositoryReleases'; }
  /** Persist safe provider output. */
  execute(context: Context, result: QueryResult<ReleasePage>): StepDecision {
    releasePage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
