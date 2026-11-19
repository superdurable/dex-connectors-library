// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDatabasesStep, type Connection, type ListDatabasesInput, type ListDatabasesOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'taosdata';
const resultAttribute = new Attribute<ListDatabasesOutput>('taosdata-result', jsonCodec<ListDatabasesOutput>());

/** Query TDengine Cloud through a typed connector Step. */
export class TaosdataInspectionFlow implements Flow<ListDatabasesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('TDengine Cloud connection name differs');
    this.read = newListDatabasesStep<ListDatabasesInput>({
      stepType: 'CallListDatabases',
      annotations: { groupId: 'provider', groupLabel: 'TDengine Cloud', explanation: 'Read provider data through TDengine Cloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteTDengine Cloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TDengine CloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListDatabasesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListDatabasesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTDengine Cloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListDatabasesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
