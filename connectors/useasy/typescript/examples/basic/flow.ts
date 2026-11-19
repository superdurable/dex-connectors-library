// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAgentsStep, type Connection, type ListAgentsInput, type ListAgentsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'useasy';
const resultAttribute = new Attribute<ListAgentsOutput>('useasy-result', jsonCodec<ListAgentsOutput>());

/** Query Useasy through a typed connector Step. */
export class UseasyInspectionFlow implements Flow<ListAgentsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Useasy connection name differs');
    this.read = newListAgentsStep<ListAgentsInput>({
      stepType: 'CallListAgents',
      annotations: { groupId: 'provider', groupLabel: 'Useasy', explanation: 'Read provider data through Useasy.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteUseasy'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'UseasyInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListAgentsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListAgentsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteUseasy'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListAgentsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
