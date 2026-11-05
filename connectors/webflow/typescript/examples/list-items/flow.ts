// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListCollectionItemsStep, type Connection, type ListCollectionItemsOutput } from '../../src/index.js';

/** Static project connection name used by the Worker and tests. */
export const ConnectionName = 'webflow';
/** Input for the runnable CMS list Flow. */
export interface ListItemsRequest { collectionId: string; limit?: number; offset?: number }

/** List a Webflow CMS page through a generated connector Step. */
export class ListItemsFlow implements Flow<ListItemsRequest> {
  private readonly completed = new ItemsListed();
  private readonly listItems;
  /** Bind a static Webflow connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('list items requires the webflow connection');
    this.listItems = newListCollectionItemsStep<ListItemsRequest>({
      stepType: 'ListWebflowItems',
      annotations: { groupId: 'webflow', groupLabel: 'Webflow', explanation: 'List one bounded page of staged CMS collection items.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: request => request,
      listed: this.completed,
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'WebflowListItems'; }
  /** Register the connector and completion Steps. */ getSteps(): StepList<ListItemsRequest> { return StepList.startStep<ListItemsRequest>(this.listItems).otherSteps(this.completed); }
}

// dex:group group-id:webflow group-label:"Webflow"
// dex:explanation text:"Complete the Flow with the listed staged CMS items."
class ItemsListed implements Step<QueryResult<ListCollectionItemsOutput>> {
  readonly inputCodec = jsonCodec<QueryResult<ListCollectionItemsOutput>>();
  getStepType(): string { return 'WebflowItemsListed'; }
  execute(_context: Context, result: QueryResult<ListCollectionItemsOutput>): StepDecision { return gracefulComplete(result.value); }
}
