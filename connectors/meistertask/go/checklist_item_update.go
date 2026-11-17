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

// UpdateChecklistItemInput replaces selected checklist fields.
type UpdateChecklistItemInput struct {
	// ChecklistItemID is the positive checklist ID returned by MeisterTask.
	ChecklistItemID string `json:"checklistItemId"`
	// Name replaces the checklist name when provided; blank is invalid.
	Name *string `json:"name,omitempty"`
	// Status sets 1 actionable or 5 completed.
	Status *int `json:"status,omitempty"`
}

// UpdateChecklistItemOperation replaces selected checklist fields.
type UpdateChecklistItemOperation struct{ client *Client }

// UpdateChecklistItem binds the Mutation to this client.
func (client *Client) UpdateChecklistItem() UpdateChecklistItemOperation {
	return UpdateChecklistItemOperation{client}
}

// Definition returns the generated checklist Mutation contract.
func (UpdateChecklistItemOperation) Definition() sdkgo.MutationDefinition {
	return UpdateChecklistItemDefinition
}

// IdempotencyKey supplies a stable call identity for the absolute update.
func (UpdateChecklistItemOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateChecklistItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute checklist update.
func (operation UpdateChecklistItemOperation) Invoke(call sdkgo.Call, input UpdateChecklistItemInput) sdkgo.MutationAttempt[ChecklistItemSummary] {
	if !validTaskID(input.ChecklistItemID) || (input.Name == nil && input.Status == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Status != nil && *input.Status != 1 && *input.Status != 5) {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchDefect, ChecklistItemSummary{}, sdkgo.FailureValidation, "checklist ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name   *string `json:"name,omitempty"`
		Status *int    `json:"status,omitempty"`
	}{input.Name, input.Status})
	if err != nil {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchDefect, ChecklistItemSummary{}, sdkgo.FailureLocalDefect, "checklist update could not be encoded")
	}
	response := operation.client.put(call, "/checklist_items/"+input.ChecklistItemID, payload)
	if errors.Is(response.err, errCredential) {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchDefect, ChecklistItemSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ChecklistItemSummary](checklistItemFailure("updateChecklistItem", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ChecklistItemSummary](checklistItemFailure("updateChecklistItem", sdkgo.FailureRateLimit, "MeisterTask rate limited the update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ChecklistItemSummary](checklistItemFailure("updateChecklistItem", sdkgo.FailureAvailability, "MeisterTask update outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchNotFound, ChecklistItemSummary{}, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist")
	}
	if response.status != http.StatusOK {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchProviderRejected, ChecklistItemSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchInvalidResponse, ChecklistItemSummary{}, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ChecklistItemSummary](checklistItemFailure("updateChecklistItem", sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	checklist, err := decodeChecklistItem(response.body, input.ChecklistItemID)
	if err != nil {
		return checklistItemMutationBranch("updateChecklistItem", UpdateChecklistItemBranchInvalidResponse, ChecklistItemSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid checklist JSON")
	}
	return sdkgo.NewMutationBranch(UpdateChecklistItemBranchUpdated, checklist, nil, sdkgo.Receipt{})
}

func checklistItemMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := checklistItemFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
