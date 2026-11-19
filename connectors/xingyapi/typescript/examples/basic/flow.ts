// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetInstanceOnlineStatusStep, type Connection, type GetInstanceOnlineStatusInput, type GetInstanceOnlineStatusOutput } from '@superdurable/connector-xingyapi';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'xingyapi';
const resultAttribute = new Attribute<GetInstanceOnlineStatusOutput>('xingyapi-result', jsonCodec<GetInstanceOnlineStatusOutput>());

/** Query XingyAPI through a typed connector Step. */
export class XingyAPIInspectionFlow implements Flow<GetInstanceOnlineStatusInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('XingyAPI connection name differs');
    this.read = newGetInstanceOnlineStatusStep<GetInstanceOnlineStatusInput>({
      stepType: 'CallGetInstanceOnlineStatus',
      annotations: { groupId: 'provider', groupLabel: 'XingyAPI', explanation: 'Check managed Enterprise WeChat login status.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      checked: new StepRef('CompleteXingyAPI'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'XingyAPIInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetInstanceOnlineStatusInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetInstanceOnlineStatusOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteXingyAPI'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetInstanceOnlineStatusOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
