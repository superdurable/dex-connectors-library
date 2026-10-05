// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const submitStatementOperationID = "submitStatement"

// SubmitStatementInput is one parameterized SQL statement.
type SubmitStatementInput struct {
	// Statement is one SQL statement written as a constant in application code, such as
	// "SELECT COUNT(*) AS EVENT_COUNT FROM USAGE_EVENTS WHERE ACCOUNT_ID = ?". Values belong in
	// Parameters: the connector never formats them into SQL text, and it cannot detect a statement
	// built by concatenating untrusted input. Snowflake runs exactly one statement per request, so
	// several statements, BEGIN, COMMIT, USE, and ALTER SESSION are rejected by Snowflake.
	Statement string `json:"statement"`
	// Parameters binds each ? placeholder in order, outside string literals, quoted identifiers, and
	// comments. Each value is nil for SQL NULL, a string (TEXT), bool (BOOLEAN), integer (FIXED),
	// finite float (REAL), json.Number (FIXED for integers, otherwise TEXT), json.RawMessage (TEXT;
	// wrap the placeholder in PARSE_JSON), []byte (BINARY), or time.Time (TIMESTAMP_TZ). Cast a
	// placeholder when the type matters, such as ?::NUMBER(12,2).
	Parameters []any `json:"parameters,omitempty"`
}

// StatementSubmission identifies a statement Snowflake accepted for asynchronous execution.
type StatementSubmission struct {
	// StatementHandle is Snowflake's UUID-shaped handle, which getStatementResult and cancelStatement take.
	StatementHandle string `json:"statementHandle"`
	// SubmittedAt is when Snowflake created the statement, when it reported that time.
	SubmittedAt *time.Time `json:"submittedAt,omitempty"`
}

// statementRequestBody is the documented body of POST /api/v2/statements.
type statementRequestBody struct {
	Statement  string                      `json:"statement"`
	Timeout    int64                       `json:"timeout"`
	Database   string                      `json:"database,omitempty"`
	Schema     string                      `json:"schema,omitempty"`
	Warehouse  string                      `json:"warehouse,omitempty"`
	Role       string                      `json:"role,omitempty"`
	Bindings   map[string]statementBinding `json:"bindings,omitempty"`
	Parameters map[string]string           `json:"parameters"`
}

// SubmitStatementOperation implements the asynchronous statement submission.
type SubmitStatementOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SubmitStatementOperation) Definition() sdkgo.MutationDefinition {
	return SubmitStatementDefinition
}

// IdempotencyKey returns the stable connector Call ID, a UUID, which becomes Snowflake's requestId.
func (SubmitStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ SubmitStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits the statement with async=true and returns its handle without waiting for it.
//
// Every attempt of one Step execution sends the same requestId with retry=true. Snowflake documents
// that it then does not run a statement that already executed successfully; that it also returns a
// statement still queued or running is assumed and unverified. On that basis a lost response, a 429,
// or a 5xx returns Retry, and the operation never selects uncertain. The statement runs with the connection's warehouse, role,
// database, schema, and statementTimeoutSeconds, and with MULTI_STATEMENT_COUNT set to 1.
func (operation SubmitStatementOperation) Invoke(call sdkgo.Call, input SubmitStatementInput) sdkgo.MutationAttempt[StatementSubmission] {
	client := operation.client
	bindings, err := bindStatement(input.Statement, input.Parameters)
	if err != nil {
		return submitDefect(sdkgo.FailureValidation, err.Error())
	}
	if !statementHandlePattern.MatchString(string(call.IdempotencyKey)) {
		return submitDefect(sdkgo.FailureLocalDefect, "the Step's idempotency key is not a UUID, so it cannot be Snowflake's requestId")
	}
	body, err := json.Marshal(statementRequestBody{
		Statement: input.Statement, Timeout: client.statementTimeoutSeconds,
		Database: client.database, Schema: client.schema, Warehouse: client.warehouse, Role: client.role,
		Bindings: bindings, Parameters: map[string]string{"MULTI_STATEMENT_COUNT": "1"},
	})
	if err != nil {
		return submitDefect(sdkgo.FailureLocalDefect, "the statement request body could not be encoded")
	}
	query := url.Values{"requestId": {string(call.IdempotencyKey)}, "retry": {"true"}, "async": {"true"}}
	response, err := client.send(call, sqlAPIRequest{method: http.MethodPost, target: statementsPath + "?" + query.Encode(), body: body})
	if err != nil {
		return submitAttemptForFailure(requestFailure(err), sdkgo.Receipt{})
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		// A 200 can carry the first result partition, so the body is streamed and its rows skipped.
		decoded, err := decodeStatementResponse(response, rowRetention{})
		if err != nil || decoded.status.StatementHandle == "" {
			// The statement may exist; the same requestId with retry=true returns its handle on the next attempt.
			return sdkgo.NewMutationRetry[StatementSubmission](sdkFailure(submitStatementOperationID,
				invalidResponseFailure("Snowflake accepted the request but its response held no readable statement handle")), 0)
		}
		status := decoded.status
		submission := StatementSubmission{StatementHandle: status.StatementHandle, SubmittedAt: createdOnTime(status.CreatedOn)}
		return sdkgo.NewMutationBranch(SubmitStatementBranchSubmitted, submission, nil, client.receipt(call, status.StatementHandle, status))
	default:
		failure := classifyUnsuccessfulResponse(response, readErrorBody(response), client.now())
		return submitAttemptForFailure(failure, client.receipt(call, failure.status.StatementHandle, failure.status))
	}
}

func submitAttemptForFailure(failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementSubmission] {
	switch failure.disposition {
	case dispositionRetry, dispositionInvalidResponse:
		return sdkgo.NewMutationRetry[StatementSubmission](sdkFailure(submitStatementOperationID, failure), failure.retryAfter)
	case dispositionDefect:
		return sdkgo.NewMutationBranch(SubmitStatementBranchDefect, StatementSubmission{}, failurePointer(submitStatementOperationID, failure), receipt)
	default:
		return sdkgo.NewMutationBranch(SubmitStatementBranchProviderRejected, StatementSubmission{}, failurePointer(submitStatementOperationID, failure), receipt)
	}
}

func submitDefect(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[StatementSubmission] {
	failure := classifiedFailure{disposition: dispositionDefect, kind: kind, message: message}
	return submitAttemptForFailure(failure, sdkgo.Receipt{})
}
