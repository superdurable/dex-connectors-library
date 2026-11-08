// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  Attribute, StepList, StepRef, gracefulComplete, jsonCodec,
  type Context, type Flow, type PersistenceSchema, type Step, type StepDecision,
} from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import {
  newListTablesStep, type Connection, type TablesPage, type ListTablesInput,
} from '@superdurable/connector-clickhouse';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'clickhouse';
const tablesAttribute = new Attribute<TablesPage>('clickhouse-tables', jsonCodec<TablesPage>());

/** Read table names through a typed ClickHouse Query. */
export class TableInspectionFlow implements Flow<ListTablesInput> {
  private readonly complete = new CompleteTableInspection();
  private readonly read;

  /** Bind the static provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('ClickHouse connection name differs');
    this.read = newListTablesStep<ListTablesInput>({
      stepType: 'CallListTables',
      annotations: { groupId: 'provider', groupLabel: 'ClickHouse', explanation: 'Read table names from ClickHouse.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      listed: new StepRef('CompleteClickHouse'),
    });
  }

  /** Return the stable example Flow identity. */ getFlowType(): string { return 'ClickHouseTableInspectionTypeScript'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<ListTablesInput> {
    return StepList.startStep(this.read).otherSteps(this.complete);
  }
  /** Persist the table list result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [tablesAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the table list and complete the Flow."
class CompleteTableInspection implements Step<QueryResult<TablesPage>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteClickHouse'; }
  /** Persist and return the table list. */
  execute(context: Context, result: QueryResult<TablesPage>): StepDecision {
    tablesAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
