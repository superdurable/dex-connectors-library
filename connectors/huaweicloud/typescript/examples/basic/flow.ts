// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListAccessibleProjectsStep, type Connection, type ListAccessibleProjectsInput, type ListAccessibleProjectsOutput } from '@superdurable/connector-huaweicloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'huaweicloud';
const resultAttribute = new Attribute<ListAccessibleProjectsOutput>('huaweicloud-result', jsonCodec<ListAccessibleProjectsOutput>());

/** Query HuaweiCloud through a typed connector Step. */
export class HuaweiCloudInspectionFlow implements Flow<ListAccessibleProjectsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('HuaweiCloud connection name differs');
    this.read = newListAccessibleProjectsStep<ListAccessibleProjectsInput>({
      stepType: 'CallListAccessibleProjects',
      annotations: { groupId: 'provider', groupLabel: 'HuaweiCloud', explanation: 'Read provider data through HuaweiCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteHuaweiCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'HuaweiCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListAccessibleProjectsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListAccessibleProjectsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteHuaweiCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListAccessibleProjectsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
