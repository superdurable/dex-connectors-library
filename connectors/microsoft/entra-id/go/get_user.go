// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetUserInput identifies one account to read.
type GetUserInput struct {
	// UserKey is the account's object ID or its user principal name, such as ada.lovelace@contoso.com.
	UserKey string `json:"userKey"`
}

// GetUserOperation implements the getUser connector Query.
type GetUserOperation struct{ client *Client }

var getUserFailureBranches = failureBranches{
	notFound: GetUserBranchNotFound, rejected: GetUserBranchProviderRejected,
	invalidResponse: GetUserBranchInvalidResponse, defect: GetUserBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads the account's bounded properties. A missing account selects notFound.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[User] {
	const operationID = "getUser"
	userKey, err := validateUserKey(input.UserKey)
	if err != nil {
		return sdkgo.NewQueryBranch(GetUserBranchDefect, User{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetUserBranchDefect, User{}, failure, sdkgo.Receipt{})
	}
	exchange := operation.client.readUser(call, &credential, operationID, userKey, userSelectedProperties)
	if exchange.outcome != exchangeSucceeded {
		return queryAttemptFromExchange[User](operation.client, call, exchange, getUserFailureBranches)
	}
	receipt := operation.client.receipt(call, exchange.response, "")
	user, _, err := decodeUser(exchange.response.body, true)
	if err != nil {
		return sdkgo.NewQueryBranch(GetUserBranchInvalidResponse, User{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid account: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = user.ID
	return sdkgo.NewQueryBranch(GetUserBranchFound, user, nil, receipt)
}
