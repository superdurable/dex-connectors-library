// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListBucketsStep, type Connection, type ListBucketsInput, type ListBucketsOutput } from '@superdurable/connector-qiniu';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'qiniu';
const resultAttribute = new Attribute<ListBucketsOutput>('qiniu-result', jsonCodec<ListBucketsOutput>());

/** Query Qiniu through a typed connector Step. */
export class QiniuInspectionFlow implements Flow<ListBucketsInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Qiniu connection name differs');
    this.read = newListBucketsStep<ListBucketsInput>({
      stepType: 'CallListBuckets',
      annotations: { groupId: 'provider', groupLabel: 'Qiniu', explanation: 'Read provider data through Qiniu.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteQiniu'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'QiniuInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListBucketsInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListBucketsOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteQiniu'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListBucketsOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
