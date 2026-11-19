// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ModelPage } from '@superdurable/connector-moxin';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'moxin';
const resultAttribute = new Attribute<ModelPage>('moxin-result', jsonCodec<ModelPage>());

/** Query Moxin through a typed connector Step. */
export class MoxinInspectionFlow implements Flow<ListModelsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Moxin connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallListModels',
      annotations: { groupId: 'provider', groupLabel: 'Moxin', explanation: 'Read provider data through Moxin.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteMoxin'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'MoxinInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ModelPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMoxin'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ModelPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
