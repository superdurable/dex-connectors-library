// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetBalanceStep, type Connection, type GetBalanceInput, type GetBalanceOutput } from '@superdurable/connector-chuanglan';

/** Static Dex Web connection used by the example. */ export const ConnectionName = 'chuanglan';
/** Persisted remaining domestic SMS count. */ export const balanceAttribute = new Attribute<GetBalanceOutput>('chuanglan-balance', jsonCodec<GetBalanceOutput>());

/** Query the connected Chuanglan domestic SMS account balance. */
export class ChuanglanBalanceFlow implements Flow<GetBalanceInput> {
  private readonly complete = new CompleteBalance();
  private readonly read;
  /** Bind one static Chuanglan connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Chuanglan connection name differs');
    this.read = newGetBalanceStep<GetBalanceInput>({ stepType: 'CallGetBalance',
      annotations: { groupId: 'provider', groupLabel: 'Chuanglan 253', explanation: 'Read the remaining domestic SMS count for the connected account.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      fetched: new StepRef('CompleteChuanglanBalance'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ChuanglanBalanceTypeScript'; }
  /** Register the provider and completion Steps. */ getSteps(): StepList<GetBalanceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Register the persisted balance. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [balanceAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the remaining SMS count and complete the Flow."
class CompleteBalance implements Step<QueryResult<GetBalanceOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteChuanglanBalance'; }
  /** Persist the provider result without credentials. */
  execute(context: Context, result: QueryResult<GetBalanceOutput>): StepDecision { balanceAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
