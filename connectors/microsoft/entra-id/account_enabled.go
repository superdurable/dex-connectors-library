// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DisableUserInput identifies one account whose sign-in to block.
type DisableUserInput struct {
	// UserKey is the account's object ID or its user principal name.
	UserKey string `json:"userKey"`
}

// EnableUserInput identifies one account whose sign-in to restore.
type EnableUserInput struct {
	// UserKey is the account's object ID or its user principal name.
	UserKey string `json:"userKey"`
}

// DisableUserOperation implements the disableUser connector Mutation.
type DisableUserOperation struct{ client *Client }

// EnableUserOperation implements the enableUser connector Mutation.
type EnableUserOperation struct{ client *Client }

// accountEnabledBranches names one accountEnabled operation's branches.
type accountEnabledBranches struct {
	operationID string
	applied     sdkgo.BranchID
	failure     failureBranches
}

var disableUserBranches = accountEnabledBranches{
	operationID: "disableUser", applied: DisableUserBranchDisabled,
	failure: failureBranches{
		notFound: DisableUserBranchNotFound, rejected: DisableUserBranchProviderRejected,
		invalidResponse: DisableUserBranchInvalidResponse, defect: DisableUserBranchDefect,
	},
}

var enableUserBranches = accountEnabledBranches{
	operationID: "enableUser", applied: EnableUserBranchEnabled,
	failure: failureBranches{
		notFound: EnableUserBranchNotFound, rejected: EnableUserBranchProviderRejected,
		invalidResponse: EnableUserBranchInvalidResponse, defect: EnableUserBranchDefect,
	},
}

// Definition returns the immutable connector operation definition.
func (DisableUserOperation) Definition() sdkgo.MutationDefinition { return DisableUserDefinition }

// IdempotencyKey returns the Call ID; setting an absolute accountEnabled value is safe to repeat.
func (DisableUserOperation) IdempotencyKey(callID sdkgo.CallID, _ DisableUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets accountEnabled to false and reads the account back. Repeating it on a disabled account changes nothing.
func (operation DisableUserOperation) Invoke(call sdkgo.Call, input DisableUserInput) sdkgo.MutationAttempt[User] {
	return setAccountEnabled(operation.client, call, input.UserKey, false, disableUserBranches)
}

// Definition returns the immutable connector operation definition.
func (EnableUserOperation) Definition() sdkgo.MutationDefinition { return EnableUserDefinition }

// IdempotencyKey returns the Call ID; setting an absolute accountEnabled value is safe to repeat.
func (EnableUserOperation) IdempotencyKey(callID sdkgo.CallID, _ EnableUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets accountEnabled to true and reads the account back. Repeating it on an enabled account changes nothing.
func (operation EnableUserOperation) Invoke(call sdkgo.Call, input EnableUserInput) sdkgo.MutationAttempt[User] {
	return setAccountEnabled(operation.client, call, input.UserKey, true, enableUserBranches)
}

// setAccountEnabled patches only accountEnabled, then reads back; a stale read-back is replication lag, retried within the window.
func setAccountEnabled(client *Client, call sdkgo.Call, rawUserKey string, isEnabled bool, branches accountEnabledBranches) sdkgo.MutationAttempt[User] {
	userKey, err := validateUserKey(rawUserKey)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.failure.defect, User{}, failurePointer(sdkgo.FailureValidation, branches.operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, branches.operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(branches.failure.defect, User{}, failure, sdkgo.Receipt{})
	}
	written := client.exchange(call, &credential, branches.operationID, graphRequest{
		method: http.MethodPatch, path: userPath(userKey), payload: map[string]bool{"accountEnabled": isEnabled},
	})
	if written.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[User](client, call, written, branches.failure)
	}
	read := client.readUser(call, &credential, branches.operationID, userKey, userSelectedProperties)
	if read.outcome == exchangeNotFound && client.isWithinPropagationWindow(call) {
		return sdkgo.NewMutationRetry[User](graphFailure(sdkgo.FailureAvailability, branches.operationID, "Microsoft Graph accepted the change but does not return the account yet"), 0)
	}
	if read.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[User](client, call, read, branches.failure)
	}
	receipt := client.receipt(call, read.response, "")
	user, _, err := decodeUser(read.response.body, true)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.failure.invalidResponse, User{}, failurePointer(sdkgo.FailureProtocol, branches.operationID, "provider returned an invalid account: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = user.ID
	if user.IsAccountEnabled != isEnabled {
		if client.isWithinPropagationWindow(call) {
			return sdkgo.NewMutationRetry[User](graphFailure(sdkgo.FailureAvailability, branches.operationID, "Microsoft Graph does not report the change yet"), 0)
		}
		return sdkgo.NewMutationBranch(branches.failure.rejected, user,
			failurePointer(sdkgo.FailureProviderRejection, branches.operationID, "Microsoft Graph returned the account without the requested accountEnabled value"), receipt)
	}
	return sdkgo.NewMutationBranch(branches.applied, user, nil, receipt)
}
