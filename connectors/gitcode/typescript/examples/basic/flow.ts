// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetRepositoryStep, type Connection, type GetRepositoryInput, type GetRepositoryOutput } from '@superdurable/connector-gitcode';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'gitcode';
const resultAttribute = new Attribute<GetRepositoryOutput>('gitcode-result', jsonCodec<GetRepositoryOutput>());

/** Query Gitcode through a typed connector Step. */
export class GitcodeInspectionFlow implements Flow<GetRepositoryInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Gitcode connection name differs');
    this.read = newGetRepositoryStep<GetRepositoryInput>({
      stepType: 'CallGetRepository',
      annotations: { groupId: 'provider', groupLabel: 'Gitcode', explanation: 'Read provider data through Gitcode.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteGitcode'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GitcodeInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetRepositoryInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetRepositoryOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGitcode'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetRepositoryOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
