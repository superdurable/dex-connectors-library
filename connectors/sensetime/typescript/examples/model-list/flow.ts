// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '@superdurable/connector-sensetime';

/** Static Dex Web connection for the SenseTime example. */
export const ConnectionName = 'sensetime';
const modelsResult = new Attribute<ListModelsOutput>('sensetime-model-list-result', jsonCodec<ListModelsOutput>());

/** Read the model list through SenseTime. */
export class ModelCatalogFlow implements Flow<ListModelsInput> {
  private readonly read;
  private readonly complete = new CompleteModelList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SenseTime connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallSenseTime',
      annotations: { groupId: 'sensetime', groupLabel: 'SenseTime', explanation: 'Read the model catalog with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteSenseTimeModels'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'SenseTimeModelCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable model list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [modelsResult] }; }
}

// dex:group group-id:sensetime group-label:"SenseTime"
// dex:explanation text:"Persist the model list and complete the Flow."
class CompleteModelList implements Step<QueryResult<ListModelsOutput>> {
  getStepType(): string { return 'CompleteSenseTimeModels'; }
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision { modelsResult.set(context, result.value); return gracefulComplete(result.value); }
}
