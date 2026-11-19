// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListGatewayModelsStep, type Connection, type ListGatewayModelsInput, type ListGatewayModelsOutput } from '@superdurable/connector-opencsg';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'opencsg';
const resultAttribute = new Attribute<ListGatewayModelsOutput>('opencsg-result', jsonCodec<ListGatewayModelsOutput>());

/** Query OpenCSG through a typed connector Step. */
export class OpenCSGInspectionFlow implements Flow<ListGatewayModelsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('OpenCSG connection name differs');
    this.read = newListGatewayModelsStep<ListGatewayModelsInput>({
      stepType: 'CallListGatewayModels',
      annotations: { groupId: 'provider', groupLabel: 'OpenCSG', explanation: 'Read provider data through OpenCSG.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteOpenCSG'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OpenCSGInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListGatewayModelsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListGatewayModelsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOpenCSG'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListGatewayModelsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
