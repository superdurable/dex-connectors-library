// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetUserProfileStep, type Connection, type GetUserProfileInput, type GetUserProfileOutput } from '@superdurable/connector-netease';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'netease';
const resultAttribute = new Attribute<GetUserProfileOutput>('netease-result', jsonCodec<GetUserProfileOutput>());

/** Query Netease through a typed connector Step. */
export class NeteaseInspectionFlow implements Flow<GetUserProfileInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Netease connection name differs');
    this.read = newGetUserProfileStep<GetUserProfileInput>({
      stepType: 'CallGetUserProfile',
      annotations: { groupId: 'provider', groupLabel: 'Netease', explanation: 'Read provider data through Netease.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteNetease'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'NeteaseInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetUserProfileInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetUserProfileOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteNetease'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetUserProfileOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
