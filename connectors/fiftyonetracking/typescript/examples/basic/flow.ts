// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetShipmentStatusCountsStep, type Connection, type GetShipmentStatusCountsInput, type ShipmentStatusCounts } from '@superdurable/connector-fiftyonetracking';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fiftyonetracking';
const resultAttribute = new Attribute<ShipmentStatusCounts>('fiftyonetracking-result', jsonCodec<ShipmentStatusCounts>());

/** Query Fiftyonetracking through a typed connector Step. */
export class FiftyonetrackingInspectionFlow implements Flow<GetShipmentStatusCountsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Fiftyonetracking connection name differs');
    this.read = newGetShipmentStatusCountsStep<GetShipmentStatusCountsInput>({
      stepType: 'CallGetShipmentStatusCounts',
      annotations: { groupId: 'provider', groupLabel: 'Fiftyonetracking', explanation: 'Read provider data through Fiftyonetracking.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      counted: new StepRef('CompleteFiftyonetracking'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FiftyonetrackingInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetShipmentStatusCountsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ShipmentStatusCounts>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFiftyonetracking'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ShipmentStatusCounts>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
