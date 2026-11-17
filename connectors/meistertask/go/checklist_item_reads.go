// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ChecklistItemSummary contains one checklist item's identity and state.
type ChecklistItemSummary struct {
	// ID is the positive item ID.
	ID string `json:"id"`
	// ChecklistID is the positive parent checklist ID.
	ChecklistID string `json:"checklistId"`
	// Name is the item's visible name.
	Name string `json:"name"`
	// Status is 1 actionable or 5 completed.
	Status string `json:"status"`
}

// ChecklistItemsPage contains one page of checklist identities.
type ChecklistItemsPage struct {
	// Items are returned in provider order.
	Items []ChecklistItemSummary `json:"items"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListChecklistItemsInput selects a checklist and one page of items.
type ListChecklistItemsInput struct {
	// ChecklistID is the positive parent checklist ID.
	ChecklistID string `json:"checklistId"`
	// Page is one-based; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive page size; zero uses the provider default.
	Items int `json:"items"`
}

// GetChecklistItemInput selects one checklist item.
type GetChecklistItemInput struct {
	// ChecklistItemID is a positive decimal item ID.
	ChecklistItemID string `json:"checklistItemId"`
}

// ListChecklistItemsOperation reads task items.
type ListChecklistItemsOperation struct{ client *Client }

// ListChecklistItems binds the Query to this client.
func (client *Client) ListChecklistItems() ListChecklistItemsOperation {
	return ListChecklistItemsOperation{client}
}

// Definition returns the generated checklist-list Query contract.
func (ListChecklistItemsOperation) Definition() sdkgo.QueryDefinition {
	return ListChecklistItemsDefinition
}

// Invoke reads one page of checklist identities without persisting checklist text.
func (operation ListChecklistItemsOperation) Invoke(call sdkgo.Call, input ListChecklistItemsInput) sdkgo.QueryAttempt[ChecklistItemsPage] {
	if !validTaskID(input.ChecklistID) || input.Page < 0 || input.Items < 0 || input.Items > 100 {
		return checklistItemQueryBranch("listChecklistItems", ListChecklistItemsBranchDefect, ChecklistItemsPage{}, sdkgo.FailureValidation, "checklist ID or item page is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	response := operation.client.get(call, "/checklists/"+input.ChecklistID+"/checklist_items?"+query.Encode())
	if decision, handled := classifyChecklistItemQueryResponse("listChecklistItems", response, ChecklistItemsPage{}, "", ListChecklistItemsBranchProviderRejected, ListChecklistItemsBranchInvalidResponse, ListChecklistItemsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return checklistItemQueryBranch("listChecklistItems", ListChecklistItemsBranchInvalidResponse, ChecklistItemsPage{}, sdkgo.FailureProtocol, "MeisterTask returned invalid items JSON")
	}
	items := make([]ChecklistItemSummary, 0, len(wire))
	for _, raw := range wire {
		checklist, err := decodeChecklistItem(raw, "")
		if err != nil || checklist.ChecklistID != input.ChecklistID {
			return checklistItemQueryBranch("listChecklistItems", ListChecklistItemsBranchInvalidResponse, ChecklistItemsPage{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete checklist metadata")
		}
		items = append(items, checklist)
	}
	return sdkgo.NewQueryBranch(ListChecklistItemsBranchListed, ChecklistItemsPage{Items: items, Page: page}, nil, sdkgo.Receipt{})
}

// GetChecklistItemOperation reads one checklist's identity.
type GetChecklistItemOperation struct{ client *Client }

// GetChecklistItem binds the Query to this client.
func (client *Client) GetChecklistItem() GetChecklistItemOperation {
	return GetChecklistItemOperation{client}
}

// Definition returns the generated checklist Query contract.
func (GetChecklistItemOperation) Definition() sdkgo.QueryDefinition {
	return GetChecklistItemDefinition
}

// Invoke reads a checklist without persisting its text or HTML.
func (operation GetChecklistItemOperation) Invoke(call sdkgo.Call, input GetChecklistItemInput) sdkgo.QueryAttempt[ChecklistItemSummary] {
	if !validTaskID(input.ChecklistItemID) {
		return checklistItemQueryBranch("getChecklistItem", GetChecklistItemBranchDefect, ChecklistItemSummary{}, sdkgo.FailureValidation, "checklist item ID is invalid")
	}
	response := operation.client.get(call, "/checklist_items/"+input.ChecklistItemID)
	if decision, handled := classifyChecklistItemQueryResponse("getChecklistItem", response, ChecklistItemSummary{}, GetChecklistItemBranchNotFound, GetChecklistItemBranchProviderRejected, GetChecklistItemBranchInvalidResponse, GetChecklistItemBranchDefect); handled {
		return decision
	}
	checklist, err := decodeChecklistItem(response.body, input.ChecklistItemID)
	if err != nil {
		return checklistItemQueryBranch("getChecklistItem", GetChecklistItemBranchInvalidResponse, ChecklistItemSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid checklist JSON")
	}
	return sdkgo.NewQueryBranch(GetChecklistItemBranchFound, checklist, nil, sdkgo.Receipt{})
}

func decodeChecklistItem(body []byte, requestedID string) (ChecklistItemSummary, error) {
	var wire struct {
		ID          int64  `json:"id"`
		ChecklistID int64  `json:"checklist_id"`
		Name        string `json:"name"`
		Status      *int64 `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChecklistItemSummary{}, err
	}
	id, taskID := strconv.FormatInt(wire.ID, 10), strconv.FormatInt(wire.ChecklistID, 10)
	if !validTaskID(id) || !validTaskID(taskID) || wire.Name == "" || wire.Status == nil || (*wire.Status != 1 && *wire.Status != 5) || (requestedID != "" && requestedID != id) {
		return ChecklistItemSummary{}, errors.New("checklist identity or name missing")
	}
	return ChecklistItemSummary{ID: id, ChecklistID: taskID, Name: wire.Name, Status: strconv.FormatInt(*wire.Status, 10)}, nil
}

func classifyChecklistItemQueryResponse[Output any](operation string, response providerResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return checklistItemQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](checklistItemFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](checklistItemFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](checklistItemFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return checklistItemQueryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist"), true
	}
	if response.status != http.StatusOK {
		return checklistItemQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return checklistItemQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](checklistItemFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func checklistItemFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}
func checklistItemQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := checklistItemFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
