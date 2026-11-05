// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchTicketsStep, type Connection, type SearchTicketsInput, type SearchTicketsOutput } from '../../src/index.js';

/** Static Dex Web connection name shared by the Worker and tests. */
export const ConnectionName = 'zendesk-support';

/** Search one page of matching customer support tickets. */
export class CustomerIssueFlow implements Flow<SearchTicketsInput> {
  private readonly searched = new CustomerIssuesSearched();
  private readonly searchTickets;
  /** Bind the connector's named connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('customer issue Flow requires its static Zendesk connection');
    const searched = this.searched;
    this.searchTickets = newSearchTicketsStep<SearchTicketsInput>({
      stepType: 'SearchCustomerIssueTickets',
      annotations: { groupId: 'zendesk-support', groupLabel: 'Zendesk Support', explanation: 'Search Zendesk for customer issue tickets matching the typed filters.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      searched,
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'ZendeskCustomerIssue'; }
  /** Register the search and completion Steps. */ getSteps(): StepList<SearchTicketsInput> { return StepList.startStep<SearchTicketsInput>(this.searchTickets).otherSteps(this.searched); }
}

// dex:group group-id:zendesk-support group-label:"Zendesk Support"
// dex:explanation text:"Return the searched ticket page to the caller."
class CustomerIssuesSearched implements Step<QueryResult<SearchTicketsOutput>> {
  getStepType(): string { return 'CustomerIssuesSearched'; }
  execute(_context: Context, result: QueryResult<SearchTicketsOutput>): StepDecision { return gracefulComplete(result.value); }
}
