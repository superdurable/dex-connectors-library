// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

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
	// "INSERT INTO refunds (order_id, amount) VALUES (?, ?)". Values belong in Parameters: the
	// connector never formats them into SQL text, and it cannot detect a statement built by
	// concatenating untrusted input. Only INSERT, UPDATE, DELETE, REPLACE, and WITH statements are
	// accepted, because DDL, LOCK TABLES, and similar statements commit implicitly and would escape
	// the connector's transaction. A MariaDB RETURNING clause returns rows in the Result.
	Statement string `json:"statement"`
	// Parameters binds the ? placeholders in order, with the same value rules as QueryRowsInput.
	Parameters []any `json:"parameters,omitempty"`
	// IdempotencyKeyPlaceholder, when non-zero, is the position n of the nth ? placeholder, which
	// receives this Step execution's idempotency key. It must equal len(Parameters)+1, so the key is
	// always the last placeholder. The key is a 36-character UUID that stays the same across every
	// retry and Worker replacement of one Step execution, and it is also returned in the Result's
	// Receipt.
	IdempotencyKeyPlaceholder int `json:"idempotencyKeyPlaceholder,omitempty"`
	// MaxRowsAffected, when set, is the most rows the statement may affect. A statement that affects
	// more is rolled back and selects limitExceeded, which guards against a missing WHERE clause.
	// Nil applies no limit; zero allows only a statement that changes nothing.
	MaxRowsAffected *int64 `json:"maxRowsAffected,omitempty"`
}

// StatementExecution is the committed outcome of one ExecuteStatement transaction.
type StatementExecution struct {
	// RowsAffected is ROW_COUNT() after the statement: rows inserted, deleted, or changed. An UPDATE
	// counts only rows whose values changed. An INSERT ... ON DUPLICATE KEY UPDATE counts 1 for an
	// inserted row, 2 for a changed existing row, and 0 for an existing row left unchanged. For a
	// MariaDB RETURNING statement it is the number of returned rows.
	RowsAffected int64 `json:"rowsAffected"`
	// LastInsertID is LAST_INSERT_ID() after the statement as decimal text: the first AUTO_INCREMENT
	// value the statement inserted, the value passed to LAST_INSERT_ID(expr) if the statement called
	// it, or "0". The connector's session is new, so no earlier statement contributes to it.
	LastInsertID string `json:"lastInsertId"`
	// WarningCount is the number of warnings and notes the statement raised, such as a decimal
	// rounded to its column's scale.
	WarningCount int64 `json:"warningCount"`
	// Columns lists MariaDB RETURNING columns in statement order; it is empty without RETURNING.
	Columns []Column `json:"columns"`
	// Rows holds MariaDB RETURNING rows with the same type mapping as RowSet.Rows; it is empty,
	// never nil, without RETURNING.
	Rows []map[string]any `json:"rows"`
}

// statementOutcome is what ROW_COUNT(), LAST_INSERT_ID(), and @@warning_count report after a statement.
type statementOutcome struct {
	rowsAffected int64
	lastInsertID string
	warningCount int64
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
// Invoke opens one connection, pins the connector's session settings, sends START TRANSACTION,
// prepares the statement, checks its placeholders, runs it, reads any RETURNING rows, and reads
// ROW_COUNT() and LAST_INSERT_ID(). It rolls back, by closing the connection before COMMIT, when a
// bound or MaxRowsAffected is exceeded. Every failure before COMMIT leaves nothing committed:
// transient ones return Retry and conclusive ones select a branch. A COMMIT whose outcome is
// unknown, such as a connection lost after COMMIT was sent, also returns Retry, so Dex runs the
// statement again; execute never selects uncertain.
//
// Dex can therefore run the statement more than once for one Step execution, also after a Worker
// is lost mid-call or when an async Step outlasts its local phase. Make the statement repeat-safe,
// such as INSERT ... ON DUPLICATE KEY UPDATE keyed by IdempotencyKeyPlaceholder, an absolute
// UPDATE, or a DELETE. Use ExecuteStatementNonIdempotent for a statement that must not run twice.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return writeDefect(executeOperationID, sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return writeDefect(executeOperationID, sdkgo.FailureAuthentication, "MySQL connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, session, failure))
	}
	pending, failure, isReadyToCommit := client.runStatementBeforeCommit(ctx, session, statement, input.MaxRowsAffected)
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
	return sdkgo.NewMutationBranch(ExecuteStatementBranchCompleted, pending.execution(), nil, client.receipt(call, session, classifiedFailure{}))
}

// pendingCommit is an open transaction whose statement ran within every bound.
type pendingCommit struct {
	transaction driver.Tx
	read        rowRead
	outcome     statementOutcome
}

func (pending pendingCommit) execution() StatementExecution {
	return StatementExecution{
		RowsAffected: pending.outcome.rowsAffected, LastInsertID: pending.outcome.lastInsertID, WarningCount: pending.outcome.warningCount,
		Columns: pending.read.columns, Rows: pending.read.rows,
	}
}

// runStatementBeforeCommit starts the transaction and runs the statement within its bounds.
// It never sends COMMIT, so a returned failure always means nothing was committed.
func (client *Client) runStatementBeforeCommit(ctx context.Context, session *databaseSession, statement boundStatement, maxRowsAffected *int64) (pendingCommit, classifiedFailure, bool) {
	transaction, err := session.beginTransaction(ctx, false)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseBegin), false
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	if err := validatePreparedStatement(prepared, len(statement.parameterValues)); err != nil {
		return pendingCommit{}, classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}, false
	}
	queryer, isQueryer := prepared.(driver.StmtQueryContext)
	if !isQueryer {
		return pendingCommit{}, session.classifyFailure(errDriverInterfaceMissing, phaseStatement), false
	}
	rows, err := queryer.QueryContext(ctx, statement.parameterValues)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	if read.isTruncated {
		return pendingCommit{}, classifiedFailure{
			disposition: dispositionLimitExceeded, kind: sdkgo.FailureResponseTooLarge,
			message: "RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
		}, false
	}
	if err := rows.Close(); err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	outcome, err := session.readStatementOutcome(ctx)
	if err != nil {
		return pendingCommit{}, session.classifyFailure(err, phaseStatement), false
	}
	if outcome.rowsAffected < 0 {
		outcome.rowsAffected = int64(len(read.rows))
	}
	if maxRowsAffected != nil && outcome.rowsAffected > *maxRowsAffected {
		return pendingCommit{}, classifiedFailure{
			disposition: dispositionLimitExceeded, kind: sdkgo.FailureConflict,
			message: fmt.Sprintf("the statement affected %d rows but maxRowsAffected is %d, so the transaction was rolled back", outcome.rowsAffected, *maxRowsAffected),
		}, false
	}
	return pendingCommit{transaction: transaction, read: read, outcome: outcome}, classifiedFailure{}, true
}

// bindExecuteStatement validates the input and appends the idempotency key as the last placeholder.
func bindExecuteStatement(call sdkgo.Call, input ExecuteStatementInput) (boundStatement, error) {
	statement, err := bindStatement(input.Statement, input.Parameters, statementKindWrite)
	if err != nil {
		return boundStatement{}, err
	}
	if input.MaxRowsAffected != nil && *input.MaxRowsAffected < 0 {
		return boundStatement{}, fmt.Errorf("maxRowsAffected cannot be negative")
	}
	switch input.IdempotencyKeyPlaceholder {
	case 0:
		return statement, nil
	case len(input.Parameters) + 1:
		if len(statement.parameterValues) == maximumParameters {
			return boundStatement{}, fmt.Errorf("statement cannot bind more than %d parameters", maximumParameters)
		}
		statement.parameterValues = append(statement.parameterValues, driver.NamedValue{
			Ordinal: len(statement.parameterValues) + 1, Value: string(call.IdempotencyKey),
		})
		return statement, nil
	default:
		return boundStatement{}, fmt.Errorf("idempotencyKeyPlaceholder must be 0 or %d, one more than the number of parameters", len(input.Parameters)+1)
	}
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
	return StatementExecution{LastInsertID: "0", Columns: []Column{}, Rows: []map[string]any{}}
}
