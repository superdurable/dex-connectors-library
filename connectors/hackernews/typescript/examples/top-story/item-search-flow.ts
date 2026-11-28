// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchItemsStep, type Connection, type ItemSearchPage } from '@superdurable/connector-hackernews';

const ConnectionName = 'hackernews-public';

/** Search keywords and optional zero-based result page. */
export interface ItemSearchRequest { query: string; page?: number }

/** Read one page of public Hacker News search results. */
export class ItemSearchFlow implements Flow<ItemSearchRequest> {
  private readonly complete = new ReturnItemSearch();
  private readonly read;

  /** Bind the Worker's static public connection. */
  constructor(connection: Connection) {
    this.read = newSearchItemsStep<ItemSearchRequest>({
      stepType: 'ReadHackerNewsItemSearch',
      annotations: { groupId: 'search', groupLabel: 'Keyword search', explanation: 'Search all indexed public item types.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => ({ query: input.query, page: input.page ?? 0, hitsPerPage: 2 }),
      searched: new StepRef('ReturnHackerNewsItemSearch'),
    });
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'HackerNewsItemSearchTypeScript'; }
  /** Register the provider read and completion Steps. */ getSteps(): StepList<ItemSearchRequest> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
}

// dex:group group-id:search group-label:"Keyword search"
// dex:explanation text:"Return the indexed hits and next page number."
class ReturnItemSearch implements Step<QueryResult<ItemSearchPage>> {
  readonly inputCodec = jsonCodec<QueryResult<ItemSearchPage>>();
  getStepType(): string { return 'ReturnHackerNewsItemSearch'; }
  execute(_context: Context, input: QueryResult<ItemSearchPage>): StepDecision {
    return gracefulComplete(input.value);
  }
}
