// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetTicketStep, type Connection, type GetTicketInput, type TicketDetails } from '@superdurable/connector-gorgias';

/** Static Dex Web connection name used by the Worker and starter. */
export const ConnectionName = 'gorgias-helpdesk';

/** Read one Gorgias ticket and its latest messages. */
export class ReadTicketFlow implements Flow<GetTicketInput> {
  private readonly complete = new CompleteTicketRead();

  /** Bind the named Gorgias connection at registration. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Gorgias connection is required');
  }

  /** Stable Dex Flow type. */ getFlowType(): string { return 'GorgiasReadTicketFlow'; }
  /** Register the provider Step and completion Step. */
  getSteps(): StepList<GetTicketInput> {
    return StepList.startStep(newGetTicketStep<GetTicketInput>({
      stepType: 'ReadGorgiasTicket',
      annotations: { groupId: 'gorgias-ticket', groupLabel: 'Gorgias Ticket', explanation: 'Read one ticket and its latest messages.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      found: this.complete,
    })).otherSteps(this.complete);
  }
  /** This Flow persists only its input and final output. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete the Flow with the bounded ticket details. */
// dex:group group-id:gorgias-ticket group-label:"Gorgias Ticket"
// dex:explanation text:"Complete after the ticket and its latest messages are read."
export class CompleteTicketRead implements Step<QueryResult<TicketDetails>> {
  /** Stable Dex Step type. */ getStepType(): string { return 'CompleteGorgiasTicketRead'; }
  /** Return the connector's safe output. */
  execute(_context: Context, result: QueryResult<TicketDetails>): StepDecision { return gracefulComplete(result.value); }
}
