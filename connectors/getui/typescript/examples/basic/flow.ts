// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetDailyPushReportStep, type Connection, type GetDailyPushReportInput, type GetDailyPushReportOutput } from '@superdurable/connector-getui';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'getui';
const resultAttribute = new Attribute<GetDailyPushReportOutput>('getui-result', jsonCodec<GetDailyPushReportOutput>());

/** Query Getui through a typed connector Step. */
export class GetuiInspectionFlow implements Flow<GetDailyPushReportInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Getui connection name differs');
    this.read = newGetDailyPushReportStep<GetDailyPushReportInput>({
      stepType: 'CallGetDailyPushReport',
      annotations: { groupId: 'provider', groupLabel: 'Getui', explanation: 'Read provider data through Getui.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteGetui'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GetuiInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetDailyPushReportInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetDailyPushReportOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGetui'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetDailyPushReportOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
