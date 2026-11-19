// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryWaybillTraceStep, type Connection, type QueryWaybillTraceInput, type WaybillTrace } from '@superdurable/connector-jdlogistics';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'jdlogistics';
const resultAttribute = new Attribute<WaybillTrace>('jdlogistics-result', jsonCodec<WaybillTrace>());

/** Query JDLogistics through a typed connector Step. */
export class JDLogisticsInspectionFlow implements Flow<QueryWaybillTraceInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('JDLogistics connection name differs');
    this.read = newQueryWaybillTraceStep<QueryWaybillTraceInput>({
      stepType: 'CallQueryWaybillTrace',
      annotations: { groupId: 'provider', groupLabel: 'JDLogistics', explanation: 'Read provider data through JDLogistics.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteJDLogistics'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'JDLogisticsInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<QueryWaybillTraceInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<WaybillTrace>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteJDLogistics'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<WaybillTrace>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
