// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchPoliciesStep, type Connection, type SearchPoliciesInput, type SearchPoliciesOutput } from '@superdurable/connector-enlvon';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'enlvon';
const resultAttribute = new Attribute<SearchPoliciesOutput>('enlvon-result', jsonCodec<SearchPoliciesOutput>());

/** Query Enlvon through a typed connector Step. */
export class EnlvonInspectionFlow implements Flow<SearchPoliciesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Enlvon connection name differs');
    this.read = newSearchPoliciesStep<SearchPoliciesInput>({
      stepType: 'CallSearchPolicies',
      annotations: { groupId: 'provider', groupLabel: 'Enlvon', explanation: 'Read provider data through Enlvon.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteEnlvon'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'EnlvonInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<SearchPoliciesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<SearchPoliciesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteEnlvon'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<SearchPoliciesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
