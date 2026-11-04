// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchConversationsStep, type Connection, type SearchConversationsInput, type SearchConversationsOutput } from '@superdurable/connector-helpscout';

/** Static Dex Web connection name for the example Worker and starter. */
export const ConnectionName = 'helpscout-support-inbox';

/** Nonsecret search filters retained for Flow inspection. */
export const searchRequest = new Attribute<SearchConversationsInput>('helpscout-search-request', jsonCodec<SearchConversationsInput>());
/** Safe provider page retained for Flow inspection. */
export const conversationPage = new Attribute<SearchConversationsOutput>('helpscout-conversation-page', jsonCodec<SearchConversationsOutput>());

/** One bounded conversation search Flow. */
export class SearchConversationsFlow implements Flow<SearchConversationsInput> {
  private readonly prepare = new PrepareSearch();
  private readonly complete = new CompleteSearch();

  /** Bind the named Help Scout connection at Worker registration. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Help Scout connection is required'); }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'HelpScoutSearchConversationsFlow'; }
  /** Register preparation, provider call, and completion. */
  getSteps(): StepList<SearchConversationsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchConversationsStep({
        stepType: 'SearchHelpScoutConversations',
        annotations: { groupId: 'helpscout-search', groupLabel: 'Help Scout Search', explanation: 'Search one bounded Help Scout conversation page.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        searched: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [searchRequest, conversationPage] }; }
}

/** Retain validated, nonsecret search filters. */
// dex:group group-id:helpscout-search group-label:"Help Scout Search"
// dex:explanation text:"Validate the page selection before searching conversations."
export class PrepareSearch implements Step<SearchConversationsInput> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'PrepareHelpScoutSearch'; }
  /** Route one search request to the provider Step. */
  execute(context: Context, input: SearchConversationsInput): StepDecision {
    if (input.page !== undefined && (!Number.isInteger(input.page) || input.page < 0 || input.page > 10_000)) return forceFail('Help Scout page must be from 1 to 10000');
    searchRequest.set(context, input);
    return goTo(new StepRef('SearchHelpScoutConversations'), input);
  }
}

/** Persist the safe search page and finish the Flow. */
// dex:group group-id:helpscout-search group-label:"Help Scout Search"
// dex:explanation text:"Store the returned conversation page and complete the Flow."
export class CompleteSearch implements Step<QueryResult<SearchConversationsOutput>> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'CompleteHelpScoutSearch'; }
  /** Complete with the provider's safe page. */
  execute(context: Context, result: QueryResult<SearchConversationsOutput>): StepDecision {
    conversationPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
