// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetBalanceStep, type Connection, type GetBalanceInput, type GetBalanceOutput } from '@superdurable/connector-cloopen';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'cloopen';
/** Persisted main account balance. */ export const balanceAttribute = new Attribute<GetBalanceOutput>('cloopen-balance', jsonCodec<GetBalanceOutput>());

/** Query the connected Cloopen main account balance. */
export class CloopenBalanceFlow implements Flow<GetBalanceInput> {
  private readonly complete = new CompleteBalance();
  private readonly read;
  /** Bind one static Cloopen connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Cloopen connection name differs');
    this.read = newGetBalanceStep<GetBalanceInput>({ stepType: 'CallGetBalance',
      annotations: { groupId: 'provider', groupLabel: 'Cloopen', explanation: 'Read the main account balance for the connected account.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      fetched: new StepRef('CompleteCloopenBalance'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'CloopenBalanceTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted balance. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [balanceAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the main account balance and complete the Flow."
class CompleteBalance implements Step<QueryResult<GetBalanceOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteCloopenBalance'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<GetBalanceOutput>): StepDecision { balanceAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
