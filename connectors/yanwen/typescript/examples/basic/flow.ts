// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListWarehousesStep, type Connection, type ListWarehousesInput, type WarehouseList } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'yanwen';
const resultAttribute = new Attribute<WarehouseList>('yanwen-result', jsonCodec<WarehouseList>());

/** Query Yanwen through a typed connector Step. */
export class YanwenListWarehousesFlow implements Flow<ListWarehousesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Yanwen connection name differs');
    this.read = newListWarehousesStep<ListWarehousesInput>({
      stepType: 'CallListWarehouses',
      annotations: { groupId: 'provider', groupLabel: 'Yanwen', explanation: 'Read provider data through Yanwen.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteYanwen'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'YanwenListWarehousesTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListWarehousesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<WarehouseList>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteYanwen'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<WarehouseList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
