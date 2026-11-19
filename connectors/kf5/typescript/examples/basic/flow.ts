// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTicketStep, type Connection, type GetTicketInput, type GetTicketOutput } from '@superdurable/connector-kf5';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'kf5';
const resultAttribute = new Attribute<GetTicketOutput>('kf5-result', jsonCodec<GetTicketOutput>());

/** Query KF5 through a typed connector Step. */
export class KF5InspectionFlow implements Flow<GetTicketInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('KF5 connection name differs');
    this.read = newGetTicketStep<GetTicketInput>({
      stepType: 'CallGetTicket',
      annotations: { groupId: 'provider', groupLabel: 'KF5', explanation: 'Read provider data through KF5.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteKF5'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'KF5InspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetTicketInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetTicketOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteKF5'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetTicketOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
