// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListStoresStep, type Connection, type ListStoresInput, type StoreList } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'lingxing';
const resultAttribute = new Attribute<StoreList>('lingxing-result', jsonCodec<StoreList>());

/** Query Lingxing through a typed connector Step. */
export class LingxingListStoresFlow implements Flow<ListStoresInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Lingxing connection name differs');
    this.read = newListStoresStep<ListStoresInput>({
      stepType: 'CallListStores',
      annotations: { groupId: 'provider', groupLabel: 'Lingxing', explanation: 'Read provider data through Lingxing.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteLingxing'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'LingxingListStoresTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListStoresInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<StoreList>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteLingxing'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<StoreList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
