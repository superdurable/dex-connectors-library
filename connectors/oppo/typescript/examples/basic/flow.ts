// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetPushQuotaStep, type Connection, type GetPushQuotaInput, type GetPushQuotaOutput } from '@superdurable/connector-oppo';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'oppo';
const resultAttribute = new Attribute<GetPushQuotaOutput>('oppo-result', jsonCodec<GetPushQuotaOutput>());

/** Query Oppo through a typed connector Step. */
export class OppoInspectionFlow implements Flow<GetPushQuotaInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Oppo connection name differs');
    this.read = newGetPushQuotaStep<GetPushQuotaInput>({
      stepType: 'CallGetPushQuota',
      annotations: { groupId: 'provider', groupLabel: 'Oppo', explanation: 'Read provider data through Oppo.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteOppo'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OppoInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetPushQuotaInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetPushQuotaOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOppo'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetPushQuotaOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
