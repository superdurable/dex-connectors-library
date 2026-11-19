// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListWarehousesStep, type Connection, type ListWarehousesInput, type WarehousePage } from '@superdurable/connector-chukou1';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'chukou1';
/** Persisted authorized overseas warehouses. */ export const warehousesAttribute = new Attribute<WarehousePage>('chukou1-warehouses', jsonCodec<WarehousePage>());

/** List overseas warehouses for the connected Chukou1 account. */
export class Chukou1WarehouseListFlow implements Flow<ListWarehousesInput> {
  private readonly complete = new CompleteWarehouseList();
  private readonly list;
  /** Bind one static Chukou1 connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Chukou1 connection name differs');
    this.list = newListWarehousesStep<ListWarehousesInput>({ stepType: 'CallListWarehouses',
      annotations: { groupId: 'provider', groupLabel: 'Chukou1', explanation: 'Read authorized overseas warehouse codes and countries.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: new StepRef('CompleteChukou1WarehouseList'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'Chukou1WarehouseListTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<ListWarehousesInput> { return StepList.startStep(this.list).otherSteps(this.complete); }
  /** Register the persisted warehouse page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [warehousesAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist authorized warehouses and complete the Flow."
class CompleteWarehouseList implements Step<QueryResult<WarehousePage>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteChukou1WarehouseList'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<WarehousePage>): StepDecision { warehousesAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
