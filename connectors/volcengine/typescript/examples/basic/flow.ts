// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDescribeRegionsStep, type Connection, type DescribeRegionsInput, type DescribeRegionsOutput } from '@superdurable/connector-volcengine';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'volcengine';
const resultAttribute = new Attribute<DescribeRegionsOutput>('volcengine-result', jsonCodec<DescribeRegionsOutput>());

/** Query Volcengine through a typed connector Step. */
export class VolcengineInspectionFlow implements Flow<DescribeRegionsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Volcengine connection name differs');
    this.read = newDescribeRegionsStep<DescribeRegionsInput>({
      stepType: 'CallDescribeRegions',
      annotations: { groupId: 'provider', groupLabel: 'Volcengine', explanation: 'Read provider data through Volcengine.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteVolcengine'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'VolcengineInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<DescribeRegionsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<DescribeRegionsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteVolcengine'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<DescribeRegionsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
