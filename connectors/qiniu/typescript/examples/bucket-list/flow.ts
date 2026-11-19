// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListBucketsStep, type Connection, type ListBucketsInput, type ListBucketsOutput } from '@superdurable/connector-qiniu';

/** Static Dex Web connection name. */
export const ConnectionName = 'qiniu';
/** Durable bucket names without signing credentials. */
export const bucketListAttribute = new Attribute('qiniu-buckets', jsonCodec<ListBucketsOutput>());

/** List names of Qiniu Kodo buckets. */
export class QiniuBucketListFlow implements Flow<ListBucketsInput> {
  private readonly complete = new BucketsListed();
  private readonly list;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('bucket Flow requires the qiniu connection');
    this.list = newListBucketsStep<ListBucketsInput>({
      stepType: 'ListQiniuBuckets',
      annotations: { groupId: 'qiniu', groupLabel: 'Qiniu', explanation: 'Read the bucket names owned by the signing account.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      listed: new StepRef('CompleteQiniuBucketList'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'QiniuBucketList'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<ListBucketsInput> { return StepList.startStep(this.list).otherSteps(this.complete); }
  /** Register the persisted bucket list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [bucketListAttribute] }; }
}

// dex:group group-id:qiniu group-label:"Qiniu"
// dex:explanation text:"Persist bucket names and complete the Flow."
class BucketsListed implements Step<QueryResult<ListBucketsOutput>> {
  getStepType(): string { return 'CompleteQiniuBucketList'; }
  execute(context: Context, result: QueryResult<ListBucketsOutput>): StepDecision {
    bucketListAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
