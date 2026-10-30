// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
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

// Invoke runs one statement in its own transaction and commits it.
//
// Invoke opens one connection, pins the connector's session settings, sends START TRANSACTION,
// prepares the statement, checks its placeholders, runs it, reads any RETURNING rows, and reads
// ROW_COUNT() and LAST_INSERT_ID(). It rolls back, by closing the connection before COMMIT, when a
// bound or MaxRowsAffected is exceeded. Every failure before COMMIT leaves nothing committed:
// transient ones return Retry and conclusive ones select a branch. A failure after COMMIT is sent
// selects uncertain, because the server may have committed.
//
// Dex can run Invoke more than once for one Step execution, for example after a Worker is lost
// mid-call or when an async Step outlasts its local phase. Make the statement idempotent, such as
// INSERT ... ON DUPLICATE KEY UPDATE keyed by IdempotencyKeyPlaceholder.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return executeDefect(sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return executeDefect(sdkgo.FailureAuthentication, "MySQL connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	transaction, err := session.beginTransaction(ctx, false)
	if err != nil {
		failure := session.classifyFailure(err, phaseBegin)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if err := validatePreparedStatement(prepared, len(statement.parameterValues)); err != nil {
		return executeDefect(sdkgo.FailureValidation, err.Error(), client.receipt(call, session, classifiedFailure{}))
	}
	queryer, isQueryer := prepared.(driver.StmtQueryContext)
	if !isQueryer {
		failure := session.classifyFailure(errDriverInterfaceMissing, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	rows, err := queryer.QueryContext(ctx, statement.parameterValues)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if read.isTruncated {
		failure := classifiedFailure{
			kind:    sdkgo.FailureResponseTooLarge,
			message: "RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
		}
		return sdkgo.NewMutationBranch(ExecuteStatementBranchLimitExceeded, emptyStatementExecution(), failurePointer(executeOperationID, failure), client.receipt(call, session, failure))
	}
	if err := rows.Close(); err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	outcome, err := session.readStatementOutcome(ctx)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if outcome.rowsAffected < 0 {
		outcome.rowsAffected = int64(len(read.rows))
	}
	if input.MaxRowsAffected != nil && outcome.rowsAffected > *input.MaxRowsAffected {
		failure := classifiedFailure{
			kind:    sdkgo.FailureConflict,
			message: fmt.Sprintf("the statement affected %d rows but maxRowsAffected is %d, so the transaction was rolled back", outcome.rowsAffected, *input.MaxRowsAffected),
		}
		return sdkgo.NewMutationBranch(ExecuteStatementBranchLimitExceeded, emptyStatementExecution(), failurePointer(executeOperationID, failure), client.receipt(call, session, failure))
	}
	if err := session.commitTransaction(ctx, transaction); err != nil {
		failure := session.classifyFailure(err, phaseCommit)
		return executeAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	execution := StatementExecution{
		RowsAffected: outcome.rowsAffected, LastInsertID: outcome.lastInsertID, WarningCount: outcome.warningCount,
		Columns: read.columns, Rows: read.rows,
	}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchCompleted, execution, nil, client.receipt(call, session, classifiedFailure{}))
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
	return StatementExecution{LastInsertID: "0", Columns: []Column{}, Rows: []map[string]any{}}
}
