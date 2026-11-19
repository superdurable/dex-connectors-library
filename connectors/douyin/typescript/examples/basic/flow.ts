// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetBehaviorSummaryStep, type Connection, type GetBehaviorSummaryInput, type GetBehaviorSummaryOutput } from '@superdurable/connector-douyin';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'douyin';
const resultAttribute = new Attribute<GetBehaviorSummaryOutput>('douyin-result', jsonCodec<GetBehaviorSummaryOutput>());

/** Query Douyin through a typed connector Step. */
export class DouyinInspectionFlow implements Flow<GetBehaviorSummaryInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Douyin connection name differs');
    this.read = newGetBehaviorSummaryStep<GetBehaviorSummaryInput>({
      stepType: 'CallGetBehaviorSummary',
      annotations: { groupId: 'provider', groupLabel: 'Douyin', explanation: 'Read provider data through Douyin.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteDouyin'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'DouyinInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetBehaviorSummaryInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetBehaviorSummaryOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteDouyin'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetBehaviorSummaryOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
