// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const cancelStatementOperationID = "cancelStatement"

// CancelStatementInput identifies the statement to cancel.
type CancelStatementInput struct {
	// StatementHandle is the handle submitStatement returned.
	StatementHandle string `json:"statementHandle"`
}

// StatementCancellation records that Snowflake accepted a cancellation.
type StatementCancellation struct {
	// StatementHandle is the statement Snowflake was asked to cancel.
	StatementHandle string `json:"statementHandle"`
}

// CancelStatementOperation implements the statement cancellation.
type CancelStatementOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CancelStatementOperation) Definition() sdkgo.MutationDefinition {
	return CancelStatementDefinition
}

// IdempotencyKey returns the stable connector Call ID, a UUID, which becomes Snowflake's requestId.
func (CancelStatementOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelStatementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v2/statements/{handle}/cancel.
//
// Canceling is safe to repeat, so a lost response, a 429, or a 5xx returns Retry and the operation
// never selects uncertain. A 200 selects canceled. A 422, such as Snowflake's code 000709 for an
// unknown handle, selects providerRejected with the code and SQLSTATE in the Receipt; Snowflake does
// not document whether canceling a finished statement answers 200 or 422.
func (operation CancelStatementOperation) Invoke(call sdkgo.Call, input CancelStatementInput) sdkgo.MutationAttempt[StatementCancellation] {
	client := operation.client
	if !statementHandlePattern.MatchString(input.StatementHandle) {
		return cancelAttemptForFailure(classifiedFailure{
			disposition: dispositionDefect, kind: sdkgo.FailureValidation,
			message: "statementHandle must be the UUID-shaped handle submitStatement returned",
		}, sdkgo.Receipt{})
	}
	query := url.Values{"requestId": {string(call.IdempotencyKey)}}
	target := statementsPath + "/" + input.StatementHandle + "/cancel?" + query.Encode()
	response, err := client.send(call, sqlAPIRequest{method: http.MethodPost, target: target, body: []byte("{}")})
	if err != nil {
		return cancelAttemptForFailure(requestFailure(err), sdkgo.Receipt{})
	}
	defer response.Body.Close()
	body := readErrorBody(response)
	if response.StatusCode == http.StatusOK {
		status := decodeStatementStatus(body)
		cancellation := StatementCancellation{StatementHandle: input.StatementHandle}
		return sdkgo.NewMutationBranch(CancelStatementBranchCanceled, cancellation, nil, client.receipt(call, input.StatementHandle, status))
	}
	failure := classifyUnsuccessfulResponse(response, body, client.now())
	return cancelAttemptForFailure(failure, client.receipt(call, input.StatementHandle, failure.status))
}

func cancelAttemptForFailure(failure classifiedFailure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[StatementCancellation] {
	switch failure.disposition {
	case dispositionRetry, dispositionInvalidResponse:
		return sdkgo.NewMutationRetry[StatementCancellation](sdkFailure(cancelStatementOperationID, failure), failure.retryAfter)
	case dispositionDefect:
		return sdkgo.NewMutationBranch(CancelStatementBranchDefect, StatementCancellation{}, failurePointer(cancelStatementOperationID, failure), receipt)
	default:
		return sdkgo.NewMutationBranch(CancelStatementBranchProviderRejected, StatementCancellation{}, failurePointer(cancelStatementOperationID, failure), receipt)
	}
}
