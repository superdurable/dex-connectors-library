// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, StepRef, forceFail, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import { z } from 'zod';
import type { MutationResult, QueryResult } from '@superdurable/connectors-sdk';
import { newSubmitStatementStep, newGetStatementResultStep, newCancelStatementStep, type Connection, type StatementSubmission, type StatementResult, type StatementCancellation, type SubmitStatementInput, type GetStatementResultInput, type CancelStatementInput } from '../../src/index.js';

/** Static Dex Web connection name for this example. */
export const ConnectionName = 'snowflake-warehouse';
/** One account and first date for the aggregate. */
export interface UsageRequest { accountId: string; since: string }

const requestSchema = z.object({ accountId: z.string().min(1), since: z.iso.date() });
const requestCodec = jsonCodec<UsageRequest>({ typeName: 'SnowflakeUsageRequest', decode: value => requestSchema.parse(value) });
const statement = `SELECT COUNT(*) AS EVENT_COUNT, COALESCE(SUM(CREDITS_USED), 0) AS CREDITS_USED, MAX(EVENT_AT) AS LAST_EVENT_AT
FROM USAGE_EVENTS WHERE ACCOUNT_ID = ? AND EVENT_AT >= TO_DATE(?)`;

/** Submit a bounded aggregate, inspect it once, and cancel if it remains running. */
export class AccountUsageSummaryFlow implements Flow<UsageRequest> {
  private readonly completed = new CompleteUsageSummary();
  private readonly canceled = new CompleteCancellation();
  private readonly running = new CancelRunningUsageQuery();
  private readonly submitted = new ContinueSubmittedUsageQuery();
  private readonly start = new PrepareUsageRequest();

  /** Bind the example's static Snowflake connection. */
  constructor(private readonly connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('account usage requires the static Snowflake connection');
  }

  /** Return a stable TypeScript example Flow identity. */ getFlowType(): string { return 'SnowflakeTypeScriptAccountUsageSummary'; }
  /** Register every reachable Step. */
  getSteps(): StepList<UsageRequest> {
    return StepList.startStep<UsageRequest>(this.start).otherSteps(
      newSubmitStatementStep<SubmitStatementInput>({ stepType: 'SubmitUsageQuery', annotations: { groupId: 'snowflake', groupLabel: 'Snowflake', explanation: 'Submit the bound account usage aggregate without holding Execute open.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, submitted: this.submitted }),
      this.submitted,
      newGetStatementResultStep<GetStatementResultInput>({ stepType: 'ReadUsageResult', annotations: { groupId: 'snowflake', groupLabel: 'Snowflake', explanation: 'Read one bounded result partition from Snowflake.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, completed: this.completed,
        // Cancel a still-running statement so this one-read example does not leave warehouse work behind.
        running: this.running }),
      this.running,
      newCancelStatementStep<CancelStatementInput>({ stepType: 'CancelUsageQuery', annotations: { groupId: 'snowflake', groupLabel: 'Snowflake', explanation: 'Cancel a statement that remains running after the first status read.' }, connection: this.connection, connectionName: ConnectionName, mapToOperationInput: input => input, canceled: this.canceled }),
      this.completed, this.canceled,
    );
  }
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Validate the account and date, then bind both values outside SQL text."
class PrepareUsageRequest implements Step<UsageRequest> {
  readonly inputCodec = requestCodec;
  getStepType() { return 'PrepareUsageRequest'; }
  execute(_context: Context, request: UsageRequest): StepDecision {
    if (!request.accountId || !/^\d{4}-\d{2}-\d{2}$/.test(request.since)) return forceFail('accountId and since are required');
    return goTo(new StepRef('SubmitUsageQuery'), { statement, parameters: [request.accountId, request.since] });
  }
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Carry Snowflake's statement handle to the first status read."
class ContinueSubmittedUsageQuery implements Step<MutationResult<StatementSubmission>> {
  getStepType() { return 'ContinueSubmittedUsageQuery'; }
  execute(_context: Context, result: MutationResult<StatementSubmission>): StepDecision { return goTo(new StepRef('ReadUsageResult'), { statementHandle: result.value.statementHandle }); }
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Cancel the statement when the first status read still finds it running."
class CancelRunningUsageQuery implements Step<QueryResult<StatementResult>> {
  getStepType() { return 'CancelRunningUsageQuery'; }
  execute(_context: Context, result: QueryResult<StatementResult>): StepDecision { return goTo(new StepRef('CancelUsageQuery'), { statementHandle: result.value.statementHandle }); }
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Return the completed aggregate row and its Snowflake handle."
class CompleteUsageSummary implements Step<QueryResult<StatementResult>> {
  getStepType() { return 'CompleteUsageSummary'; }
  execute(_context: Context, result: QueryResult<StatementResult>): StepDecision { return gracefulComplete({ status: 'completed', statementHandle: result.value.statementHandle, rows: result.value.rows }); }
}

// dex:group group-id:snowflake group-label:"Snowflake"
// dex:explanation text:"Report that Snowflake accepted cancellation after the first status read."
class CompleteCancellation implements Step<MutationResult<StatementCancellation>> {
  getStepType() { return 'CompleteCancellation'; }
  execute(_context: Context, result: MutationResult<StatementCancellation>): StepDecision { return gracefulComplete({ status: 'canceled', statementHandle: result.value.statementHandle }); }
}
