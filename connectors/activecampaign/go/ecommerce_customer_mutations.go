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

// UpdateEcommerceCustomerInput replaces a store customer's identity fields.
type UpdateEcommerceCustomerInput struct {
	// CustomerID is the numeric provider customer ID.
	CustomerID string `json:"customerId"`
	// ExternalID replaces the store customer ID when supplied.
	ExternalID *string `json:"externalId,omitempty"`
	// Email replaces the customer email when supplied.
	Email *string `json:"email,omitempty"`
}

// DeleteEcommerceCustomerInput selects one store customer for removal.
type DeleteEcommerceCustomerInput struct {
	// CustomerID is the numeric provider customer ID.
	CustomerID string `json:"customerId"`
}

// EcommerceCustomerDeletion confirms a Deep Data customer is absent.
type EcommerceCustomerDeletion struct {
	// CustomerID is the customer removed or already absent.
	CustomerID string `json:"customerId"`
	// Deleted is true when the customer is absent.
	Deleted bool `json:"deleted"`
}

// UpdateEcommerceCustomerOperation replaces selected customer fields.
type UpdateEcommerceCustomerOperation struct{ client *Client }

// DeleteEcommerceCustomerOperation removes one Deep Data customer.
type DeleteEcommerceCustomerOperation struct{ client *Client }

// UpdateEcommerceCustomer binds the update Mutation.
func (client *Client) UpdateEcommerceCustomer() UpdateEcommerceCustomerOperation {
	return UpdateEcommerceCustomerOperation{client}
}

// DeleteEcommerceCustomer binds the delete Mutation.
func (client *Client) DeleteEcommerceCustomer() DeleteEcommerceCustomerOperation {
	return DeleteEcommerceCustomerOperation{client}
}

// Definition returns the manifest-generated update contract.
func (UpdateEcommerceCustomerOperation) Definition() sdkgo.MutationDefinition {
	return UpdateEcommerceCustomerDefinition
}

// Definition returns the manifest-generated delete contract.
func (DeleteEcommerceCustomerOperation) Definition() sdkgo.MutationDefinition {
	return DeleteEcommerceCustomerDefinition
}

// IdempotencyKey identifies an absolute customer replacement.
func (UpdateEcommerceCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEcommerceCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies a convergent customer deletion.
func (DeleteEcommerceCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEcommerceCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a customer's external store ID or email by provider ID.
func (operation UpdateEcommerceCustomerOperation) Invoke(call sdkgo.Call, input UpdateEcommerceCustomerInput) sdkgo.MutationAttempt[EcommerceCustomerMetadata] {
	if !numericID.MatchString(input.CustomerID) || input.ExternalID == nil && input.Email == nil || input.ExternalID != nil && (strings.TrimSpace(*input.ExternalID) == "" || len(*input.ExternalID) > 250 || strings.ContainsAny(*input.ExternalID, "\r\n")) || input.Email != nil && !validContactEmail(*input.Email) {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchDefect, sdkgo.FailureValidation, "customer ID, external ID, or email is invalid")
	}
	fields := map[string]string{}
	if input.ExternalID != nil {
		fields["externalid"] = *input.ExternalID
	}
	if input.Email != nil {
		fields["email"] = *input.Email
	}
	encoded, err := json.Marshal(map[string]any{"ecomCustomer": fields})
	if err != nil {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchDefect, sdkgo.FailureLocalDefect, "customer update could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/ecomCustomers/"+input.CustomerID, encoded)
	if errors.Is(err, errCredential) {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("updateEcommerceCustomer", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("updateEcommerceCustomer", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("updateEcommerceCustomer", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchNotFound, sdkgo.FailureNotFound, "customer does not exist")
	}
	if status != http.StatusOK {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("updateEcommerceCustomer", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	customer, err := decodeEcommerceCustomer(body)
	if err != nil || customer.ID != input.CustomerID {
		return ecommerceCustomerMutationBranch("updateEcommerceCustomer", UpdateEcommerceCustomerBranchInvalidResponse, sdkgo.FailureProtocol, "customer response invalid")
	}
	return sdkgo.NewMutationBranch(UpdateEcommerceCustomerBranchUpdated, customer, nil, dealReceipt(call, customer.ID))
}

// Invoke deletes one customer and converges when the ID is already absent.
func (operation DeleteEcommerceCustomerOperation) Invoke(call sdkgo.Call, input DeleteEcommerceCustomerInput) sdkgo.MutationAttempt[EcommerceCustomerDeletion] {
	if !numericID.MatchString(input.CustomerID) {
		return ecommerceCustomerDeletionBranch(DeleteEcommerceCustomerBranchDefect, sdkgo.FailureValidation, "customer ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/ecomCustomers/"+input.CustomerID, nil)
	if errors.Is(err, errCredential) {
		return ecommerceCustomerDeletionBranch(DeleteEcommerceCustomerBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[EcommerceCustomerDeletion](queryFailure("deleteEcommerceCustomer", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[EcommerceCustomerDeletion](queryFailure("deleteEcommerceCustomer", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[EcommerceCustomerDeletion](queryFailure("deleteEcommerceCustomer", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return ecommerceCustomerDeletionBranch(DeleteEcommerceCustomerBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return ecommerceCustomerDeletionBranch(DeleteEcommerceCustomerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[EcommerceCustomerDeletion](queryFailure("deleteEcommerceCustomer", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	if status == http.StatusOK && len(body) > 0 && !json.Valid(body) {
		return ecommerceCustomerDeletionBranch(DeleteEcommerceCustomerBranchInvalidResponse, sdkgo.FailureProtocol, "invalid response JSON")
	}
	return sdkgo.NewMutationBranch(DeleteEcommerceCustomerBranchDeleted, EcommerceCustomerDeletion{CustomerID: input.CustomerID, Deleted: true}, nil, dealReceipt(call, input.CustomerID))
}

func ecommerceCustomerMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EcommerceCustomerMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, EcommerceCustomerMetadata{}, &failure, sdkgo.Receipt{})
}
func ecommerceCustomerDeletionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EcommerceCustomerDeletion] {
	failure := queryFailure("deleteEcommerceCustomer", kind, message)
	return sdkgo.NewMutationBranch(branch, EcommerceCustomerDeletion{}, &failure, sdkgo.Receipt{})
}
