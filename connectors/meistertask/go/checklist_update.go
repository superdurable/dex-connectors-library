// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateChecklistInput replaces selected checklist fields.
type UpdateChecklistInput struct {
	// ChecklistID is the positive checklist ID returned by MeisterTask.
	ChecklistID string `json:"checklistId"`
	// Name replaces the checklist name when provided; blank is invalid.
	Name *string `json:"name,omitempty"`
}

// UpdateChecklistOperation replaces selected checklist fields.
type UpdateChecklistOperation struct{ client *Client }

// UpdateChecklist binds the Mutation to this client.
func (client *Client) UpdateChecklist() UpdateChecklistOperation {
	return UpdateChecklistOperation{client}
}

// Definition returns the generated checklist Mutation contract.
func (UpdateChecklistOperation) Definition() sdkgo.MutationDefinition {
	return UpdateChecklistDefinition
}

// IdempotencyKey supplies a stable call identity for the absolute update.
func (UpdateChecklistOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateChecklistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute checklist update.
func (operation UpdateChecklistOperation) Invoke(call sdkgo.Call, input UpdateChecklistInput) sdkgo.MutationAttempt[ChecklistSummary] {
	if !validTaskID(input.ChecklistID) || input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureValidation, "checklist ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name *string `json:"name,omitempty"`
	}{input.Name})
	if err != nil {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureLocalDefect, "checklist update could not be encoded")
	}
	response := operation.client.put(call, "/checklists/"+input.ChecklistID, payload)
	if errors.Is(response.err, errCredential) {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ChecklistSummary](checklistFailure("updateChecklist", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ChecklistSummary](checklistFailure("updateChecklist", sdkgo.FailureRateLimit, "MeisterTask rate limited the update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ChecklistSummary](checklistFailure("updateChecklist", sdkgo.FailureAvailability, "MeisterTask update outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchNotFound, ChecklistSummary{}, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist")
	}
	if response.status != http.StatusOK {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchProviderRejected, ChecklistSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchInvalidResponse, ChecklistSummary{}, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ChecklistSummary](checklistFailure("updateChecklist", sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	checklist, err := decodeChecklist(response.body, input.ChecklistID)
	if err != nil {
		return checklistMutationBranch("updateChecklist", UpdateChecklistBranchInvalidResponse, ChecklistSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid checklist JSON")
	}
	return sdkgo.NewMutationBranch(UpdateChecklistBranchUpdated, checklist, nil, sdkgo.Receipt{})
}

func checklistMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := checklistFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
