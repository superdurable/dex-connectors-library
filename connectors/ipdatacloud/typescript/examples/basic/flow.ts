// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newLookupIPLocationStep, type Connection, type LookupIPLocationInput, type LookupIPLocationOutput } from '@superdurable/connector-ipdatacloud';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'ipdatacloud';
const resultAttribute = new Attribute<LookupIPLocationOutput>('ipdatacloud-result', jsonCodec<LookupIPLocationOutput>());

/** Query IPDataCloud through a typed connector Step. */
export class IPDataCloudInspectionFlow implements Flow<LookupIPLocationInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('IPDataCloud connection name differs');
    this.read = newLookupIPLocationStep<LookupIPLocationInput>({
      stepType: 'CallLookupIPLocation',
      annotations: { groupId: 'provider', groupLabel: 'IPDataCloud', explanation: 'Read provider data through IPDataCloud.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteIPDataCloud'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'IPDataCloudInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<LookupIPLocationInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<LookupIPLocationOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteIPDataCloud'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<LookupIPLocationOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
