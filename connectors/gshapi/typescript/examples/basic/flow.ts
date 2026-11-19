// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListModelsStep, type Connection, type ListModelsInput, type ListModelsOutput } from '@superdurable/connector-gshapi';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'gshapi';
const resultAttribute = new Attribute<ListModelsOutput>('gshapi-result', jsonCodec<ListModelsOutput>());

/** Query Gshapi through a typed connector Step. */
export class GshapiInspectionFlow implements Flow<ListModelsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Gshapi connection name differs');
    this.read = newListModelsStep<ListModelsInput>({
      stepType: 'CallListModels',
      annotations: { groupId: 'provider', groupLabel: 'Gshapi', explanation: 'Read provider data through Gshapi.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGshapi'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GshapiInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListModelsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGshapi'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListModelsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
