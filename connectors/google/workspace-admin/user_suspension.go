// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SuspendUserInput identifies one account to suspend.
type SuspendUserInput struct {
	// UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
	UserKey string `json:"userKey"`
}

// UnsuspendUserInput identifies one account whose sign-in to restore.
type UnsuspendUserInput struct {
	// UserKey is the account's primary email address, an alias address, or its numeric unique user ID.
	UserKey string `json:"userKey"`
}

// SuspendUserOperation implements the suspendUser connector Mutation.
type SuspendUserOperation struct{ client *Client }

// UnsuspendUserOperation implements the unsuspendUser connector Mutation.
type UnsuspendUserOperation struct{ client *Client }

// suspensionBranches names one suspension operation's branches.
type suspensionBranches struct {
	operationID string
	applied     sdkgo.BranchID
	failure     failureBranches
}

var suspendUserBranches = suspensionBranches{
	operationID: "suspendUser", applied: SuspendUserBranchSuspended,
	failure: failureBranches{
		notFound: SuspendUserBranchNotFound, conflict: SuspendUserBranchProviderRejected, rejected: SuspendUserBranchProviderRejected,
		invalidResponse: SuspendUserBranchInvalidResponse, defect: SuspendUserBranchDefect,
	},
}

var unsuspendUserBranches = suspensionBranches{
	operationID: "unsuspendUser", applied: UnsuspendUserBranchUnsuspended,
	failure: failureBranches{
		notFound: UnsuspendUserBranchNotFound, conflict: UnsuspendUserBranchProviderRejected, rejected: UnsuspendUserBranchProviderRejected,
		invalidResponse: UnsuspendUserBranchInvalidResponse, defect: UnsuspendUserBranchDefect,
	},
}

// Definition returns the immutable connector operation definition.
func (SuspendUserOperation) Definition() sdkgo.MutationDefinition { return SuspendUserDefinition }

// IdempotencyKey returns the Call ID; setting an absolute suspended flag is safe to repeat.
func (SuspendUserOperation) IdempotencyKey(callID sdkgo.CallID, _ SuspendUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the account's suspended flag to true. Repeating it on a suspended account changes nothing.
func (operation SuspendUserOperation) Invoke(call sdkgo.Call, input SuspendUserInput) sdkgo.MutationAttempt[User] {
	return setUserSuspension(operation.client, call, input.UserKey, true, suspendUserBranches)
}

// Definition returns the immutable connector operation definition.
func (UnsuspendUserOperation) Definition() sdkgo.MutationDefinition { return UnsuspendUserDefinition }

// IdempotencyKey returns the Call ID; setting an absolute suspended flag is safe to repeat.
func (UnsuspendUserOperation) IdempotencyKey(callID sdkgo.CallID, _ UnsuspendUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the account's suspended flag to false. Repeating it on an active account changes nothing.
func (operation UnsuspendUserOperation) Invoke(call sdkgo.Call, input UnsuspendUserInput) sdkgo.MutationAttempt[User] {
	return setUserSuspension(operation.client, call, input.UserKey, false, unsuspendUserBranches)
}

// setUserSuspension writes an absolute flag; an account Google returns in the other state selects providerRejected.
func setUserSuspension(client *Client, call sdkgo.Call, rawUserKey string, isSuspended bool, branches suspensionBranches) sdkgo.MutationAttempt[User] {
	userKey, err := validateUserKey(rawUserKey)
	if err != nil {
		return sdkgo.NewMutationBranch(branches.failure.defect, User{}, failurePointer(sdkgo.FailureValidation, branches.operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, branches.operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(branches.failure.defect, User{}, failure, sdkgo.Receipt{})
	}
	written := client.writeUserSuspension(call, &credential, branches.operationID, userKey, isSuspended)
	if written.exchange.outcome != exchangeSucceeded {
		return mutationAttemptFromExchange[User](client, call, written.exchange, branches.failure)
	}
	receipt := client.receipt(call, written.exchange.response, written.user.ID)
	if written.user.IsSuspended != isSuspended {
		return sdkgo.NewMutationBranch(branches.failure.rejected, written.user,
			failurePointer(sdkgo.FailureProviderRejection, branches.operationID, "Google returned the account without the requested suspension state"), receipt)
	}
	return sdkgo.NewMutationBranch(branches.applied, written.user, nil, receipt)
}
