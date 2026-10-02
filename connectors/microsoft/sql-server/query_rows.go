// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const queryOperationID = "query"

// QueryRowsInput is one parameterized read statement.
type QueryRowsInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "SELECT id, plan FROM dbo.subscriptions WHERE account_email = @p1". Values belong in
	// Parameters: the connector never formats them into SQL text, and it cannot detect a statement
	// built by concatenating untrusted input. Only statements that start with SELECT or WITH are
	// accepted, and the statement must return one result set.
	Statement string `json:"statement"`
	// Parameters binds @p1 through @pN in order. Each value is nil for SQL NULL, a string, bool,
	// integer, float, json.Number, json.RawMessage, []byte for binary data, or time.Time, and is sent
	// as a typed sp_executesql parameter. Pass exact decimals as strings or json.Number and CAST them
	// in the statement when the comparison type matters.
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

// QueryRowsOperation implements the read query operation.
type QueryRowsOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (QueryRowsOperation) Definition() sdkgo.QueryDefinition {
	return QueryRowsDefinition
}

// Invoke runs one statement inside a transaction that is always rolled back and returns its bounded rows.
//
// Invoke opens one connection, pins the connector's session settings, begins a transaction, runs
// the statement, reads at most maxRows rows and maxResponseBytes, and closes the connection
// without committing, so the server rolls back anything the statement changed. Transient
// connection, deadlock, lock-timeout, and resource failures return Retry, because a read has no
// side effect to repeat.
func (operation QueryRowsOperation) Invoke(call sdkgo.Call, input QueryRowsInput) sdkgo.QueryAttempt[RowSet] {
	client := operation.client
	statement, err := bindStatement(input.Statement, input.Parameters, len(input.Parameters), statementKindRead)
	if err != nil {
		return queryDefect(sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	password, isResolved := client.resolvePassword(call)
	if !isResolved {
		return queryDefect(sdkgo.FailureAuthentication, "SQL Server connection credentials are unavailable", sdkgo.Receipt{})
	}
	ctx, cancel := client.newCallContext(call)
	defer cancel()
	session, phase, err := client.openSession(ctx, password)
	defer session.close()
	if err != nil {
		failure := session.classifyFailure(err, phase)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if _, err := session.beginTransaction(ctx); err != nil {
		failure := session.classifyFailure(err, phaseBegin)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	prepared, err := session.prepareStatement(ctx, statement.sql)
	if err != nil {
		failure := session.classifyFailure(err, phaseStatement)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	statementCtx, cancelStatement := client.newStatementContext(ctx)
	defer cancelStatement()
	rows, err := session.queryStatement(statementCtx, prepared, statement.parameterValues)
	if err != nil {
		failure := session.classifyStatementFailure(err, statementCtx, ctx)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	read, err := readBoundedRows(rows, session.meter, client.maxRows, client.maxResponseBytes)
	if err != nil {
		failure := session.classifyStatementFailure(err, statementCtx, ctx)
		return queryAttemptForFailure(failure, client.receipt(call, session, failure))
	}
	if len(read.columns) == 0 {
		return queryDefect(sdkgo.FailureValidation, "the statement returned no result set; query runs only a SELECT, and anything else it changed was rolled back",
			client.receipt(call, session, classifiedFailure{}))
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
