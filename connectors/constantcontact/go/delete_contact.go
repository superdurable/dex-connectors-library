// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteContactInput selects a contact to remove from all lists.
type DeleteContactInput struct {
	// ContactID is the non-secret V3 contact UUID.
	ContactID string `json:"contactId"`
}

// DeleteContactOutput identifies the removed contact.
type DeleteContactOutput struct {
	// ContactID is the requested V3 contact UUID.
	ContactID string `json:"contactId"`
}

// DeleteContactOperation removes a contact from Constant Contact.
type DeleteContactOperation struct{ client *Client }

// DeleteContact binds the Mutation to this client.
func (client *Client) DeleteContact() DeleteContactOperation { return DeleteContactOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteContactOperation) Definition() sdkgo.MutationDefinition { return DeleteContactDefinition }

// IdempotencyKey correlates attempts; deleting the same contact twice has no additional effect.
func (DeleteContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one contact and classifies definitive and temporary outcomes.
func (operation DeleteContactOperation) Invoke(call sdkgo.Call, input DeleteContactInput) sdkgo.MutationAttempt[DeleteContactOutput] {
	if !contactIDPattern.MatchString(input.ContactID) {
		return deleteContactBranch(DeleteContactBranchDefect, sdkgo.FailureValidation, "contact ID must be a UUID")
	}
	response := operation.client.send(call, http.MethodDelete, "/contacts/"+input.ContactID, nil)
	if errors.Is(response.err, errCredential) {
		return deleteContactBranch(DeleteContactBranchDefect, sdkgo.FailureAuthentication, "Constant Contact credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) {
		return sdkgo.NewMutationRetry[DeleteContactOutput](deleteContactFailure(sdkgo.FailureAvailability, "Constant Contact credentials could not be refreshed"), 0)
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeleteContactOutput](deleteContactFailure(sdkgo.FailureTransport, "Constant Contact request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DeleteContactOutput](deleteContactFailure(sdkgo.FailureRateLimit, "Constant Contact rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeleteContactOutput](deleteContactFailure(sdkgo.FailureAvailability, "Constant Contact is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return deleteContactBranch(DeleteContactBranchNotFound, sdkgo.FailureNotFound, "Constant Contact did not find the contact")
	}
	if response.status != http.StatusNoContent {
		return deleteContactBranch(DeleteContactBranchProviderRejected, sdkgo.FailureProviderRejection, "Constant Contact rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(DeleteContactBranchDeleted, DeleteContactOutput{ContactID: input.ContactID}, nil, sdkgo.Receipt{})
}

func deleteContactFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "constantcontact", Operation: "deleteContact", Message: message}
}

func deleteContactBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeleteContactOutput] {
	failure := deleteContactFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, DeleteContactOutput{}, &failure, sdkgo.Receipt{})
}
