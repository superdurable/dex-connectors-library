// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetStoreBasicStep, type Connection, type GetStoreBasicInput, type StoreBasic } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'weimob';
const resultAttribute = new Attribute<StoreBasic>('weimob-result', jsonCodec<StoreBasic>());

/** Query Weimob through a typed connector Step. */
export class WeimobGetStoreBasicFlow implements Flow<GetStoreBasicInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Weimob connection name differs');
    this.read = newGetStoreBasicStep<GetStoreBasicInput>({
      stepType: 'CallGetStoreBasic',
      annotations: { groupId: 'provider', groupLabel: 'Weimob', explanation: 'Read provider data through Weimob.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteWeimob'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WeimobGetStoreBasicTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetStoreBasicInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<StoreBasic>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWeimob'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<StoreBasic>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
