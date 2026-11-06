// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchStep, type Connection, type SearchInput, type SearchOutput } from '../../src/index.js';

/** Static Notion connection selected in Dex Web. */
export const ConnectionName = 'notion';
const requestAttribute = new Attribute<SearchInput>('notion-search-request', jsonCodec<SearchInput>());
const resultAttribute = new Attribute<SearchOutput>('notion-search-result', jsonCodec<SearchOutput>());

/** Search one page of Notion objects shared with the connection. */
export class SearchPagesFlow implements Flow<SearchInput> {
  private readonly prepare = new PrepareSearch();
  private readonly complete = new CompleteSearch();
  /** Bind the static connection. */ constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Notion connection name differs'); }
  /** Stable Flow type. */ getFlowType(): string { return 'NotionSearchPages'; }
  /** Register persistence for the request and result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [requestAttribute, resultAttribute] }; }
  /** Register the provider Step and happy-path completion. */
  getSteps(): StepList<SearchInput> {
    return StepList.startStep(this.prepare).otherSteps(newSearchStep({
      stepType: 'SearchNotionPages', annotations: { groupId: 'notion', groupLabel: 'Notion', explanation: 'Search one shared Notion title page.' },
      connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      searched: this.complete,
    }), this.complete);
  }
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Persist the title query before the provider call."
class PrepareSearch implements Step<SearchInput> {
  getStepType(): string { return 'PrepareNotionSearch'; }
  execute(context: Context, input: SearchInput): StepDecision { requestAttribute.set(context, input); return goTo(new StepRef('SearchNotionPages'), input); }
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Save the search page and complete the Flow."
class CompleteSearch implements Step<QueryResult<SearchOutput>> {
  getStepType(): string { return 'CompleteNotionSearch'; }
  execute(context: Context, result: QueryResult<SearchOutput>): StepDecision { resultAttribute.set(context, result.value); return gracefulComplete(result.value); }
}
