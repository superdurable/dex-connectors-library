// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListUsersStep, type Connection, type ListUsersInput, type ListUsersOutput } from '@superdurable/connector-kingsoftcloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'kingsoftcloud';
const resultAttribute = new Attribute<ListUsersOutput>('kingsoftcloud-result', jsonCodec<ListUsersOutput>());

/** Query KingsoftCloud through a typed connector Step. */
export class KingsoftCloudInspectionFlow implements Flow<ListUsersInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('KingsoftCloud connection name differs');
    this.read = newListUsersStep<ListUsersInput>({
      stepType: 'CallListUsers',
      annotations: { groupId: 'provider', groupLabel: 'KingsoftCloud', explanation: 'Read provider data through KingsoftCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteKingsoftCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'KingsoftCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListUsersInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListUsersOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteKingsoftCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListUsersOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
