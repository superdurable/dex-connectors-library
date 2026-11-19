// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListMemberProjectsStep, type Connection, type ListMemberProjectsInput, type ListMemberProjectsOutput } from '@superdurable/connector-jihugitlab';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'jihugitlab';
const resultAttribute = new Attribute<ListMemberProjectsOutput>('jihugitlab-result', jsonCodec<ListMemberProjectsOutput>());

/** Query JiHuGitLab through a typed connector Step. */
export class JiHuGitLabInspectionFlow implements Flow<ListMemberProjectsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('JiHuGitLab connection name differs');
    this.read = newListMemberProjectsStep<ListMemberProjectsInput>({
      stepType: 'CallListMemberProjects',
      annotations: { groupId: 'provider', groupLabel: 'JiHuGitLab', explanation: 'Read provider data through JiHuGitLab.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteJiHuGitLab'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'JiHuGitLabInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListMemberProjectsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListMemberProjectsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteJiHuGitLab'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListMemberProjectsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
