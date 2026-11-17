// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"context"
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

// Invoke runs one repeat-safe statement in its own transaction and commits it.
//
// Invoke opens one connection, pins the connector's session settings, begins a transaction, runs
// the statement, and reads any OUTPUT rows. It rolls back, by closing the connection before
// COMMIT, when a bound or MaxRowsAffected is exceeded. Every failure before COMMIT leaves nothing
// committed: transient ones return Retry and conclusive ones select a branch. A COMMIT whose
// outcome is unknown, such as a connection lost after COMMIT was sent, also returns Retry, so Dex
// runs the statement again; execute never selects uncertain.
//
// Dex can therefore run the statement more than once for one Step execution, also after a Worker
// is lost mid-call or when an async Step outlasts its local phase. Make the statement repeat-safe,
// such as an INSERT ... SELECT ... WHERE NOT EXISTS keyed by IdempotencyKeyPlaceholder, an
// absolute UPDATE, or a DELETE. Use ExecuteStatementNonIdempotent for a statement that must not
// run twice.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return writeDefect(executeOperationID, sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return writeDefect(executeOperationID, sdkgo.FailureAuthentication, "SQL Server connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, session, failure))
	}
	pending, failure, isReadyToCommit := client.runStatementBeforeCommit(ctx, session, statement, input)
	if !isReadyToCommit {
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, session, failure))
	}
	if err := session.commitTransaction(ctx, pending.transaction); err != nil {
		failure := session.classifyFailure(err, phaseCommit)
		if failure.disposition == dispositionUncertain {
			// The statement is repeat-safe, so an unknown COMMIT outcome runs it again.
			failure.disposition = dispositionRetry
		}
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, session, failure))
	}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchCompleted, pending.execution, nil, client.receipt(call, session, classifiedFailure{}))
}

// pendingCommit is an open transaction whose statement ran within every bound.
type pendingCommit struct {
	transaction driver.Tx
	execution   StatementExecution
}

// runStatementBeforeCommit begins the transaction and runs the statement within its bounds.
// It never sends COMMIT, so a returned failure always means nothing was committed.
func (client *Client) runStatementBeforeCommit(ctx context.Context, session *databaseSession, statement boundStatement, input ExecuteStatementInput) (pendingCommit, classifiedFailure, bool) {
	transaction, err := session.beginTransaction(ctx)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseBegin), false
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	statementCtx, cancelStatement := client.newStatementContext(ctx)
	defer cancelStatement()
	execution := emptyStatementExecution()
	if input.ReturnsOutputRows {
		rows, err := session.queryStatement(statementCtx, prepared, statement.parameterValues)
		if err != nil {
			return pendingCommit{}, session.classifyStatementFailure(err, statementCtx, ctx), false
		}
		read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
		if err != nil {
			return pendingCommit{}, session.classifyStatementFailure(err, statementCtx, ctx), false
		}
		if read.isTruncated {
			return pendingCommit{}, classifiedFailure{
				disposition: dispositionLimitExceeded, kind: sdkgo.FailureResponseTooLarge,
				message: "OUTPUT rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
			}, false
		}
		if len(read.columns) == 0 {
			return pendingCommit{}, classifiedFailure{
				disposition: dispositionDefect, kind: sdkgo.FailureValidation,
				message: "returnsOutputRows is set but the statement returned no OUTPUT rows result set, so the transaction was rolled back",
			}, false
		}
		if err := session.closeRows(); err != nil {
			return pendingCommit{}, session.classifyStatementFailure(err, statementCtx, ctx), false
		}
		execution = StatementExecution{RowsAffected: int64(len(read.rows)), Columns: read.columns, Rows: read.rows}
	} else {
		rowsAffected, err := session.execStatement(statementCtx, prepared, statement.parameterValues)
		if err != nil {
			return pendingCommit{}, session.classifyStatementFailure(err, statementCtx, ctx), false
		}
		execution.RowsAffected = rowsAffected
	}
	if input.MaxRowsAffected != nil && execution.RowsAffected > *input.MaxRowsAffected {
		return pendingCommit{}, classifiedFailure{
			disposition: dispositionLimitExceeded, kind: sdkgo.FailureConflict,
			message: fmt.Sprintf("the statement affected %d rows but maxRowsAffected is %d, so the transaction was rolled back", execution.RowsAffected, *input.MaxRowsAffected),
		}, false
	}
	return pendingCommit{transaction: transaction, execution: execution}, classifiedFailure{}, true
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

// writeAttemptForFailure maps a failure to a branch; both write operations share their branch IDs.
func writeAttemptForFailure(operationID string, failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementExecution] {
	switch failure.disposition {
	case dispositionRetry:
		return sdkgo.NewMutationRetry[StatementExecution](sdkFailure(operationID, failure), 0)
	case dispositionUncertain:
		return sdkgo.NewMutationUncertain(emptyStatementExecution(), sdkFailure(operationID, failure), receipt)
	case dispositionLimitExceeded:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchLimitExceeded, emptyStatementExecution(), failurePointer(operationID, failure), receipt)
	case dispositionInvalidResponse:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchInvalidResponse, emptyStatementExecution(), failurePointer(operationID, failure), receipt)
	case dispositionDefect:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchDefect, emptyStatementExecution(), failurePointer(operationID, failure), receipt)
	default:
		return sdkgo.NewMutationBranch(ExecuteStatementBranchProviderRejected, emptyStatementExecution(), failurePointer(operationID, failure), receipt)
	}
}

func writeDefect(operationID string, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementExecution] {
	return writeAttemptForFailure(operationID, classifiedFailure{disposition: dispositionDefect, kind: kind, message: message}, receipt)
}

func emptyStatementExecution() StatementExecution {
	return StatementExecution{Columns: []Column{}, Rows: []map[string]any{}}
}
