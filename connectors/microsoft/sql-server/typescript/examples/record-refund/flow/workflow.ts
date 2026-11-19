// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult } from '@superdurable/connectors-sdk';
import { newExecuteStatementStep, type Connection, type ExecuteStatementInput, type StatementExecution } from '@superdurable/connector-microsoft-sql-server';

/** Static Dex Web connection name for the SQL Server refund example. */
export const ConnectionName = 'sql-server-refunds';
/** One refund request with an exact decimal amount. */
export interface RefundInput {
  /** Stable order identifier, at most 100 characters. */ orderId: string;
  /** Positive USD amount with exactly two decimal places. */ amountUsd: string;
  /** External ticket or request reference, at most 200 characters. */ externalReference: string;
}
/** Safe request retained for inspection. */
export const refundInput = new Attribute<RefundInput>('sql-server-refund-input', jsonCodec<RefundInput>());
/** Committed insert result retained for inspection. */
export const refundResult = new Attribute<StatementExecution>('sql-server-refund-result', jsonCodec<StatementExecution>());

const insertRefund = `INSERT INTO dbo.refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
  OUTPUT INSERTED.refund_id AS refund_id, INSERTED.order_id AS order_id
  SELECT @p1, CAST(@p2 AS decimal(12, 2)), @p3, CAST(@p4 AS uniqueidentifier)
  WHERE NOT EXISTS (SELECT 1 FROM dbo.refund_ledger WITH (UPDLOCK, HOLDLOCK) WHERE order_id = @p1 OR idempotency_key = CAST(@p4 AS uniqueidentifier))`;

/** Insert one refund through a transactional Connector Step. */
export class RecordRefundFlow implements Flow<RefundInput> {
  private readonly prepare = new PrepareRefund();
  private readonly complete = new CompleteRefund();
  /** Bind a named connection when registering the Worker. */ constructor(private readonly connection: Connection) {}
  /** Stable Flow type. */ getFlowType(): string { return 'SQLServerRecordRefundTypeScriptFlow'; }
  /** Register the provider and application Steps. */
  getSteps(): StepList<RefundInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newExecuteStatementStep<RefundInput>({ stepType: 'InsertSQLServerRefund', annotations: { groupId: 'sql-server-refund', groupLabel: 'SQL Server Refund', explanation: 'Insert one idempotent refund.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ statement: insertRefund, parameters: [input.orderId, input.amountUsd, input.externalReference], idempotencyKeyPlaceholder: 4, maxRowsAffected: 1, returnsOutputRows: true }) satisfies ExecuteStatementInput, completed: this.complete }),
      this.complete,
    );
  }
  /** Register every persisted Attribute. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [refundInput, refundResult] }; }
}

// dex:group group-id:sql-server-refund group-label:"SQL Server Refund"
// dex:explanation text:"Validate and retain a refund request before inserting it."
/** Prepare one refund request. */
export class PrepareRefund implements Step<RefundInput> {
  /** Stable Step type. */ getStepType(): string { return 'PrepareSQLServerRefund'; }
  /** Start the provider Step. */ execute(context: Context, input: RefundInput): StepDecision {
    if (!input.orderId || input.orderId.length > 100 || !/^\d{1,10}\.\d{2}$/.test(input.amountUsd) || Number(input.amountUsd) <= 0 || !input.externalReference || input.externalReference.length > 200) return forceFail('refund input is invalid');
    refundInput.set(context, input);
    return goTo(new StepRef('InsertSQLServerRefund'), input);
  }
}

// dex:group group-id:sql-server-refund group-label:"SQL Server Refund"
// dex:explanation text:"Persist the committed refund result and complete the Flow."
/** Complete a successful refund insert. */
export class CompleteRefund implements Step<MutationResult<StatementExecution>> {
  /** Stable Step type. */ getStepType(): string { return 'CompleteSQLServerRefund'; }
  /** Retain the committed result. */ execute(context: Context, result: MutationResult<StatementExecution>): StepDecision {
    refundResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
