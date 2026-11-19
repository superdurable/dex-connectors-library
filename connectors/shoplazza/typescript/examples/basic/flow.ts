// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetShopStep, type Connection, type GetShopInput, type GetShopOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'shoplazza';
const resultAttribute = new Attribute<GetShopOutput>('shoplazza-result', jsonCodec<GetShopOutput>());

/** Query Shoplazza through a typed connector Step. */
export class ShoplazzaInspectionFlow implements Flow<GetShopInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Shoplazza connection name differs');
    this.read = newGetShopStep<GetShopInput>({
      stepType: 'CallGetShop',
      annotations: { groupId: 'provider', groupLabel: 'Shoplazza', explanation: 'Read provider data through Shoplazza.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteShoplazza'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ShoplazzaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetShopInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetShopOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteShoplazza'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetShopOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
