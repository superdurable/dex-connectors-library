// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateAccountContactInput replaces a link's job title or account assignment.
type UpdateAccountContactInput struct {
	// AssociationID is the numeric link ID.
	AssociationID string `json:"associationId"`
	// AccountID moves the contact to this numeric account when supplied.
	AccountID *string `json:"accountId,omitempty"`
	// JobTitle replaces the contact's role; blank clears it.
	JobTitle *string `json:"jobTitle,omitempty"`
}

// DeleteAccountContactInput selects one link for removal.
type DeleteAccountContactInput struct {
	// AssociationID is the numeric link ID.
	AssociationID string `json:"associationId"`
}

// AccountContactDeletion confirms that a link is absent.
type AccountContactDeletion struct {
	// AssociationID is the link removed or already absent.
	AssociationID string `json:"associationId"`
	// Deleted is true when the link is absent.
	Deleted bool `json:"deleted"`
}

// UpdateAccountContactOperation replaces a link's absolute fields.
type UpdateAccountContactOperation struct{ client *Client }

// DeleteAccountContactOperation removes one link.
type DeleteAccountContactOperation struct{ client *Client }

// UpdateAccountContact binds the update Mutation.
func (client *Client) UpdateAccountContact() UpdateAccountContactOperation {
	return UpdateAccountContactOperation{client}
}

// DeleteAccountContact binds the delete Mutation.
func (client *Client) DeleteAccountContact() DeleteAccountContactOperation {
	return DeleteAccountContactOperation{client}
}

// Definition returns the manifest-generated update contract.
func (UpdateAccountContactOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAccountContactDefinition
}

// Definition returns the manifest-generated delete contract.
func (DeleteAccountContactOperation) Definition() sdkgo.MutationDefinition {
	return DeleteAccountContactDefinition
}

// IdempotencyKey identifies an absolute link replacement.
func (UpdateAccountContactOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAccountContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies a convergent link removal.
func (DeleteAccountContactOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAccountContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke changes a link's account or job title, leaving its contact identity stable.
func (operation UpdateAccountContactOperation) Invoke(call sdkgo.Call, input UpdateAccountContactInput) sdkgo.MutationAttempt[AccountContactAssociation] {
	if !numericID.MatchString(input.AssociationID) || input.AccountID == nil && input.JobTitle == nil || input.AccountID != nil && !numericID.MatchString(*input.AccountID) || input.JobTitle != nil && (len(*input.JobTitle) > 250 || strings.ContainsAny(*input.JobTitle, "\r\n")) {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchDefect, sdkgo.FailureValidation, "link ID or replacement field is invalid")
	}
	fields := map[string]string{}
	if input.AccountID != nil {
		fields["account"] = *input.AccountID
	}
	if input.JobTitle != nil {
		fields["jobTitle"] = *input.JobTitle
	}
	encoded, err := json.Marshal(map[string]any{"accountContact": fields})
	if err != nil {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchDefect, sdkgo.FailureLocalDefect, "link update could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/accountContacts/"+input.AssociationID, encoded)
	if errors.Is(err, errCredential) {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("updateAccountContact", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("updateAccountContact", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("updateAccountContact", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchNotFound, sdkgo.FailureNotFound, "link does not exist")
	}
	if status != http.StatusOK {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[AccountContactAssociation](queryFailure("updateAccountContact", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	link, err := decodeAccountContact(body)
	if err != nil || link.ID != input.AssociationID {
		return accountContactMutationBranch("updateAccountContact", UpdateAccountContactBranchInvalidResponse, sdkgo.FailureProtocol, "link response invalid")
	}
	return sdkgo.NewMutationBranch(UpdateAccountContactBranchUpdated, link, nil, dealReceipt(call, link.ID))
}

// Invoke removes one link while preserving its contact and account records.
func (operation DeleteAccountContactOperation) Invoke(call sdkgo.Call, input DeleteAccountContactInput) sdkgo.MutationAttempt[AccountContactDeletion] {
	if !numericID.MatchString(input.AssociationID) {
		return accountContactDeletionBranch(DeleteAccountContactBranchDefect, sdkgo.FailureValidation, "link ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/accountContacts/"+input.AssociationID, nil)
	if errors.Is(err, errCredential) {
		return accountContactDeletionBranch(DeleteAccountContactBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[AccountContactDeletion](queryFailure("deleteAccountContact", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AccountContactDeletion](queryFailure("deleteAccountContact", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[AccountContactDeletion](queryFailure("deleteAccountContact", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return accountContactDeletionBranch(DeleteAccountContactBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountContactDeletionBranch(DeleteAccountContactBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[AccountContactDeletion](queryFailure("deleteAccountContact", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	if status == http.StatusOK && len(body) > 0 && !json.Valid(body) {
		return accountContactDeletionBranch(DeleteAccountContactBranchInvalidResponse, sdkgo.FailureProtocol, "invalid response JSON")
	}
	return sdkgo.NewMutationBranch(DeleteAccountContactBranchDeleted, AccountContactDeletion{AssociationID: input.AssociationID, Deleted: true}, nil, dealReceipt(call, input.AssociationID))
}

func accountContactMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountContactAssociation] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, AccountContactAssociation{}, &failure, sdkgo.Receipt{})
}
func accountContactDeletionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountContactDeletion] {
	failure := queryFailure("deleteAccountContact", kind, message)
	return sdkgo.NewMutationBranch(branch, AccountContactDeletion{}, &failure, sdkgo.Receipt{})
}
