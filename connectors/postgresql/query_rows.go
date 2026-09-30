// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const queryOperationID = "query"

// QueryRowsInput is one parameterized read statement.
type QueryRowsInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "SELECT id, plan FROM subscriptions WHERE account_email = $1". Values belong in Parameters:
	// the connector never formats them into SQL text, and it cannot detect a statement built by
	// concatenating untrusted input. Transaction control and COPY are rejected before any request.
	Statement string `json:"statement"`
	// Parameters binds $1 through $n in order. Each value is nil for SQL NULL, a string, bool,
	// integer, float, json.Number, json.RawMessage, []byte for bytea, or time.Time, and is sent as
	// text that PostgreSQL parses for the type it infers for that placeholder. Pass exact decimals
	// and integers beyond 2^53 as strings or json.Number.
	Parameters []any `json:"parameters,omitempty"`
}

// RowSet is the bounded result of one QueryRows statement.
type RowSet struct {
	// Columns lists the result columns in statement order.
	Columns []Column `json:"columns"`
	// Rows maps each column name to its JSON value using the connector's documented type mapping.
	// It is empty, never nil, when the statement returned no rows.
	Rows []map[string]any `json:"rows"`
	// Truncated reports that the statement produced more rows or bytes than maxRows or
	// maxResponseBytes allow, so Rows holds only the rows that fit.
	Truncated bool `json:"truncated,omitempty"`
}

// QueryRowsOperation implements the read-only query operation.
type QueryRowsOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (QueryRowsOperation) Definition() sdkgo.QueryDefinition {
	return QueryRowsDefinition
}

// Invoke runs one statement inside a read-only transaction and returns its bounded rows.
//
// Invoke opens one connection, sends BEGIN READ ONLY with the connector's session settings,
// prepares the statement, checks its placeholder count and column names, reads at most maxRows
// rows and maxResponseBytes, and closes the connection without committing. A write inside the
// statement fails with SQLSTATE 25006 and selects providerRejected. Transient connection,
// serialization, and resource failures return Retry, because a read has no side effect to repeat.
func (operation QueryRowsOperation) Invoke(call sdkgo.Call, input QueryRowsInput) sdkgo.QueryAttempt[RowSet] {
	client := operation.client
	statement, err := bindStatement(input.Statement, input.Parameters)
	if err != nil {
		return queryDefect(sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return queryDefect(sdkgo.FailureAuthentication, "PostgreSQL connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	connection, err := client.connect(ctx, password)
	if err != nil {
		failure := classifyFailure(err, phaseConnect)
		return queryAttemptForFailure(failure, client.receipt(call, nil, failure))
	}
	defer client.closeConnection(connection)
	if err := client.beginTransaction(ctx, connection, transactionAccessReadOnly); err != nil {
		failure := classifyFailure(err, phaseBegin)
		return queryAttemptForFailure(failure, client.receipt(call, connection, failure))
	}
	description, err := connection.Prepare(ctx, "", statement.sql, nil)
	if err != nil {
		failure := classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, connection, failure))
	}
	if err := validateStatementDescription(description, len(statement.parameterValues)); err != nil {
		return queryDefect(sdkgo.FailureValidation, err.Error(), client.receipt(call, connection, classifiedFailure{}))
	}
	read, err := readBoundedRows(ctx, connection, description, statement.parameterValues, client.maxRows, client.maxResponseBytes)
	if err != nil {
		failure := classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, connection, failure))
	}
	rowSet := RowSet{Columns: read.columns, Rows: read.rows, Truncated: read.isTruncated}
	receipt := client.receipt(call, connection, classifiedFailure{})
	if read.isTruncated {
		failure := classifiedFailure{
			kind:    sdkgo.FailureResponseTooLarge,
			message: "the statement returned more rows or bytes than maxRows or maxResponseBytes allow",
		}
		return sdkgo.NewQueryBranch(QueryRowsBranchTruncated, rowSet, failurePointer(queryOperationID, failure), receipt)
	}
	return sdkgo.NewQueryBranch(QueryRowsBranchCompleted, rowSet, nil, receipt)
}

func queryAttemptForFailure(failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.QueryAttempt[RowSet] {
	switch failure.disposition {
	case dispositionRetry, dispositionUncertain:
		return sdkgo.NewQueryRetry[RowSet](sdkFailure(queryOperationID, failure), 0)
	case dispositionInvalidResponse:
		return sdkgo.NewQueryBranch(QueryRowsBranchInvalidResponse, emptyRowSet(), failurePointer(queryOperationID, failure), receipt)
	case dispositionDefect:
		return sdkgo.NewQueryBranch(QueryRowsBranchDefect, emptyRowSet(), failurePointer(queryOperationID, failure), receipt)
	default:
		return sdkgo.NewQueryBranch(QueryRowsBranchProviderRejected, emptyRowSet(), failurePointer(queryOperationID, failure), receipt)
	}
}

func queryDefect(kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.QueryAttempt[RowSet] {
	failure := classifiedFailure{disposition: dispositionDefect, kind: kind, message: message}
	return sdkgo.NewQueryBranch(QueryRowsBranchDefect, emptyRowSet(), failurePointer(queryOperationID, failure), receipt)
}

func emptyRowSet() RowSet {
	return RowSet{Columns: []Column{}, Rows: []map[string]any{}}
}
