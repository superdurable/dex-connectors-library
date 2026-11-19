// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetRepositoryStep, type Connection, type GetRepositoryInput, type GetRepositoryOutput } from '@superdurable/connector-gitee';

/** Static Dex Web connection name. */
export const ConnectionName = 'gitee';
/** Durable repository metadata without credentials. */
export const repositoryAttribute = new Attribute('gitee-repository', jsonCodec<GetRepositoryOutput>());

/** Look up one Gitee repository. */
export class GiteeRepositoryLookupFlow implements Flow<GetRepositoryInput> {
  private readonly complete = new RepositoryFound();
  private readonly lookup;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('repository Flow requires the gitee connection');
    this.lookup = newGetRepositoryStep<GetRepositoryInput>({
      stepType: 'GetGiteeRepository',
      annotations: { groupId: 'gitee', groupLabel: 'Gitee', explanation: 'Read one repository by owner and repository path.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      found: new StepRef('CompleteGiteeRepository'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'GiteeRepositoryLookup'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<GetRepositoryInput> { return StepList.startStep(this.lookup).otherSteps(this.complete); }
  /** Register the persisted metadata. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [repositoryAttribute] }; }
}

// dex:group group-id:gitee group-label:"Gitee"
// dex:explanation text:"Persist repository metadata and complete the Flow."
class RepositoryFound implements Step<QueryResult<GetRepositoryOutput>> {
  getStepType(): string { return 'CompleteGiteeRepository'; }
  execute(context: Context, result: QueryResult<GetRepositoryOutput>): StepDecision {
    repositoryAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
