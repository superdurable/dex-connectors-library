// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetChargeStatusStep, type Connection, type GetChargeStatusInput, type GetChargeStatusOutput } from '@superdurable/connector-pingpp';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'pingpp';
const resultAttribute = new Attribute<GetChargeStatusOutput>('pingpp-result', jsonCodec<GetChargeStatusOutput>());

/** Query PingPP through a typed connector Step. */
export class PingPPInspectionFlow implements Flow<GetChargeStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('PingPP connection name differs');
    this.read = newGetChargeStatusStep<GetChargeStatusInput>({
      stepType: 'CallGetChargeStatus',
      annotations: { groupId: 'provider', groupLabel: 'PingPP', explanation: 'Read provider data through PingPP.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompletePingPP'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'PingPPInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetChargeStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetChargeStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompletePingPP'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetChargeStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
