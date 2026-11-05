// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRepositoriesStep, type Connection, type ListRepositoriesInput, type RepositoryPage } from '@superdurable/connector-atlassian-bitbucket';

/** Static Dex Web connection name shared by Worker and starter. */
export const ConnectionName = 'bitbucket-resource-read';

/** Safe repository page retained by the Flow. */
export const repositoryPage = new Attribute<RepositoryPage>('bitbucket-repository-page', jsonCodec<RepositoryPage>());

/** One bounded Bitbucket repository listing through a provider Query Step. */
export class ResourceReadFlow implements Flow<ListRepositoriesInput> {
  private readonly complete = new CompleteResourceRead();
  private readonly listRepositories;

  /** Bind a named Bitbucket connection during Worker registration. */
  constructor(connection: Connection) {
    if (!connection || connection.reference.name !== ConnectionName) throw new TypeError('resource read requires its static Bitbucket connection');
    this.listRepositories = newListRepositoriesStep<ListRepositoriesInput>({
      stepType: 'ListBitbucketRepositories',
      annotations: { groupId: 'bitbucket-resource-read', groupLabel: 'Bitbucket Resources', explanation: 'List one repository page for the selected workspace.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: this.complete,
    });
  }

  /** Stable Dex Flow identity. */ getFlowType(): string { return 'BitbucketResourceRead'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<ListRepositoriesInput> { return StepList.startStep(this.listRepositories).otherSteps(this.complete); }
  /** Register the persisted provider page. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [repositoryPage] }; }
}

/** Persist the provider result and finish the Flow. */
// dex:group group-id:bitbucket-resource-read group-label:"Bitbucket Resources"
// dex:explanation text:"Retain the safe repository page and complete the read."
class CompleteResourceRead implements Step<QueryResult<RepositoryPage>> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'CompleteBitbucketResourceRead'; }
  /** Store the safe page. */
  execute(context: Context, result: QueryResult<RepositoryPage>): StepDecision {
    repositoryPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
