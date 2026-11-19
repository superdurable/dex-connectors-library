// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryDeviceDetailStep, type Connection, type QueryDeviceDetailInput, type DeviceDetail } from '@superdurable/connector-onenet';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'onenet';
const resultAttribute = new Attribute<DeviceDetail>('onenet-result', jsonCodec<DeviceDetail>());

/** Query Onenet through a typed connector Step. */
export class OnenetInspectionFlow implements Flow<QueryDeviceDetailInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Onenet connection name differs');
    this.read = newQueryDeviceDetailStep<QueryDeviceDetailInput>({
      stepType: 'CallQueryDeviceDetail',
      annotations: { groupId: 'provider', groupLabel: 'Onenet', explanation: 'Read provider data through Onenet.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteOnenet'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OnenetInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<QueryDeviceDetailInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<DeviceDetail>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOnenet'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<DeviceDetail>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
