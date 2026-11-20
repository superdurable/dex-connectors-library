// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRootGoodsCategoriesStep, type Connection, type ListRootGoodsCategoriesInput, type ListRootGoodsCategoriesOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'xiaoetech';
const resultAttribute = new Attribute<ListRootGoodsCategoriesOutput>('xiaoetech-result', jsonCodec<ListRootGoodsCategoriesOutput>());

/** Query Xiaoetech through a typed connector Step. */
export class XiaoetechListRootGoodsCategoriesFlow implements Flow<ListRootGoodsCategoriesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Xiaoetech connection name differs');
    this.read = newListRootGoodsCategoriesStep<ListRootGoodsCategoriesInput>({
      stepType: 'CallListRootGoodsCategories',
      annotations: { groupId: 'provider', groupLabel: 'Xiaoetech', explanation: 'Read provider data through Xiaoetech.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteXiaoetech'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'XiaoetechListRootGoodsCategoriesTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListRootGoodsCategoriesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListRootGoodsCategoriesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteXiaoetech'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListRootGoodsCategoriesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
