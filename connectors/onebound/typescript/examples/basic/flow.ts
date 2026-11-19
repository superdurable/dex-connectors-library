// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchProductsStep, type Connection, type SearchProductsInput, type ProductSearchPage } from '@superdurable/connector-onebound';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'onebound';
const resultAttribute = new Attribute<ProductSearchPage>('onebound-result', jsonCodec<ProductSearchPage>());

/** Query Onebound through a typed connector Step. */
export class OneboundInspectionFlow implements Flow<SearchProductsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Onebound connection name differs');
    this.read = newSearchProductsStep<SearchProductsInput>({
      stepType: 'CallSearchProducts',
      annotations: { groupId: 'provider', groupLabel: 'Onebound', explanation: 'Read provider data through Onebound.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteOnebound'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OneboundInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SearchProductsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ProductSearchPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOnebound'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ProductSearchPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
