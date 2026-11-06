// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchConversationsStep, type Connection, type SearchConversationsInput, type SearchConversationsOutput } from '@superdurable/connector-reamaze';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'reamaze-triage';
/** Safe search request retained for inspection. */
export const searchRequest = new Attribute<SearchConversationsInput>('reamaze-search-request', jsonCodec<SearchConversationsInput>());
/** Bounded provider result retained for inspection. */
export const searchPage = new Attribute<SearchConversationsOutput>('reamaze-search-page', jsonCodec<SearchConversationsOutput>());

/** Search one page of conversations for triage. */
export class SearchConversationsFlow implements Flow<SearchConversationsInput> {
  private readonly prepare = new PrepareSearch();
  private readonly complete = new CompleteSearch();
  /** Bind the named Re:amaze connection. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('Re:amaze connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'ReamazeSearchConversationsFlow'; }
  /** Register the provider Step and its happy-path continuation. */
  getSteps(): StepList<SearchConversationsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchConversationsStep({ stepType: 'SearchReamazeConversations', annotations: { groupId: 'reamaze-triage', groupLabel: 'Re:amaze Triage', explanation: 'Search one bounded page of conversations.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, searched: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [searchRequest, searchPage] }; }
}

/** Validate a nonsecret requester filter. */
// dex:group group-id:reamaze-triage group-label:"Re:amaze Triage"
// dex:explanation text:"Validate the requester filter before searching conversations."
export class PrepareSearch implements Step<SearchConversationsInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareReamazeSearch'; }
  /** Start the provider Step. */
  execute(context: Context, input: SearchConversationsInput): StepDecision {
    if (!input.requesterEmail?.includes('@')) return forceFail('requesterEmail is required');
    searchRequest.set(context, input);
    return goTo(new StepRef('SearchReamazeConversations'), input);
  }
}

/** Save the bounded result and complete. */
// dex:group group-id:reamaze-triage group-label:"Re:amaze Triage"
// dex:explanation text:"Persist the returned conversation page and complete the Flow."
export class CompleteSearch implements Step<QueryResult<SearchConversationsOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteReamazeSearch'; }
  /** Complete with the safe provider result. */ execute(context: Context, result: QueryResult<SearchConversationsOutput>): StepDecision { searchPage.set(context, result.value); return gracefulComplete(result.value); }
}
