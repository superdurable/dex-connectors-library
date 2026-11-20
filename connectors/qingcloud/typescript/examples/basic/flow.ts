// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newDescribeZonesStep, type Connection, type DescribeZonesInput, type ZonesPage } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qingcloud';
const resultAttribute = new Attribute<ZonesPage>('qingcloud-result', jsonCodec<ZonesPage>());

/** Query Qingcloud through a typed connector Step. */
export class QingcloudInspectionFlow implements Flow<DescribeZonesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qingcloud connection name differs');
    this.read = newDescribeZonesStep<DescribeZonesInput>({
      stepType: 'CallDescribeZones',
      annotations: { groupId: 'provider', groupLabel: 'Qingcloud', explanation: 'Read provider data through Qingcloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteQingcloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QingcloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<DescribeZonesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ZonesPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQingcloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ZonesPage>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
