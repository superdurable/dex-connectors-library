// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListProvincesStep, type Connection, type ListProvincesInput, type ListProvincesOutput } from '@superdurable/connector-jisuapi';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'jisuapi';
const resultAttribute = new Attribute<ListProvincesOutput>('jisuapi-result', jsonCodec<ListProvincesOutput>());

/** Query JisuAPI through a typed connector Step. */
export class JisuAPIInspectionFlow implements Flow<ListProvincesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('JisuAPI connection name differs');
    this.read = newListProvincesStep<ListProvincesInput>({
      stepType: 'CallListProvinces',
      annotations: { groupId: 'provider', groupLabel: 'JisuAPI', explanation: 'Read provider data through JisuAPI.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteJisuAPI'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'JisuAPIInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListProvincesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListProvincesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteJisuAPI'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListProvincesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
