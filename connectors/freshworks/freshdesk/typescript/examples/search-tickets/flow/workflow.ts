// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchTicketsStep, type Connection, type SearchTicketsInput, type SearchTicketsOutput } from '@superdurable/connector-freshdesk';

/** Static Dex Web connection used by the Worker and starter. */
export const ConnectionName = 'freshdesk-helpdesk';
/** Safe search input persisted for flow inspection. */
export const searchRequest = new Attribute<SearchTicketsInput>('freshdesk-search-request', jsonCodec<SearchTicketsInput>());
/** Bounded search page persisted for flow inspection. */
export const ticketPage = new Attribute<SearchTicketsOutput>('freshdesk-ticket-page', jsonCodec<SearchTicketsOutput>());

/** Search one Freshdesk ticket page through a connector Step. */
export class SearchTicketsFlow implements Flow<SearchTicketsInput> {
  private readonly prepare = new PrepareSearch();
  private readonly complete = new CompleteSearch();

  /** Bind one named provider connection. */ constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Freshdesk connection is required');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'FreshdeskSearchTicketsFlow'; }
  /** Register preparation, provider, and completion Steps. */
  getSteps(): StepList<SearchTicketsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newSearchTicketsStep({
        stepType: 'SearchFreshdeskTickets',
        annotations: { groupId: 'freshdesk-search', groupLabel: 'Freshdesk Tickets', explanation: 'Search one page of Freshdesk tickets.' },
        connection: this.connection, connectionName: ConnectionName,
        mapToOperationInput: input => input,
        searched: this.complete,
      }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [searchRequest, ticketPage] }; }
}

/** Save validated search input before the provider Step. */
// dex:group group-id:freshdesk-search group-label:"Freshdesk Tickets"
// dex:explanation text:"Check search criteria and keep the safe request for inspection."
export class PrepareSearch implements Step<SearchTicketsInput> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareFreshdeskSearch'; }
  /** Require at least one filter before routing. */
  execute(context: Context, input: SearchTicketsInput): StepDecision {
    if (!input.statuses?.length && !input.priorities?.length && !input.tags?.length && !input.updatedSinceDate) return forceFail('Freshdesk search needs a status, priority, tag, or date');
    searchRequest.set(context, input);
    return goTo(new StepRef('SearchFreshdeskTickets'), input);
  }
}

/** Persist the returned ticket page and finish the Flow. */
// dex:group group-id:freshdesk-search group-label:"Freshdesk Tickets"
// dex:explanation text:"Save the bounded ticket page and complete the Flow."
export class CompleteSearch implements Step<QueryResult<SearchTicketsOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteFreshdeskSearch'; }
  /** Complete with the provider result. */
  execute(context: Context, result: QueryResult<SearchTicketsOutput>): StepDecision {
    ticketPage.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
