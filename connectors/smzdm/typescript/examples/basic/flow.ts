// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchProductsStep, type Connection, type SearchProductsInput, type ProductPage } from '@superdurable/connector-smzdm';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'smzdm';
const resultAttribute = new Attribute<ProductPage>('smzdm-result', jsonCodec<ProductPage>());

/** Query SMZDM through a typed connector Step. */
export class SMZDMInspectionFlow implements Flow<SearchProductsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('SMZDM connection name differs');
    this.read = newSearchProductsStep<SearchProductsInput>({
      stepType: 'CallSearchProducts',
      annotations: { groupId: 'provider', groupLabel: 'SMZDM', explanation: 'Read provider data through SMZDM.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteSMZDM'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'SMZDMInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SearchProductsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ProductPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteSMZDM'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ProductPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
