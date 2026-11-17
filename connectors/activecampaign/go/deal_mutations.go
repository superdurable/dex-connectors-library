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

// UpdateDealInput replaces selected absolute fields on one deal.
type UpdateDealInput struct {
	// DealID is the numeric provider deal ID.
	DealID string `json:"dealId"`
	// Title replaces the deal title when supplied.
	Title *string `json:"title,omitempty"`
	// StageID moves the deal to this numeric stage when supplied.
	StageID *string `json:"stageId,omitempty"`
	// ValueMinor replaces the deal value in minor currency units when supplied.
	ValueMinor *int64 `json:"valueMinor,omitempty"`
	// Currency replaces the three-letter ISO currency code when supplied.
	Currency *string `json:"currency,omitempty"`
	// Status replaces the status: 0 open, 1 won, or 2 lost.
	Status *int `json:"status,omitempty"`
}

// DeleteDealInput selects one deal for removal.
type DeleteDealInput struct {
	// DealID is the numeric provider deal ID.
	DealID string `json:"dealId"`
}

// DealDeletion confirms the selected deal is absent.
type DealDeletion struct {
	// DealID is the deal removed or already absent.
	DealID string `json:"dealId"`
	// Deleted is true when the deal is absent after the call.
	Deleted bool `json:"deleted"`
}

// UpdateDealOperation replaces selected fields on a deal by ID.
type UpdateDealOperation struct{ client *Client }

// DeleteDealOperation removes a deal by ID.
type DeleteDealOperation struct{ client *Client }

// UpdateDeal binds the update Mutation to this client.
func (client *Client) UpdateDeal() UpdateDealOperation { return UpdateDealOperation{client} }

// DeleteDeal binds the delete Mutation to this client.
func (client *Client) DeleteDeal() DeleteDealOperation { return DeleteDealOperation{client} }

// Definition returns the manifest-generated update contract.
func (UpdateDealOperation) Definition() sdkgo.MutationDefinition { return UpdateDealDefinition }

// Definition returns the manifest-generated delete contract.
func (DeleteDealOperation) Definition() sdkgo.MutationDefinition { return DeleteDealDefinition }

// IdempotencyKey derives the stable call identity; absolute replacement is safe to repeat.
func (UpdateDealOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable call identity; deleting an absent ID is a no-op.
func (DeleteDealOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets supplied absolute deal fields by ID.
func (operation UpdateDealOperation) Invoke(call sdkgo.Call, input UpdateDealInput) sdkgo.MutationAttempt[DealMetadata] {
	if !numericID.MatchString(input.DealID) || input.Title == nil && input.StageID == nil && input.ValueMinor == nil && input.Currency == nil && input.Status == nil {
		return dealMutationBranch("updateDeal", UpdateDealBranchDefect, sdkgo.FailureValidation, "numeric deal ID and at least one field are required")
	}
	if input.Title != nil && (strings.TrimSpace(*input.Title) == "" || len(*input.Title) > 250) || input.StageID != nil && !numericID.MatchString(*input.StageID) || input.ValueMinor != nil && *input.ValueMinor < 0 || input.Currency != nil && !validDealCurrency(*input.Currency) || input.Status != nil && (*input.Status < 0 || *input.Status > 2) {
		return dealMutationBranch("updateDeal", UpdateDealBranchDefect, sdkgo.FailureValidation, "deal field is invalid")
	}
	fields := map[string]any{}
	if input.Title != nil {
		fields["title"] = *input.Title
	}
	if input.StageID != nil {
		fields["stage"] = *input.StageID
	}
	if input.ValueMinor != nil {
		fields["value"] = *input.ValueMinor
	}
	if input.Currency != nil {
		fields["currency"] = strings.ToLower(*input.Currency)
	}
	if input.Status != nil {
		fields["status"] = *input.Status
	}
	encoded, err := json.Marshal(map[string]any{"deal": fields})
	if err != nil {
		return dealMutationBranch("updateDeal", UpdateDealBranchDefect, sdkgo.FailureLocalDefect, "deal update could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/deals/"+input.DealID, encoded)
	if errors.Is(err, errCredential) {
		return dealMutationBranch("updateDeal", UpdateDealBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("updateDeal", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("updateDeal", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("updateDeal", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return dealMutationBranch("updateDeal", UpdateDealBranchNotFound, sdkgo.FailureNotFound, "ActiveCampaign did not find the deal")
	}
	if status != http.StatusOK {
		return dealMutationBranch("updateDeal", UpdateDealBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return dealMutationBranch("updateDeal", UpdateDealBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DealMetadata](queryFailure("updateDeal", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	deal, err := decodeDeal(body)
	if err != nil {
		return dealMutationBranch("updateDeal", UpdateDealBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(UpdateDealBranchUpdated, deal, nil, dealReceipt(call, deal.ID))
}

// Invoke removes a deal and treats an already-absent ID as converged.
func (operation DeleteDealOperation) Invoke(call sdkgo.Call, input DeleteDealInput) sdkgo.MutationAttempt[DealDeletion] {
	if !numericID.MatchString(input.DealID) {
		return dealDeletionBranch("deleteDeal", DeleteDealBranchDefect, sdkgo.FailureValidation, "deal ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/deals/"+input.DealID, nil)
	if errors.Is(err, errCredential) {
		return dealDeletionBranch("deleteDeal", DeleteDealBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[DealDeletion](queryFailure("deleteDeal", sdkgo.FailureTransport, "ActiveCampaign request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DealDeletion](queryFailure("deleteDeal", sdkgo.FailureRateLimit, "ActiveCampaign rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[DealDeletion](queryFailure("deleteDeal", sdkgo.FailureAvailability, "ActiveCampaign is temporarily unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return dealDeletionBranch("deleteDeal", DeleteDealBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return dealDeletionBranch("deleteDeal", DeleteDealBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ActiveCampaign response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DealDeletion](queryFailure("deleteDeal", sdkgo.FailureTransport, "ActiveCampaign response was interrupted"), 0)
	}
	if status == http.StatusOK && !json.Valid(body) {
		return dealDeletionBranch("deleteDeal", DeleteDealBranchInvalidResponse, sdkgo.FailureProtocol, "ActiveCampaign returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(DeleteDealBranchDeleted, DealDeletion{DealID: input.DealID, Deleted: true}, nil, dealReceipt(call, input.DealID))
}

func dealDeletionBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DealDeletion] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, DealDeletion{}, &failure, sdkgo.Receipt{})
}
