// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTokenPlanRemainsStep, type Connection, type GetTokenPlanRemainsInput, type GetTokenPlanRemainsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'minimax';
const resultAttribute = new Attribute<GetTokenPlanRemainsOutput>('minimax-result', jsonCodec<GetTokenPlanRemainsOutput>());

/** Query MiniMax through a typed connector Step. */
export class MiniMaxInspectionFlow implements Flow<GetTokenPlanRemainsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('MiniMax connection name differs');
    this.read = newGetTokenPlanRemainsStep<GetTokenPlanRemainsInput>({
      stepType: 'CallGetTokenPlanRemains',
      annotations: { groupId: 'provider', groupLabel: 'MiniMax', explanation: 'Read provider data through MiniMax.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteMiniMax'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'MiniMaxInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetTokenPlanRemainsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetTokenPlanRemainsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteMiniMax'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetTokenPlanRemainsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
