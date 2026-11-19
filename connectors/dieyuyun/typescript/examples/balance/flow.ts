// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAccountBalanceStep, type Connection, type GetAccountBalanceInput, type GetAccountBalanceOutput } from '@superdurable/connector-dieyuyun';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'dieyuyun';
/** Persisted main account balance. */ export const balanceAttribute = new Attribute<GetAccountBalanceOutput>('dieyuyun-balance', jsonCodec<GetAccountBalanceOutput>());

/** Query the connected Dieyuyun main account balance. */
export class DieyuyunBalanceFlow implements Flow<GetAccountBalanceInput> {
  private readonly complete = new CompleteBalance();
  private readonly read;
  /** Bind one static Dieyuyun connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Dieyuyun connection name differs');
    this.read = newGetAccountBalanceStep<GetAccountBalanceInput>({ stepType: 'CallGetAccountBalance',
      annotations: { groupId: 'provider', groupLabel: 'Dieyuyun', explanation: 'Read the main account balance for the connected account.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      fetched: new StepRef('CompleteDieyuyunBalance'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'DieyuyunBalanceTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetAccountBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted balance. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [balanceAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the main account balance and complete the Flow."
class CompleteBalance implements Step<QueryResult<GetAccountBalanceOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteDieyuyunBalance'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<GetAccountBalanceOutput>): StepDecision { balanceAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
