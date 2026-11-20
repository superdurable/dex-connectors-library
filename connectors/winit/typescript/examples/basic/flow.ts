// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListWarehousesStep, type Connection, type ListWarehousesInput, type WarehousePage } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'winit';
const resultAttribute = new Attribute<WarehousePage>('winit-result', jsonCodec<WarehousePage>());

/** Query Winit through a typed connector Step. */
export class WinitListWarehousesFlow implements Flow<ListWarehousesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Winit connection name differs');
    this.read = newListWarehousesStep<ListWarehousesInput>({
      stepType: 'CallListWarehouses',
      annotations: { groupId: 'provider', groupLabel: 'Winit', explanation: 'Read provider data through Winit.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteWinit'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WinitListWarehousesTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListWarehousesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<WarehousePage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWinit'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<WarehousePage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
