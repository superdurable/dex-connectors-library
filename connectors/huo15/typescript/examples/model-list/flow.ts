// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '../../src/index.js';

/** Static Dex Web connection for the Huo15 example. */
export const ConnectionName = 'huo15';
const modelsResult = new Attribute<ListModelsOutput>('huo15-model-list-result', jsonCodec<ListModelsOutput>());

/** Read the model list through Huo15. */
export class ModelCatalogFlow implements Flow<ListModelsInput> {
  private readonly read;
  private readonly complete = new CompleteModelList();
  /** Bind the static project connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Huo15 connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallHuo15',
      annotations: { groupId: 'huo15', groupLabel: 'Huo15', explanation: 'Read the model catalog with an API Key.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteHuo15Models'),
    });
  }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'Huo15ModelCatalog'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the durable model list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [modelsResult] }; }
}

// dex:group group-id:huo15 group-label:"Huo15"
// dex:explanation text:"Persist the model list and complete the Flow."
class CompleteModelList implements Step<QueryResult<ListModelsOutput>> {
  getStepType(): string { return 'CompleteHuo15Models'; }
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision { modelsResult.set(context, result.value); return gracefulComplete(result.value); }
}
