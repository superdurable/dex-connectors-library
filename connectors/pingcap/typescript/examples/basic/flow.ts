// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListProjectsStep, type Connection, type ListProjectsInput, type ListProjectsOutput } from '@superdurable/connector-pingcap';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'pingcap';
const resultAttribute = new Attribute<ListProjectsOutput>('pingcap-result', jsonCodec<ListProjectsOutput>());

/** Query Pingcap through a typed connector Step. */
export class PingcapInspectionFlow implements Flow<ListProjectsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Pingcap connection name differs');
    this.read = newListProjectsStep<ListProjectsInput>({
      stepType: 'CallListProjects',
      annotations: { groupId: 'provider', groupLabel: 'Pingcap', explanation: 'Read provider data through Pingcap.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompletePingcap'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'PingcapInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListProjectsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListProjectsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePingcap'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListProjectsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
