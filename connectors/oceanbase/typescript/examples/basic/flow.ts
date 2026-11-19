// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListInstancesStep, type Connection, type ListInstancesInput, type ListInstancesOutput } from '@superdurable/connector-oceanbase';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'oceanbase';
const resultAttribute = new Attribute<ListInstancesOutput>('oceanbase-result', jsonCodec<ListInstancesOutput>());

/** Query Oceanbase through a typed connector Step. */
export class OceanbaseInspectionFlow implements Flow<ListInstancesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Oceanbase connection name differs');
    this.read = newListInstancesStep<ListInstancesInput>({
      stepType: 'CallListInstances',
      annotations: { groupId: 'provider', groupLabel: 'Oceanbase', explanation: 'Read provider data through Oceanbase.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteOceanbase'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'OceanbaseInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListInstancesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListInstancesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteOceanbase'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListInstancesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
