// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '@superdurable/connector-stepfun';

/** Static Dex Web connection for the StepFun example. */
export const ConnectionName = 'stepfun';
const modelsResult = new Attribute<ListModelsOutput>('stepfun-model-list-result', jsonCodec<ListModelsOutput>());

/** Read the model list through StepFun. */
export class ModelCatalogFlow implements Flow<ListModelsInput> {
  private readonly read;
  private readonly complete = new CompleteModelList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('StepFun connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallStepFun',
      annotations: { groupId: 'stepfun', groupLabel: 'StepFun', explanation: 'Read the model catalog with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteStepFunModels'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'StepFunModelCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable model list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [modelsResult] }; }
}

// dex:group group-id:stepfun group-label:"StepFun"
// dex:explanation text:"Persist the model list and complete the Flow."
class CompleteModelList implements Step<QueryResult<ListModelsOutput>> {
  getStepType(): string { return 'CompleteStepFunModels'; }
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision { modelsResult.set(context, result.value); return gracefulComplete(result.value); }
}
