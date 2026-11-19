// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const deleteEmailIdentityOperationID = "deleteEmailIdentity"

// DeleteEmailIdentityInput names one email or domain identity to remove.
type DeleteEmailIdentityInput struct {
	// EmailIdentity is the exact identity shown under SES Verified identities.
	EmailIdentity string `json:"emailIdentity"`
}

// DeletedEmailIdentity identifies the identity SES removed.
type DeletedEmailIdentity struct {
	// Name is the email address or domain.
	Name string `json:"name"`
}

// DeleteEmailIdentityOperation implements one regional identity deletion.
type DeleteEmailIdentityOperation struct{ client *Client }

// DeleteEmailIdentity binds this Mutation to the client.
func (client *Client) DeleteEmailIdentity() DeleteEmailIdentityOperation {
	return DeleteEmailIdentityOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteEmailIdentityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteEmailIdentityDefinition
}

// IdempotencyKey derives a stable key for repeated attempts of this Step.
func (DeleteEmailIdentityOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEmailIdentityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a signed deletion. Missing identities are treated as already deleted.
func (operation DeleteEmailIdentityOperation) Invoke(call sdkgo.Call, input DeleteEmailIdentityInput) sdkgo.MutationAttempt[DeletedEmailIdentity] {
	if !emailIdentityPattern.MatchString(input.EmailIdentity) {
		return deleteIdentityBranch(DeleteEmailIdentityBranchDefect, sdkgo.FailureValidation, "SES email identity name is invalid", DeletedEmailIdentity{})
	}
	deleted := DeletedEmailIdentity{Name: input.EmailIdentity}
	response := operation.client.request(call, http.MethodDelete, "/v2/email/identities/"+url.PathEscape(input.EmailIdentity), nil)
	if errors.Is(response.err, errCredential) {
		return deleteIdentityBranch(DeleteEmailIdentityBranchDefect, sdkgo.FailureAuthentication, "SES credentials are unavailable or invalid", deleted)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedEmailIdentity](sesFailure(deleteEmailIdentityOperationID, sdkgo.FailureTransport, "SES identity deletion failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[DeletedEmailIdentity](sesFailure(deleteEmailIdentityOperationID, kind, "SES identity deletion is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNoContent || response.status == http.StatusOK || response.status == http.StatusNotFound {
		if response.err != nil {
			return sdkgo.NewMutationRetry[DeletedEmailIdentity](sesFailure(deleteEmailIdentityOperationID, sdkgo.FailureTransport, "SES identity deletion response was interrupted"), 0)
		}
		return sdkgo.NewMutationBranch(DeleteEmailIdentityBranchDeleted, deleted, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-ses", ProviderObjectID: input.EmailIdentity})
	}
	return deleteIdentityBranch(DeleteEmailIdentityBranchProviderRejected, sdkgo.FailureProviderRejection, "SES rejected identity deletion HTTP "+strconv.Itoa(response.status), deleted)
}

func deleteIdentityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, value DeletedEmailIdentity) sdkgo.MutationAttempt[DeletedEmailIdentity] {
	failure := sesFailure(deleteEmailIdentityOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
