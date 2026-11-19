// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newExecuteStatementNonIdempotentStep, newExecuteStatementStep, newQueryRowsStep, type Connection, type RowSet, type StatementExecution } from '@superdurable/connector-postgresql';

/** Static Dex Web connection name for the refund ledger. */
export const ConnectionName = 'postgresql-ledger';
const refundColumns = 'refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at';
const lookupSQL = `SELECT ${refundColumns} FROM refund_ledger WHERE order_id = $1`;
const insertSQL = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key) VALUES ($1, $2::numeric, $3, $4) ON CONFLICT (idempotency_key) DO NOTHING RETURNING ${refundColumns}`;
// Running this twice counts the refund twice, so it uses executeNonIdempotent.
const addToDailyTotalSQL = 'INSERT INTO refund_daily_totals (refund_date, total_usd) VALUES (CURRENT_DATE, $1::numeric) ON CONFLICT (refund_date) DO UPDATE SET total_usd = refund_daily_totals.total_usd + EXCLUDED.total_usd RETURNING refund_date, total_usd';
const refundRequest = new Attribute<RefundInput>('postgresql-refund-request', jsonCodec<RefundInput>());
const recordedRefund = new Attribute<RefundOutcome>('postgresql-refund-recorded', jsonCodec<RefundOutcome>());

/** Safe Flow input; the password stays in the Worker credential provider. */
export interface RefundInput { orderId: string; amountUsd: string; externalReference?: string }
/** Durable outcome of one refund-ledger recording; a new refund also carries the day's refunded total. */
export interface RefundOutcome { status: 'recorded' | 'alreadyRecorded'; row: Record<string, unknown | null>; dailyTotalUsd?: string }

/** Query before inserting an idempotent refund row, then add a new refund to the daily total once. */
export class RecordRefundFlow implements Flow<RefundInput> {
  private readonly prepare = new PrepareRefund();
  private readonly decide = new DecideRefund();
  private readonly complete = new CompleteRefund();
  private readonly already = new AlreadyRecorded();
  private readonly completeDailyTotal = new CompleteDailyTotal();

  /** Bind a static named PostgreSQL connection. */ constructor(private readonly connection: Connection) { if (!connection) throw new TypeError('PostgreSQL connection is required'); }
  /** Stable Flow identity. */ getFlowType(): string { return 'PostgreSQLRecordRefundTypeScript'; }
  /** Register the lookup, write, daily-total, and completion Steps. */
  getSteps(): StepList<RefundInput> {
    return StepList.startStep(this.prepare).otherSteps(
      newQueryRowsStep({ stepType: 'FindRecordedRefundTypeScript', annotations: { groupId: 'postgresql-refund', groupLabel: 'PostgreSQL Refund', explanation: 'Find any existing refund for the order.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ statement: lookupSQL, parameters: [input.orderId] }), completed: this.decide }),
      this.decide,
      newExecuteStatementStep({ stepType: 'InsertRefundTypeScript', annotations: { groupId: 'postgresql-refund', groupLabel: 'PostgreSQL Refund', explanation: 'Insert one refund using the Dex call identity.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => ({ statement: insertSQL, parameters: [input.orderId, input.amountUsd, input.externalReference ?? null], idempotencyKeyPlaceholder: 4, maxRowsAffected: 1 }), completed: this.complete }),
      this.complete, this.already,
      newExecuteStatementNonIdempotentStep({ stepType: 'AddRefundToDailyTotalTypeScript', annotations: { groupId: 'postgresql-refund', groupLabel: 'PostgreSQL Refund', explanation: "Add the new refund to today's total at most once." }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: (outcome: RefundOutcome) => ({ statement: addToDailyTotalSQL, parameters: [String(outcome.row.amount_usd)], maxRowsAffected: 1 }), completed: this.completeDailyTotal }),
      this.completeDailyTotal,
    );
  }
  /** Register the request and the recorded refund used by later Steps. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [refundRequest, recordedRefund] }; }
}

// dex:group group-id:postgresql-refund group-label:"PostgreSQL Refund"
// dex:explanation text:"Validate the refund fields before the database lookup."
class PrepareRefund implements Step<RefundInput> {
  getStepType(): string { return 'PrepareRefundTypeScript'; }
  execute(context: Context, input: RefundInput): StepDecision {
    if (!input.orderId?.trim() || !/^[0-9]{1,10}(?:\.[0-9]{1,2})?$/.test(input.amountUsd) || Number(input.amountUsd) <= 0) return forceFail('orderId and a positive amountUsd are required');
    refundRequest.set(context, input);
    return goTo(new StepRef('FindRecordedRefundTypeScript'), input);
  }
}

// dex:group group-id:postgresql-refund group-label:"PostgreSQL Refund"
// dex:explanation text:"Complete an existing refund or prepare an idempotent insert."
class DecideRefund implements Step<QueryResult<RowSet>> {
  getStepType(): string { return 'DecideRefundTypeScript'; }
  execute(context: Context, result: QueryResult<RowSet>): StepDecision {
    const row = result.value.rows?.[0];
    if (row) return goTo(new StepRef('AlreadyRecordedTypeScript'), row);
    const input = refundRequest.get(context);
    return goTo(new StepRef('InsertRefundTypeScript'), input);
  }
}

// dex:group group-id:postgresql-refund group-label:"PostgreSQL Refund"
// dex:explanation text:"Record the committed refund row, then add it to the daily total."
class CompleteRefund implements Step<MutationResult<StatementExecution>> {
  getStepType(): string { return 'CompleteRefundTypeScript'; }
  execute(context: Context, result: MutationResult<StatementExecution>): StepDecision {
    const row = result.value.rows?.[0];
    if (!row) return forceFail('the insert returned no refund row; inspect the ledger before retrying');
    const outcome: RefundOutcome = { status: 'recorded', row };
    recordedRefund.set(context, outcome);
    return goTo(new StepRef('AddRefundToDailyTotalTypeScript'), outcome);
  }
}

// dex:group group-id:postgresql-refund group-label:"PostgreSQL Refund"
// dex:explanation text:"Complete the Flow with the refund row and today's refunded total."
class CompleteDailyTotal implements Step<MutationResult<StatementExecution>> {
  getStepType(): string { return 'CompleteDailyTotalTypeScript'; }
  execute(context: Context, result: MutationResult<StatementExecution>): StepDecision {
    const total = result.value.rows?.length === 1 ? result.value.rows[0]?.total_usd : undefined;
    if (typeof total !== 'string') return forceFail('the daily total update returned no numeric total');
    return gracefulComplete({ ...recordedRefund.get(context), dailyTotalUsd: total } satisfies RefundOutcome);
  }
}

// dex:group group-id:postgresql-refund group-label:"PostgreSQL Refund"
// dex:explanation text:"Complete the Flow with the existing refund row."
class AlreadyRecorded implements Step<Record<string, unknown | null>> {
  getStepType(): string { return 'AlreadyRecordedTypeScript'; }
  execute(_context: Context, row: Record<string, unknown | null>): StepDecision { return gracefulComplete({ status: 'alreadyRecorded', row } satisfies RefundOutcome); }
}
