// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '@superdurable/connector-siliconflow';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'siliconflow';
const resultAttribute = new Attribute<ListModelsOutput>('siliconflow-result', jsonCodec<ListModelsOutput>());

/** Query SiliconFlow through a typed connector Step. */
export class SiliconFlowInspectionFlow implements Flow<ListModelsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SiliconFlow connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallListModels',
      annotations: { groupId: 'provider', groupLabel: 'SiliconFlow', explanation: 'Read provider data through SiliconFlow.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteSiliconFlow'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SiliconFlowInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListModelsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSiliconFlow'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
