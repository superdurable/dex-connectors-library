// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetDeviceDetailsStep, type Connection, type GetDeviceDetailsInput, type GetDeviceDetailsOutput } from '@superdurable/connector-tuya';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'tuya';
const resultAttribute = new Attribute<GetDeviceDetailsOutput>('tuya-result', jsonCodec<GetDeviceDetailsOutput>());

/** Query Tuya through a typed connector Step. */
export class TuyaInspectionFlow implements Flow<GetDeviceDetailsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Tuya connection name differs');
    this.read = newGetDeviceDetailsStep<GetDeviceDetailsInput>({
      stepType: 'CallGetDeviceDetails',
      annotations: { groupId: 'provider', groupLabel: 'Tuya', explanation: 'Read provider data through Tuya.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteTuya'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'TuyaInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetDeviceDetailsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetDeviceDetailsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteTuya'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetDeviceDetailsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
