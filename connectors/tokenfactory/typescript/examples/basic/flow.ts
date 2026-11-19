// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetBalanceStep, type Connection, type GetBalanceInput, type Balance } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tokenfactory';
const resultAttribute = new Attribute<Balance>('tokenfactory-result', jsonCodec<Balance>());

/** Query TokenFactory through a typed connector Step. */
export class TokenFactoryInspectionFlow implements Flow<GetBalanceInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TokenFactory connection name differs');
    this.read = newGetBalanceStep<GetBalanceInput>({
      stepType: 'CallGetBalance',
      annotations: { groupId: 'provider', groupLabel: 'TokenFactory', explanation: 'Read provider data through TokenFactory.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      retrieved: new StepRef('CompleteTokenFactory'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TokenFactoryInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<Balance>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTokenFactory'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<Balance>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
