// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAppsPageStep, type Connection, type ListAppsPageInput, type ListAppsPageOutput } from '@superdurable/connector-fanruan';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'fanruan';
const resultAttribute = new Attribute<ListAppsPageOutput>('fanruan-result', jsonCodec<ListAppsPageOutput>());

/** Query Fanruan through a typed connector Step. */
export class FanruanInspectionFlow implements Flow<ListAppsPageInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Fanruan connection name differs');
    this.read = newListAppsPageStep<ListAppsPageInput>({
      stepType: 'CallListAppsPage',
      annotations: { groupId: 'provider', groupLabel: 'Fanruan', explanation: 'Read provider data through Fanruan.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteFanruan'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'FanruanInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListAppsPageInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListAppsPageOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteFanruan'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListAppsPageOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
