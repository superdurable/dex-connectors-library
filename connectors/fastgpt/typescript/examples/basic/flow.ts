// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetDatasetDetailsStep, type Connection, type GetDatasetDetailsInput, type GetDatasetDetailsOutput } from '@superdurable/connector-fastgpt';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fastgpt';
const resultAttribute = new Attribute<GetDatasetDetailsOutput>('fastgpt-result', jsonCodec<GetDatasetDetailsOutput>());

/** Query FastGPT through a typed connector Step. */
export class FastGPTInspectionFlow implements Flow<GetDatasetDetailsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('FastGPT connection name differs');
    this.read = newGetDatasetDetailsStep<GetDatasetDetailsInput>({
      stepType: 'CallGetDatasetDetails',
      annotations: { groupId: 'provider', groupLabel: 'FastGPT', explanation: 'Read provider data through FastGPT.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteFastGPT'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FastGPTInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetDatasetDetailsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetDatasetDetailsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFastGPT'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetDatasetDetailsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
