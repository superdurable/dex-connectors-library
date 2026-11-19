// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListVehicleBrandsStep, type Connection, type ListVehicleBrandsInput, type ListVehicleBrandsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'wapi';
const resultAttribute = new Attribute<ListVehicleBrandsOutput>('wapi-result', jsonCodec<ListVehicleBrandsOutput>());

/** Query Wapi through a typed connector Step. */
export class WapiListVehicleBrandsFlow implements Flow<ListVehicleBrandsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Wapi connection name differs');
    this.read = newListVehicleBrandsStep<ListVehicleBrandsInput>({
      stepType: 'CallListVehicleBrands',
      annotations: { groupId: 'provider', groupLabel: 'Wapi', explanation: 'Read provider data through Wapi.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteWapi'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WapiListVehicleBrandsTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListVehicleBrandsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListVehicleBrandsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWapi'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListVehicleBrandsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
