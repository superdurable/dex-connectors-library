// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetWaybillBalanceStep, type Connection, type GetWaybillBalanceInput, type GetWaybillBalanceOutput } from '../../src/index.js';

/** Static Dex Web connection for the Yunda example. */
export const ConnectionName = 'yunda';
const balanceResult = new Attribute<GetWaybillBalanceOutput>('yunda-waybill-balance-result', jsonCodec<GetWaybillBalanceOutput>());

/** Read the waybill balance through Yunda. */
export class WaybillBalanceFlow implements Flow<GetWaybillBalanceInput> {
  private readonly start = new StartBalanceRead();
  private readonly complete = new CompleteBalanceRead();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Yunda connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'YundaWaybillBalance'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetWaybillBalanceInput> {
    return StepList.startStep(this.start).otherSteps(newGetWaybillBalanceStep({
      stepType: 'CallYundaAPI',
      annotations: { groupId: 'yunda', groupLabel: 'Yunda', explanation: 'Read the waybill balance with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteYundaBalance'),
    }), this.complete);
  }
  /** Register the durable waybill balance. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [balanceResult] }; }
}

// dex:group group-id:yunda group-label:"Yunda"
// dex:explanation text:"Start the waybill-balance query."
class StartBalanceRead implements Step<GetWaybillBalanceInput> {
  getStepType(): string { return 'StartYundaBalance'; }
  execute(_context: Context, input: GetWaybillBalanceInput): StepDecision { return goTo(new StepRef('CallYundaAPI'), input); }
}

// dex:group group-id:yunda group-label:"Yunda"
// dex:explanation text:"Persist the waybill balance and complete the Flow."
class CompleteBalanceRead implements Step<QueryResult<GetWaybillBalanceOutput>> {
  getStepType(): string { return 'CompleteYundaBalance'; }
  execute(context: Context, result: QueryResult<GetWaybillBalanceOutput>): StepDecision { balanceResult.set(context, result.value); return gracefulComplete(result.value); }
}
