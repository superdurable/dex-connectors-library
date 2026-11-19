// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListIndustriesStep, type Connection, type ListIndustriesInput, type ListIndustriesOutput } from '@superdurable/connector-shixiseng';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'shixiseng';
const resultAttribute = new Attribute<ListIndustriesOutput>('shixiseng-result', jsonCodec<ListIndustriesOutput>());

/** Query Shixiseng through a typed connector Step. */
export class ShixisengInspectionFlow implements Flow<ListIndustriesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Shixiseng connection name differs');
    this.read = newListIndustriesStep<ListIndustriesInput>({
      stepType: 'CallListIndustries',
      annotations: { groupId: 'provider', groupLabel: 'Shixiseng', explanation: 'Read provider data through Shixiseng.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: new StepRef('CompleteShixiseng'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'ShixisengInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListIndustriesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListIndustriesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteShixiseng'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListIndustriesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
