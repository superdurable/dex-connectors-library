// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListClustersStep, type Connection, type ListClustersInput, type ListClustersOutput } from '../../src/index.js';

/** Static Dex Web connection for the Zilliz example. */
export const ConnectionName = 'zilliz';
const clustersResult = new Attribute<ListClustersOutput>('zilliz-cluster-list-result', jsonCodec<ListClustersOutput>());

/** Read the cluster list through Zilliz. */
export class ClusterListFlow implements Flow<ListClustersInput> {
  private readonly start = new StartClusterList();
  private readonly complete = new CompleteClusterList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Zilliz connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ZillizClusterList'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListClustersInput> {
    return StepList.startStep(this.start).otherSteps(newListClustersStep({
      stepType: 'CallZillizAPI',
      annotations: { groupId: 'zilliz', groupLabel: 'Zilliz', explanation: 'Read the cluster list with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteZillizClusters'),
    }), this.complete);
  }
  /** Register the durable cluster list. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [clustersResult] }; }
}

// dex:group group-id:zilliz group-label:"Zilliz"
// dex:explanation text:"Start the cluster-list query."
class StartClusterList implements Step<ListClustersInput> {
  getStepType(): string { return 'StartZillizClusters'; }
  execute(_context: Context, input: ListClustersInput): StepDecision { return goTo(new StepRef('CallZillizAPI'), input); }
}

// dex:group group-id:zilliz group-label:"Zilliz"
// dex:explanation text:"Persist the cluster list and complete the Flow."
class CompleteClusterList implements Step<QueryResult<ListClustersOutput>> {
  getStepType(): string { return 'CompleteZillizClusters'; }
  execute(context: Context, result: QueryResult<ListClustersOutput>): StepDecision { clustersResult.set(context, result.value); return gracefulComplete(result.value); }
}
