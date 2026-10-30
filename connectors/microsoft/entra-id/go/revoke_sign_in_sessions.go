// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RevokeSignInSessionsInput identifies one account whose sign-in sessions to revoke.
type RevokeSignInSessionsInput struct {
	// UserKey is the account's object ID or its user principal name.
	UserKey string `json:"userKey"`
}

// RevokeSignInSessionsOutput confirms that Microsoft accepted the revocation.
type RevokeSignInSessionsOutput struct {
	// UserKey is the user key the Step requested.
	UserKey string `json:"userKey"`
}

// RevokeSignInSessionsOperation implements the revokeSignInSessions connector Mutation.
type RevokeSignInSessionsOperation struct{ client *Client }

type revokeSignInSessionsResponse struct {
	Value *bool `json:"value"`
}

var revokeSignInSessionsFailureBranches = failureBranches{
	notFound: RevokeSignInSessionsBranchNotFound, rejected: RevokeSignInSessionsBranchProviderRejected,
	invalidResponse: RevokeSignInSessionsBranchInvalidResponse, defect: RevokeSignInSessionsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (RevokeSignInSessionsOperation) Definition() sdkgo.MutationDefinition {
	return RevokeSignInSessionsDefinition
}

// IdempotencyKey returns the Call ID; a repeated revocation only resets the cutoff time again.
func (RevokeSignInSessionsOperation) IdempotencyKey(callID sdkgo.CallID, _ RevokeSignInSessionsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke resets the account's signInSessionsValidFromDateTime to now, which invalidates every
// refresh token and session cookie issued before it. Microsoft documents a 2xx answer and a
// delay of a few minutes before tokens stop working; it does not revoke an external user's
// sessions, which their home tenant owns. A lost response is retried, because a repeated
// revocation is harmless.
func (operation RevokeSignInSessionsOperation) Invoke(call sdkgo.Call, input RevokeSignInSessionsInput) sdkgo.MutationAttempt[RevokeSignInSessionsOutput] {
	const operationID = "revokeSignInSessions"
	userKey, err := validateUserKey(input.UserKey)
	if err != nil {
		return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchDefect, RevokeSignInSessionsOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchDefect, RevokeSignInSessionsOutput{}, failure, sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, graphRequest{method: http.MethodPost, path: userPath(userKey) + "/revokeSignInSessions"})
	if exchange.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[RevokeSignInSessionsOutput](operation.client, call, exchange, revokeSignInSessionsFailureBranches)
	}
	receipt := operation.client.receipt(call, exchange.response, "")
	if objectIDPattern.MatchString(userKey) {
		receipt.ProviderObjectID = userKey
	}
	output := RevokeSignInSessionsOutput{UserKey: userKey}
	if len(bytes.TrimSpace(exchange.response.body)) == 0 {
		return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchRevoked, output, nil, receipt)
	}
	var response revokeSignInSessionsResponse
	if json.Unmarshal(exchange.response.body, &response) != nil {
		return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchInvalidResponse, output,
			failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid revocation response"), receipt)
	}
	if response.Value != nil && !*response.Value {
		return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchProviderRejected, output,
			failurePointer(sdkgo.FailureProviderRejection, operationID, "Microsoft Graph reported that it did not revoke the sessions"), receipt)
	}
	return sdkgo.NewMutationBranch(RevokeSignInSessionsBranchRevoked, output, nil, receipt)
}
