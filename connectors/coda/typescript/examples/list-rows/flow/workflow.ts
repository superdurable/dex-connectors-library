// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListRowsStep, type Connection, type ListRowsInput, type ListRowsOutput } from '@superdurable/connector-coda';

/** Static Dex Web connection name shared by the Worker and starter. */
export const ConnectionName = 'coda';

/** One bounded Coda row page Flow. */
export class ListRowsFlow implements Flow<ListRowsInput> {
  private readonly complete = new CompleteListRows();

  /** Bind the named Coda connection when registering the Worker. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('Coda connection is required');
  }

  /** Stable Dex Flow type. */
  getFlowType(): string { return 'CodaListRowsFlow'; }

  /** Connect the provider Step's successful branch to completion. */
  getSteps(): StepList<ListRowsInput> {
    return StepList.startStep(newListRowsStep<ListRowsInput>({
      stepType: 'ListCodaRows',
      annotations: { groupId: 'coda-rows', groupLabel: 'Coda Rows', explanation: 'List one page of Coda table rows.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: this.complete,
    })).otherSteps(this.complete);
  }

  /** This Flow persists no Attributes or Streams. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [] }; }
}

/** Complete the Flow with one provider result. */
// dex:group group-id:coda-rows group-label:"Coda Rows"
// dex:explanation text:"Complete the Flow with one bounded Coda row page."
export class CompleteListRows implements Step<QueryResult<ListRowsOutput>> {
  /** Stable Dex Step identity. */ getStepType(): string { return 'CompleteCodaListRows'; }
  /** Complete with the query output. */
  execute(_context: unknown, result: QueryResult<ListRowsOutput>): StepDecision {
    return gracefulComplete(result.value);
  }
}
