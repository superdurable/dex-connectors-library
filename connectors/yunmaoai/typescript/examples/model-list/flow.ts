// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '../../src/index.js';

/** Static Dex Web connection for the YunMaoAI example. */
export const ConnectionName = 'yunmaoai';
const modelsResult = new Attribute<ListModelsOutput>('yunmaoai-model-list-result', jsonCodec<ListModelsOutput>());

/** Read the model list through YunMaoAI. */
export class ModelCatalogFlow implements Flow<ListModelsInput> {
  private readonly start = new StartModelList();
  private readonly complete = new CompleteModelList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('YunMaoAI connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'YunMaoAIModelCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListModelsInput> {
    return StepList.startStep(this.start).otherSteps(newListModelsStep({
      stepType: 'CallYunMaoAIAPI',
      annotations: { groupId: 'yunmaoai', groupLabel: 'YunMaoAI', explanation: 'Read the model catalog with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteYunMaoAIModels'),
    }), this.complete);
  }
  /** Register the durable model list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [modelsResult] }; }
}

// dex:group group-id:yunmaoai group-label:"YunMaoAI"
// dex:explanation text:"Start the model-list query."
class StartModelList implements Step<ListModelsInput> {
  getStepType(): string { return 'StartYunMaoAIModels'; }
  execute(_context: Context, input: ListModelsInput): StepDecision { return goTo(new StepRef('CallYunMaoAIAPI'), input); }
}

// dex:group group-id:yunmaoai group-label:"YunMaoAI"
// dex:explanation text:"Persist the model list and complete the Flow."
class CompleteModelList implements Step<QueryResult<ListModelsOutput>> {
  getStepType(): string { return 'CompleteYunMaoAIModels'; }
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision { modelsResult.set(context, result.value); return gracefulComplete(result.value); }
}
