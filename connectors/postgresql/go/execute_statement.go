// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const executeOperationID = "execute"

// ExecuteStatementInput is one parameterized write statement.
type ExecuteStatementInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "INSERT INTO refunds (order_id, amount) VALUES ($1, $2::numeric)". Values belong in
	// Parameters: the connector never formats them into SQL text, and it cannot detect a statement
	// built by concatenating untrusted input. Transaction control and COPY are rejected before any
	// request. A RETURNING clause returns rows in the Result.
	Statement string `json:"statement"`
	// Parameters binds $1 through $n in order, with the same value rules as QueryRowsInput.
	Parameters []any `json:"parameters,omitempty"`
	// IdempotencyKeyPlaceholder, when non-zero, is the number n of the $n placeholder that receives
	// this Step execution's idempotency key. It must equal len(Parameters)+1, so the key is always
	// the last placeholder. The key is a UUID that stays the same across every retry and Worker
	// replacement of one Step execution, and it is also returned in the Result's Receipt.
	IdempotencyKeyPlaceholder int `json:"idempotencyKeyPlaceholder,omitempty"`
	// MaxRowsAffected, when set, is the most rows the statement may affect. A statement that affects
	// more is rolled back and selects limitExceeded, which guards against a missing WHERE clause.
	// Nil applies no limit; zero allows only a statement that changes nothing.
	MaxRowsAffected *int64 `json:"maxRowsAffected,omitempty"`
}

// StatementExecution is the committed outcome of one ExecuteStatement transaction.
type StatementExecution struct {
	// Command is the PostgreSQL command tag without its counts, such as INSERT, UPDATE, or MERGE.
	Command string `json:"command"`
	// RowsAffected is the row count PostgreSQL reported for the command. An INSERT ... ON CONFLICT
	// DO NOTHING that found its row already present reports zero.
	RowsAffected int64 `json:"rowsAffected"`
	// Columns lists the RETURNING columns in statement order; it is empty without RETURNING.
	Columns []Column `json:"columns"`
	// Rows holds the RETURNING rows with the same type mapping as RowSet.Rows; it is empty, never
	// nil, without RETURNING.
	Rows []map[string]any `json:"rows"`
}

// ExecuteStatementOperation implements the transactional write operation.
type ExecuteStatementOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ExecuteStatementOperation) Definition() sdkgo.MutationDefinition {
	return ExecuteStatementDefinition
}

// IdempotencyKey returns the stable connector Call ID, a UUID, as PostgreSQL's idempotency key.
func (ExecuteStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ ExecuteStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke runs one repeat-safe statement in its own transaction and commits it.
//
// Invoke opens one connection, sends BEGIN with the connector's session settings, prepares the
// statement, checks its placeholders, runs it, and reads any RETURNING rows. It rolls back, by
// closing the connection before COMMIT, when a bound or MaxRowsAffected is exceeded. Every failure
// before COMMIT leaves nothing committed: transient ones return Retry and conclusive ones select a
// branch. A COMMIT whose outcome is unknown, such as a connection lost after COMMIT was sent, also
// returns Retry, so Dex runs the statement again; execute never selects uncertain.
//
// Dex can therefore run the statement more than once for one Step execution, also after a Worker
// is lost mid-call or when an async Step outlasts its local phase. Make the statement repeat-safe,
// such as INSERT ... ON CONFLICT DO NOTHING keyed by IdempotencyKeyPlaceholder, an absolute UPDATE,
// or a DELETE. Use ExecuteStatementNonIdempotent for a statement that must not run twice.
func (operation ExecuteStatementOperation) Invoke(call sdkgo.Call, input ExecuteStatementInput) sdkgo.MutationAttempt[StatementExecution] {
	client := operation.client
	statement, err := bindExecuteStatement(call, input)
	if err != nil {
		return writeDefect(executeOperationID, sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return writeDefect(executeOperationID, sdkgo.FailureAuthentication, "PostgreSQL connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	connection, err := client.connect(ctx, password)
	if err != nil {
		failure := classifyFailure(err, phaseConnect)
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, nil, failure))
	}
	defer client.closeConnection(connection)
	read, failure, isReadyToCommit := client.runStatementBeforeCommit(ctx, connection, statement, input.MaxRowsAffected)
	if !isReadyToCommit {
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, connection, failure))
	}
	if err := client.commitTransaction(ctx, connection); err != nil {
		failure := classifyFailure(err, phaseCommit)
		if failure.disposition == dispositionUncertain {
			// The statement is repeat-safe, so an unknown COMMIT outcome runs it again.
			failure.disposition = dispositionRetry
		}
		return writeAttemptForFailure(executeOperationID, failure, client.receipt(call, connection, failure))
	}
	return sdkgo.NewMutationBranch(ExecuteStatementBranchCompleted, newStatementExecution(read), nil, client.receipt(call, connection, classifiedFailure{}))
}

// runStatementBeforeCommit opens the transaction and runs the statement within its bounds.
// It never sends COMMIT, so a returned failure always means nothing was committed.
func (client *Client) runStatementBeforeCommit(ctx context.Context, connection *pgconn.PgConn, statement boundStatement, maxRowsAffected *int64) (rowRead, classifiedFailure, bool) {
	if err := client.beginTransaction(ctx, connection, transactionAccessDefault); err != nil {
		return rowRead{}, classifyFailure(err, phaseBegin), false
	}
	description, err := connection.Prepare(ctx, "", statement.sql, nil)
	if err != nil {
		return rowRead{}, classifyFailure(err, phaseStatement), false
	}
	if err := validateStatementDescription(description, len(statement.parameterValues)); err != nil {
		return rowRead{}, classifiedFailure{disposition: dispositionDefect, kind: sdkgo.FailureValidation, message: err.Error()}, false
	}
	read, err := readBoundedRows(ctx, connection, description, statement.parameterValues, client.maxRows, client.maxResponseBytes)
	if err != nil {
		return rowRead{}, classifyFailure(err, phaseStatement), false
	}
	if read.isTruncated {
		return rowRead{}, classifiedFailure{
			disposition: dispositionLimitExceeded, kind: sdkgo.FailureResponseTooLarge,
			message: "RETURNING rows exceeded maxRows or maxResponseBytes, so the transaction was rolled back",
		}, false
	}
	if maxRowsAffected != nil && read.commandTag.RowsAffected() > *maxRowsAffected {
		return rowRead{}, classifiedFailure{
			disposition: dispositionLimitExceeded, kind: sdkgo.FailureConflict,
			message: fmt.Sprintf("the statement affected %d rows but maxRowsAffected is %d, so the transaction was rolled back", read.commandTag.RowsAffected(), *maxRowsAffected),
		}, false
	}
	return read, classifiedFailure{}, true
}

// bindExecuteStatement validates the input and appends the idempotency key as the last placeholder.
func bindExecuteStatement(call sdkgo.Call, input ExecuteStatementInput) (boundStatement, error) {
	statement, err := bindStatement(input.Statement, input.Parameters)
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
		statement.parameterValues = append(statement.parameterValues, []byte(call.IdempotencyKey))
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

func newStatementExecution(read rowRead) StatementExecution {
	return StatementExecution{
		Command: commandName(read.commandTag.String()), RowsAffected: read.commandTag.RowsAffected(),
		Columns: read.columns, Rows: read.rows,
	}
}

func emptyStatementExecution() StatementExecution {
	return StatementExecution{Columns: []Column{}, Rows: []map[string]any{}}
}

// commandName drops the trailing counts from a command tag, so "INSERT 0 1" becomes "INSERT".
func commandName(commandTag string) string {
	words := strings.Fields(commandTag)
	for len(words) > 0 && isDecimalDigits(words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func isDecimalDigits(word string) bool {
	for _, character := range word {
		if character < '0' || character > '9' {
			return false
		}
	}
	return word != ""
}
