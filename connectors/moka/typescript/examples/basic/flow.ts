// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListJobFunctionsStep, type Connection, type ListJobFunctionsInput, type ListJobFunctionsOutput } from '@superdurable/connector-moka';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'moka';
const resultAttribute = new Attribute<ListJobFunctionsOutput>('moka-result', jsonCodec<ListJobFunctionsOutput>());

/** Query Moka through a typed connector Step. */
export class MokaInspectionFlow implements Flow<ListJobFunctionsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Moka connection name differs');
    this.read = newListJobFunctionsStep<ListJobFunctionsInput>({
      stepType: 'CallListJobFunctions',
      annotations: { groupId: 'provider', groupLabel: 'Moka', explanation: 'Read provider data through Moka.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteMoka'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'MokaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListJobFunctionsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListJobFunctionsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMoka'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListJobFunctionsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
