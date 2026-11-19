// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListTicketStatusesStep, type Connection, type ListTicketStatusesInput, type ListTicketStatusesOutput } from '../../src/index.js';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'udesk';
const resultAttribute = new Attribute<ListTicketStatusesOutput>('udesk-result', jsonCodec<ListTicketStatusesOutput>());

/** Query Udesk through a typed connector Step. */
export class UdeskListTicketStatusesFlow implements Flow<ListTicketStatusesInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Udesk connection name differs');
    this.read = newListTicketStatusesStep<ListTicketStatusesInput>({
      stepType: 'CallListTicketStatuses',
      annotations: { groupId: 'provider', groupLabel: 'Udesk', explanation: 'Read provider data through Udesk.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteUdesk'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'UdeskListTicketStatusesTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<ListTicketStatusesInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<ListTicketStatusesOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteUdesk'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<ListTicketStatusesOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
