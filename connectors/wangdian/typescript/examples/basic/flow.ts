// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListShopsStep, type Connection, type ListShopsInput, type ListShopsOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'wangdian';
const resultAttribute = new Attribute<ListShopsOutput>('wangdian-result', jsonCodec<ListShopsOutput>());

/** Query Wangdian through a typed connector Step. */
export class WangdianInspectionFlow implements Flow<ListShopsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Wangdian connection name differs');
    this.read = newListShopsStep<ListShopsInput>({
      stepType: 'CallListShops',
      annotations: { groupId: 'provider', groupLabel: 'Wangdian', explanation: 'Read provider data through Wangdian.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteWangdian'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'WangdianInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListShopsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListShopsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteWangdian'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListShopsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
