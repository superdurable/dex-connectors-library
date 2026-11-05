// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newQueryRowsStep, type Connection, type QueryRowsInput, type RowSet } from '../../src/index.js';

/** Static connection name configured in Dex Web. */
export const ConnectionName = 'mysql-refunds';
/** Persisted safe read request. */
export const refundQuery = new Attribute<QueryRowsInput>('mysql-refund-query', jsonCodec<QueryRowsInput>());
/** Persisted bounded database rows. */
export const refundRows = new Attribute<RowSet>('mysql-refund-rows', jsonCodec<RowSet>());

/** Query a refund ledger by its external order ID. */
export class LookupRefundFlow implements Flow<{ orderId: string }> {
  private readonly prepare = new PrepareRefundQuery();
  private readonly complete = new CompleteRefundQuery();
  /** Bind one named MySQL connection. */
  constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('MySQL connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'MySQLLookupRefundFlow'; }
  /** Register application and provider Steps. */
  getSteps(): StepList<{ orderId: string }> {
    return StepList.startStep(this.prepare).otherSteps(
      newQueryRowsStep({ stepType: 'QueryMySQLRefund', annotations: { groupId: 'mysql-refund', groupLabel: 'MySQL Refund', explanation: 'Read the bounded refund ledger row.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, completed: this.complete }),
      this.complete,
    );
  }
  /** Register all persisted Attributes. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [refundQuery, refundRows] }; }
}

/** Build one parameterized refund lookup. */
// dex:group group-id:mysql-refund group-label:"MySQL Refund"
// dex:explanation text:"Prepare the parameterized refund lookup without persisting credentials."
export class PrepareRefundQuery implements Step<{ orderId: string }> {
  /** Stable Step identity. */ getStepType(): string { return 'PrepareMySQLRefundQuery'; }
  /** Route a nonsecret order ID to the provider Step. */
  execute(context: Context, input: { orderId: string }): StepDecision {
    if (!input.orderId?.trim()) throw new TypeError('orderId is required');
    const query: QueryRowsInput = { statement: 'SELECT order_id, amount_usd FROM refund_ledger WHERE order_id = ?', parameters: [input.orderId] };
    refundQuery.set(context, query);
    return goTo(new StepRef('QueryMySQLRefund'), query);
  }
}

/** Finish with bounded query rows. */
// dex:group group-id:mysql-refund group-label:"MySQL Refund"
// dex:explanation text:"Persist the refund lookup result and complete the Flow."
export class CompleteRefundQuery implements Step<QueryResult<RowSet>> {
  /** Stable Step identity. */ getStepType(): string { return 'CompleteMySQLRefundQuery'; }
  /** Store safe row data and complete. */ execute(context: Context, result: QueryResult<RowSet>): StepDecision { refundRows.set(context, result.value); return gracefulComplete(result.value); }
}
