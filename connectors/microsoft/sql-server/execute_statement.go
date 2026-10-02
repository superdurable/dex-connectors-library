// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"database/sql/driver"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const executeOperationID = "execute"

// ExecuteStatementInput is one parameterized write statement.
type ExecuteStatementInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "INSERT INTO dbo.refunds (order_id, amount) VALUES (@p1, CAST(@p2 AS decimal(12, 2)))".
	// Values belong in Parameters: the connector never formats them into SQL text, and it cannot
	// detect a statement built by concatenating untrusted input. Only statements that start with
	// INSERT, UPDATE, DELETE, MERGE, or WITH are accepted. A MERGE must end with a semicolon.
	Statement string `json:"statement"`
	// Parameters binds @p1 through @pN in order, with the same value rules as QueryRowsInput.
	Parameters []any `json:"parameters,omitempty"`
	// IdempotencyKeyPlaceholder, when non-zero, is the number n of the @pn placeholder that receives
	// this Step execution's idempotency key. It must equal len(Parameters)+1, so the key is always
	// the last placeholder. The key is a 36-character lowercase UUID, bound as nvarchar, that stays
	// the same across every retry and Worker replacement of one Step execution; it is also returned
	// in the Result's Receipt.
	IdempotencyKeyPlaceholder int `json:"idempotencyKeyPlaceholder,omitempty"`
	// MaxRowsAffected, when set, is the most rows the statement may affect. A statement that affects
	// more is rolled back and selects limitExceeded, which guards against a missing WHERE clause.
	// Nil applies no limit; zero allows only a statement that changes nothing.
	MaxRowsAffected *int64 `json:"maxRowsAffected,omitempty"`
	// ReturnsOutputRows declares that the statement has an OUTPUT clause that returns rows to the
	// client. The connector then returns those rows, bounded by maxRows and maxResponseBytes, and
	// counts them as the rows affected. Leave it false for a statement without OUTPUT, or whose
	// OUTPUT clause only writes INTO a table; OUTPUT rows of such a statement are discarded.
	ReturnsOutputRows bool `json:"returnsOutputRows,omitempty"`
}

// StatementExecution is the committed outcome of one ExecuteStatement transaction.
type StatementExecution struct {
	// RowsAffected is the number of rows the statement inserted, updated, deleted, or merged. With
	// ReturnsOutputRows it is the number of OUTPUT rows, one per affected row. Otherwise it is the
	// sum of the row counts the server reported, which also includes rows changed by an AFTER
	// trigger that does not SET NOCOUNT ON.
	RowsAffected int64 `json:"rowsAffected"`
	// Columns lists the OUTPUT columns in statement order; it is empty without ReturnsOutputRows.
	Columns []Column `json:"columns"`
	// Rows holds the OUTPUT rows with the same type mapping as RowSet.Rows; it is empty, never nil,
	// without ReturnsOutputRows.
	Rows []map[string]any `json:"rows"`
}

// ExecuteStatementOperation implements the transactional write operation.
type ExecuteStatementOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ExecuteStatementOperation) Definition() sdkgo.MutationDefinition {
	return ExecuteStatementDefinition
}

// IdempotencyKey returns the stable connector Call ID, a UUID, as the statement's idempotency key.
func (ExecuteStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ ExecuteStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke runs one statement in its own transaction and commits it.
//
// Invoke opens one connection, pins the connector's session settings, begins a transaction, runs
// the statement, and reads any OUTPUT rows. It rolls back, by closing the connection before
// COMMIT, when a bound or MaxRowsAffected is exceeded. Every failure before COMMIT leaves nothing
// committed: transient ones return Retry and conclusive ones select a branch. A failure after
// COMMIT is sent selects uncertain, because the server may have committed.
//
// Dex can run Invoke more than once for one Step execution, for example after a Worker is lost
// mid-call or when an async Step outlasts its local phase. Make the statement idempotent, such as
// an INSERT ... SELECT ... WHERE NOT EXISTS keyed by IdempotencyKeyPlaceholder.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return executeDefect(sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return executeDefect(sdkgo.FailureAuthentication, "SQL Server connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	transaction, err := session.beginTransaction(ctx)
	if err != nil {
		failure := session.classifyFailure(err, phaseBegin)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	statementCtx, cancelStatement := client.newStatementContext(ctx)
	defer cancelStatement()
	execution := emptyStatementExecution()
	if input.ReturnsOutputRows {
		rows, err := session.queryStatement(statementCtx, prepared, statement.parameterValues)
		if err != nil {
			failure := session.classifyStatementFailure(err, statementCtx, ctx)
			return executeAttemptForFailure(failure, client.receipt(call, session, failure))
		}
		read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
		if err != nil {
			failure := session.classifyStatementFailure(err, statementCtx, ctx)
			return executeAttemptForFailure(failure, client.receipt(call, session, failure))
		}
		if read.isTruncated {
			failure := classifiedFailure{
				kind:    sdkgo.FailureResponseTooLarge,
				message: "OUTPUT rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
			}
			return sdkgo.NewMutationBranch(ExecuteStatementBranchLimitExceeded, emptyStatementExecution(), failurePointer(executeOperationID, failure), client.receipt(call, session, failure))
		}
		if len(read.columns) == 0 {
			return executeDefect(sdkgo.FailureValidation, "returnsOutputRows is set but the statement returned no OUTPUT rows result set, so the transaction was rolled back",
				client.receipt(call, session, classifiedFailure{}))
		}
		if err := session.closeRows(); err != nil {
			failure := session.classifyStatementFailure(err, statementCtx, ctx)
			return executeAttemptForFailure(failure, client.receipt(call, session, failure))
		}
		execution = StatementExecution{RowsAffected: int64(len(read.rows)), Columns: read.columns, Rows: read.rows}
	} else {
		rowsAffected, err := session.execStatement(statementCtx, prepared, statement.parameterValues)
		if err != nil {
			failure := session.classifyStatementFailure(err, statementCtx, ctx)
			return executeAttemptForFailure(failure, client.receipt(call, session, failure))
		}
		execution.RowsAffected = rowsAffected
	}
	if input.MaxRowsAffected != nil && execution.RowsAffected > *input.MaxRowsAffected {
		failure := classifiedFailure{
			kind:    sdkgo.FailureConflict,
			message: fmt.Sprintf("the statement affected %d rows but maxRowsAffected is %d, so the transaction was rolled back", execution.RowsAffected, *input.MaxRowsAffected),
		}
		return sdkgo.NewMutationBranch(ExecuteStatementBranchLimitExceeded, emptyStatementExecution(), failurePointer(executeOperationID, failure), client.receipt(call, session, failure))
	}
	if err := session.commitTransaction(ctx, transaction); err != nil {
		failure := session.classifyFailure(err, phaseCommit)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchCompleted, execution, nil, client.receipt(call, session, classifiedFailure{}))
}

// bindExecuteStatement validates the input and appends the idempotency key as the last placeholder.
func bindExecuteStatement(call sdkgo.Call, input ExecuteStatementInput) (boundStatement, error) {
	if input.MaxRowsAffected != nil && *input.MaxRowsAffected < 0 {
		return boundStatement{}, fmt.Errorf("maxRowsAffected cannot be negative")
	}
	placeholderCount := len(input.Parameters)
	switch input.IdempotencyKeyPlaceholder {
	case 0:
	case len(input.Parameters) + 1:
		placeholderCount++
	default:
		return boundStatement{}, fmt.Errorf("idempotencyKeyPlaceholder must be 0 or %d, one more than the number of parameters", len(input.Parameters)+1)
	}
	statement, err := bindStatement(input.Statement, input.Parameters, placeholderCount, statementKindWrite)
	if err != nil {
		return boundStatement{}, err
	}
	if input.IdempotencyKeyPlaceholder != 0 {
		statement.parameterValues = append(statement.parameterValues, driver.NamedValue{
			Ordinal: len(statement.parameterValues) + 1, Value: string(call.IdempotencyKey),
		})
	}
	return statement, nil
}

func executeAttemptForFailure(failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementExecution] {
	switch failure.disposition {
	case dispositionRetry:
		return sdkgo.NewMutationRetry[StatementExecution](sdkFailure(executeOperationID, failure), 0)
	case dispositionUncertain:
		return sdkgo.NewMutationUncertain(emptyStatementExecution(), sdkFailure(executeOperationID, failure), receipt)
	case dispositionInvalidResponse:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchInvalidResponse, emptyStatementExecution(), failurePointer(executeOperationID, failure), receipt)
	case dispositionDefect:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchDefect, emptyStatementExecution(), failurePointer(executeOperationID, failure), receipt)
	default:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchProviderRejected, emptyStatementExecution(), failurePointer(executeOperationID, failure), receipt)
	}
}

func executeDefect(kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementExecution] {
	failure := classifiedFailure{disposition: dispositionDefect, kind: kind, message: message}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchDefect, emptyStatementExecution(), failurePointer(executeOperationID, failure), receipt)
}

func emptyStatementExecution() StatementExecution {
	return StatementExecution{Columns: []Column{}, Rows: []map[string]any{}}
}
