// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetFlightStatusStep, type Connection, type GetFlightStatusInput, type GetFlightStatusOutput } from '@superdurable/connector-variflight';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'variflight';
const resultAttribute = new Attribute<GetFlightStatusOutput>('variflight-result', jsonCodec<GetFlightStatusOutput>());

/** Query VariFlight through a typed connector Step. */
export class VariFlightInspectionFlow implements Flow<GetFlightStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('VariFlight connection name differs');
    this.read = newGetFlightStatusStep<GetFlightStatusInput>({
      stepType: 'CallGetFlightStatus',
      annotations: { groupId: 'provider', groupLabel: 'VariFlight', explanation: 'Read provider data through VariFlight.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteVariFlight'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'VariFlightInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetFlightStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetFlightStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteVariFlight'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetFlightStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
