// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetServiceUsageStep, type Connection, type GetServiceUsageInput, type ServiceUsage } from '@superdurable/connector-upyun';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'upyun';
const resultAttribute = new Attribute<ServiceUsage>('upyun-result', jsonCodec<ServiceUsage>());

/** Query UPYUN through a typed connector Step. */
export class UPYUNInspectionFlow implements Flow<GetServiceUsageInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('UPYUN connection name differs');
    this.read = newGetServiceUsageStep<GetServiceUsageInput>({
      stepType: 'CallGetServiceUsage',
      annotations: { groupId: 'provider', groupLabel: 'UPYUN', explanation: 'Read provider data through UPYUN.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      read: new StepRef('CompleteUPYUN'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'UPYUNInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetServiceUsageInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ServiceUsage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteUPYUN'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ServiceUsage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
