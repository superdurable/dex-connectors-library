// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryRowsStep, type Connection, type QueryRowsInput, type RowSet } from '@superdurable/connector-microsoft-sql-server';

/** Static Dex Web connection name for the SQL Server example. */
export const ConnectionName = 'sql-server-refunds';
/** Safe read input retained for inspection. */
export const queryInput = new Attribute<QueryRowsInput>('sql-server-query-input', jsonCodec<QueryRowsInput>());
/** Query output retained for inspection. */
export const queryRows = new Attribute<RowSet>('sql-server-query-rows', jsonCodec<RowSet>());

/** Query one SQL Server result set through a Dex Step. */
export class QueryRowsFlow implements Flow<QueryRowsInput> {
  private readonly prepare = new PrepareQuery();
  private readonly complete = new CompleteQuery();
  /** Bind a named connection when registering the Worker. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow type. */ getFlowType(): string { return 'SQLServerQueryRowsFlow'; }
  /** Register the provider and application Steps. */
  getSteps(): StepList<QueryRowsInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newQueryRowsStep({ stepType: 'QuerySQLServerRows', annotations: { groupId: 'sql-server-query', groupLabel: 'SQL Server Query', explanation: 'Run one bounded parameterized SELECT.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, completed: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [queryInput, queryRows] }; }
}

// dex:group group-id:sql-server-query group-label:"SQL Server Query"
// dex:explanation text:"Keep the query input and start the provider Step."
/** Prepare the safe statement and parameters. */
export class PrepareQuery implements Step<QueryRowsInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareSQLServerQuery'; }
  /** Start the provider Step. */ execute(context: Context, input: QueryRowsInput): StepDecision {
    if (!input.statement) return forceFail('statement is required');
    queryInput.set(context, input);
    return goTo(new StepRef('QuerySQLServerRows'), input);
  }
}

// dex:group group-id:sql-server-query group-label:"SQL Server Query"
// dex:explanation text:"Persist the bounded rows and complete the Flow."
/** Complete the successful query. */
export class CompleteQuery implements Step<QueryResult<RowSet>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteSQLServerQuery'; }
  /** Retain returned rows. */ execute(context: Context, result: QueryResult<RowSet>): StepDecision {
    queryRows.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
