// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '../../src/index.js';

/** Static Dex Web connection for the ZhuanqiCloud example. */
export const ConnectionName = 'zhuanqicloud';
const modelsResult = new Attribute<ListModelsOutput>('zhuanqicloud-model-list-result', jsonCodec<ListModelsOutput>());

/** Read the model list through ZhuanqiCloud. */
export class ModelCatalogFlow implements Flow<ListModelsInput> {
  private readonly start = new StartModelList();
  private readonly complete = new CompleteModelList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('ZhuanqiCloud connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ZhuanqiCloudModelCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListModelsInput> {
    return StepList.startStep(this.start).otherSteps(newListModelsStep({
      stepType: 'CallZhuanqiCloudAPI',
      annotations: { groupId: 'zhuanqicloud', groupLabel: 'ZhuanqiCloud', explanation: 'Read the model catalog with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteZhuanqiCloudModels'),
    }), this.complete);
  }
  /** Register the durable model list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [modelsResult] }; }
}

// dex:group group-id:zhuanqicloud group-label:"ZhuanqiCloud"
// dex:explanation text:"Start the model-list query."
class StartModelList implements Step<ListModelsInput> {
  getStepType(): string { return 'StartZhuanqiCloudModels'; }
  execute(_context: Context, input: ListModelsInput): StepDecision { return goTo(new StepRef('CallZhuanqiCloudAPI'), input); }
}

// dex:group group-id:zhuanqicloud group-label:"ZhuanqiCloud"
// dex:explanation text:"Persist the model list and complete the Flow."
class CompleteModelList implements Step<QueryResult<ListModelsOutput>> {
  getStepType(): string { return 'CompleteZhuanqiCloudModels'; }
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision { modelsResult.set(context, result.value); return gracefulComplete(result.value); }
}
