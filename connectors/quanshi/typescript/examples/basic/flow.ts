// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRoomsStep, type Connection, type ListRoomsInput, type ListRoomsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'quanshi';
const resultAttribute = new Attribute<ListRoomsOutput>('quanshi-result', jsonCodec<ListRoomsOutput>());

/** Query Quanshi through a typed connector Step. */
export class QuanshiInspectionFlow implements Flow<ListRoomsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Quanshi connection name differs');
    this.read = newListRoomsStep<ListRoomsInput>({
      stepType: 'CallListRooms',
      annotations: { groupId: 'provider', groupLabel: 'Quanshi', explanation: 'Read provider data through Quanshi.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteQuanshi'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QuanshiInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListRoomsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListRoomsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQuanshi'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListRoomsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
