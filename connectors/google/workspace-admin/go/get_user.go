// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetUserInput identifies one account to read.
type GetUserInput struct {
	// UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
	UserKey string `json:"userKey"`
}

// GetUserOperation implements the getUser connector Query.
type GetUserOperation struct{ client *Client }

var getUserFailureBranches = failureBranches{
	notFound: GetUserBranchNotFound, conflict: GetUserBranchProviderRejected, rejected: GetUserBranchProviderRejected,
	invalidResponse: GetUserBranchInvalidResponse, defect: GetUserBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads the account's administrator view. A missing account selects notFound.
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
	read := operation.client.readUser(call, &credential, operationID, userKey)
	if read.exchange.outcome != exchangeSucceeded {
		return queryAttemptFromExchange[User](operation.client, call, read.exchange, getUserFailureBranches)
	}
	return sdkgo.NewQueryBranch(GetUserBranchFound, read.user, nil, operation.client.receipt(call, read.exchange.response, read.user.ID))
}
