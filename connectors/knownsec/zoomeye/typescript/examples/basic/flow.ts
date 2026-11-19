// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetAccountQuotaStep, type Connection, type GetAccountQuotaInput, type GetAccountQuotaOutput } from '@superdurable/connector-zoomeye';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'zoomeye';
const resultAttribute = new Attribute<GetAccountQuotaOutput>('zoomeye-result', jsonCodec<GetAccountQuotaOutput>());

/** Query ZoomEye through a typed connector Step. */
export class ZoomEyeInspectionFlow implements Flow<GetAccountQuotaInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ZoomEye connection name differs');
    this.read = newGetAccountQuotaStep<GetAccountQuotaInput>({
      stepType: 'CallGetAccountQuota',
      annotations: { groupId: 'provider', groupLabel: 'ZoomEye', explanation: 'Read provider data through ZoomEye.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteZoomEye'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ZoomEyeInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetAccountQuotaInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetAccountQuotaOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteZoomEye'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetAccountQuotaOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
