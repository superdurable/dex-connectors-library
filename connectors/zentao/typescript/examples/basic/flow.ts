// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListProductsStep, type Connection, type ListProductsInput, type ProductList } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'zentao';
const resultAttribute = new Attribute<ProductList>('zentao-result', jsonCodec<ProductList>());

/** Query ZenTao through a typed connector Step. */
export class ZenTaoInspectionFlow implements Flow<ListProductsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ZenTao connection name differs');
    this.read = newListProductsStep<ListProductsInput>({
      stepType: 'CallListProducts',
      annotations: { groupId: 'provider', groupLabel: 'ZenTao', explanation: 'Read provider data through ZenTao.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteZenTao'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ZenTaoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListProductsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ProductList>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteZenTao'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ProductList>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
