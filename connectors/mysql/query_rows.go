// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"database/sql/driver"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const queryOperationID = "query"

// QueryRowsInput is one parameterized read statement.
type QueryRowsInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "SELECT id, plan FROM subscriptions WHERE account_email = ?". Values belong in Parameters: the
	// connector never formats them into SQL text, and it cannot detect a statement built by
	// concatenating untrusted input. Only SELECT, WITH, TABLE, VALUES, SHOW, EXPLAIN, DESCRIBE, and
	// DESC statements are accepted, because MySQL commits implicitly before DDL and would run it
	// outside the read-only transaction.
	Statement string `json:"statement"`
	// Parameters binds the ? placeholders in order. Each value is nil for SQL NULL, a string, bool,
	// integer, float, json.Number, json.RawMessage, []byte for binary data, or time.Time, and is sent
	// as a typed prepared-statement parameter. Pass exact decimals and integers beyond 2^53 as strings
	// or json.Number.
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
// Invoke opens one connection, pins the connector's session settings, sends START TRANSACTION
// READ ONLY, prepares the statement, checks its placeholder count and column names, reads at most
// maxRows rows and maxResponseBytes, and closes the connection without committing. A write inside
// the statement fails with error 1792 and selects providerRejected. Transient connection, deadlock,
// lock-wait, and resource failures return Retry, because a read has no side effect to repeat.
func (operation QueryRowsOperation) Invoke(call sdkgo.Call, input QueryRowsInput) sdkgo.QueryAttempt[RowSet] {
	client := operation.client
	statement, err := bindStatement(input.Statement, input.Parameters, statementKindRead)
	if err != nil {
		return queryDefect(sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return queryDefect(sdkgo.FailureAuthentication, "MySQL connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if _, err := session.beginTransaction(ctx, true); err != nil {
		failure := session.classifyFailure(err, phaseBegin)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if err := validatePreparedStatement(prepared, len(statement.parameterValues)); err != nil {
		return queryDefect(sdkgo.FailureValidation, err.Error(), client.receipt(call, session, classifiedFailure{}))
	}
	queryer, isQueryer := prepared.(driver.StmtQueryContext)
	if !isQueryer {
		failure := session.classifyFailure(errDriverInterfaceMissing, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	rows, err := queryer.QueryContext(ctx, statement.parameterValues)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	rowSet := RowSet{Columns: read.columns, Rows: read.rows, Truncated: read.isTruncated}
	receipt := client.receipt(call, session, classifiedFailure{})
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
