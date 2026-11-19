// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"time"
)

// UpdateLeadInput replaces selected fields on an existing lead.
type UpdateLeadInput struct {
	// LeadID is the positive ID of the existing lead.
	LeadID int64 `json:"leadId"`
	// FirstName replaces the first name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName replaces the last name when supplied.
	LastName *string `json:"lastName,omitempty"`
}

// UpdatedLead identifies an accepted lead update.
type UpdatedLead struct {
	// ID is the lead ID.
	ID int64 `json:"id"`
	// Status is Marketo's update status.
	Status string `json:"status"`
}

// UpdateLeadOperation updates an existing lead by ID.
type UpdateLeadOperation struct{ client *Client }

// UpdateLead binds this Mutation to the client.
func (client *Client) UpdateLead() UpdateLeadOperation { return UpdateLeadOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateLeadOperation) Definition() sdkgo.MutationDefinition { return UpdateLeadDefinition }

// IdempotencyKey derives a stable call identity for the absolute update.
func (UpdateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected names with Marketo updateOnly, which never creates a lead.
func (operation UpdateLeadOperation) Invoke(call sdkgo.Call, input UpdateLeadInput) sdkgo.MutationAttempt[UpdatedLead] {
	if input.LeadID <= 0 || input.FirstName == nil && input.LastName == nil {
		return updateLeadBranch(UpdateLeadBranchDefect, sdkgo.FailureValidation, "lead ID or name fields are invalid")
	}
	fields := map[string]any{"id": input.LeadID}
	if input.FirstName != nil {
		fields["firstName"] = *input.FirstName
	}
	if input.LastName != nil {
		fields["lastName"] = *input.LastName
	}
	body, err := json.Marshal(map[string]any{"action": "updateOnly", "lookupField": "id", "input": []any{fields}})
	if err != nil {
		return updateLeadBranch(UpdateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead update could not be encoded")
	}
	response := operation.client.requestLeadAPI(call, http.MethodPost, "/v1/leads.json", body)
	if errors.Is(response.err, errLeadReauthorization) {
		return updateLeadBranch(UpdateLeadBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return updateLeadRetry(sdkgo.FailureTransport, "Marketo request or credential refresh failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return updateLeadRetry(sdkgo.FailureRateLimit, "Marketo rate limited the update", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return updateLeadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return updateLeadBranch(UpdateLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return updateLeadBranch(UpdateLeadBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Marketo response exceeded the configured limit")
	}
	if response.err != nil {
		return updateLeadRetry(sdkgo.FailureTransport, "Marketo response was interrupted", 0)
	}
	var envelope struct {
		Success *bool          `json:"success"`
		Result  *[]UpdatedLead `json:"result"`
		Errors  []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil || envelope.Success == nil {
		return updateLeadBranch(UpdateLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid update JSON")
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return updateLeadBranch(UpdateLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an error without code")
		}
		switch envelope.Errors[0].Code {
		case "702":
			return updateLeadBranch(UpdateLeadBranchNotFound, sdkgo.FailureNotFound, "Marketo lead was not found")
		case "602", "606", "607", "615":
			return updateLeadRetry(sdkgo.FailureRateLimit, "Marketo token or quota temporarily blocked the update", response.retryAfter)
		case "604", "608", "611", "614", "500", "502":
			return updateLeadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
		default:
			return updateLeadBranch(UpdateLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the lead update")
		}
	}
	if envelope.Result == nil || len(*envelope.Result) > 1 {
		return updateLeadBranch(UpdateLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid update result")
	}
	if len(*envelope.Result) == 0 {
		return updateLeadBranch(UpdateLeadBranchNotFound, sdkgo.FailureNotFound, "Marketo found no lead to update")
	}
	updated := (*envelope.Result)[0]
	if updated.ID != input.LeadID || updated.Status != "updated" {
		return updateLeadBranch(UpdateLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an unexpected update result")
	}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-marketo", ProviderObjectID: strconv.FormatInt(updated.ID, 10), ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(UpdateLeadBranchUpdated, updated, nil, receipt)
}

func updateLeadFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "updateLead", Message: message}
}
func updateLeadBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UpdatedLead] {
	failure := updateLeadFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, UpdatedLead{}, &failure, sdkgo.Receipt{})
}
func updateLeadRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[UpdatedLead] {
	return sdkgo.NewMutationRetry[UpdatedLead](updateLeadFailure(kind, message), delay)
}
