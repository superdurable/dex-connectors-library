// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newSearchTicketsStep, type Connection, type SearchTicketsInput, type SearchTicketsOutput } from '../../../src/index.js';

/** Static Dex Web connection used by this Flow. */
export const ConnectionName = 'zoho-desk-helpdesk';

/** Search one Zoho Desk ticket page. */
export class SearchTicketsFlow implements Flow<SearchTicketsInput> {
  private readonly complete = new CompleteSearch();

  /** Bind the static provider connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Zoho Desk connection name must be zoho-desk-helpdesk');
  }
  /** Stable Flow identity. */ getFlowType(): string { return 'ZohoDeskTypeScriptSearchTickets'; }
  /** Register the query and success continuation. */
  getSteps(): StepList<SearchTicketsInput> {
    return StepList.startStep(newSearchTicketsStep<SearchTicketsInput>({
      stepType: 'SearchZohoDeskTickets',
      annotations: { groupId: 'zoho-desk-search', groupLabel: 'Zoho Desk', explanation: 'Search one filtered Zoho Desk ticket page.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      searched: this.complete,
    })).otherSteps(this.complete);
  }
  /** This Flow stores no Attributes or Streams. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete with the bounded search result. */
// dex:group group-id:zoho-desk-search group-label:"Zoho Desk"
// dex:explanation text:"Complete the Flow with the searched Zoho Desk ticket page."
export class CompleteSearch implements Step<QueryResult<SearchTicketsOutput>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteZohoDeskSearch'; }
  /** Return the connector output. */
  execute(_context: Context, result: QueryResult<SearchTicketsOutput>): StepDecision {
    return gracefulComplete(result.value);
  }
}
