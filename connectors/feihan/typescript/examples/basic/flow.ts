// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryRechargeOrderStep, type Connection, type QueryRechargeOrderInput, type RechargeOrderResult } from '@superdurable/connector-feihan';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'feihan';
const resultAttribute = new Attribute<RechargeOrderResult>('feihan-result', jsonCodec<RechargeOrderResult>());

/** Query Feihan through a typed connector Step. */
export class FeihanInspectionFlow implements Flow<QueryRechargeOrderInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Feihan connection name differs');
    this.read = newQueryRechargeOrderStep<QueryRechargeOrderInput>({
      stepType: 'CallQueryRechargeOrder',
      annotations: { groupId: 'provider', groupLabel: 'Feihan', explanation: 'Read provider data through Feihan.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteFeihan'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FeihanInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<QueryRechargeOrderInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<RechargeOrderResult>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFeihan'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<RechargeOrderResult>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
