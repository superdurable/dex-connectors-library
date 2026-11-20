// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newCheckUserOnlineStep, type Connection, type CheckUserOnlineInput, type CheckUserOnlineOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'rongcloud';
const resultAttribute = new Attribute<CheckUserOnlineOutput>('rongcloud-result', jsonCodec<CheckUserOnlineOutput>());

/** Query RongCloud through a typed connector Step. */
export class RongCloudInspectionFlow implements Flow<CheckUserOnlineInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('RongCloud connection name differs');
    this.read = newCheckUserOnlineStep<CheckUserOnlineInput>({
      stepType: 'CallCheckUserOnline',
      annotations: { groupId: 'provider', groupLabel: 'RongCloud', explanation: 'Read provider data through RongCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      checked: new StepRef('CompleteRongCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'RongCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<CheckUserOnlineInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<CheckUserOnlineOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteRongCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<CheckUserOnlineOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
