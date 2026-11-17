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

// ChecklistSummary contains task checklist identity and name.
type ChecklistSummary struct {
	// ID is the positive checklist ID.
	ID string `json:"id"`
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// Name is the checklist's visible name.
	Name string `json:"name"`
}

// TaskChecklistsPage contains one page of checklist identities.
type TaskChecklistsPage struct {
	// Checklists are returned in provider order.
	Checklists []ChecklistSummary `json:"checklists"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListTaskChecklistsInput selects a task and one page of checklists.
type ListTaskChecklistsInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// Page is one-based; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive page size; zero uses the provider default.
	Items int `json:"items"`
}

// GetChecklistInput selects one checklist.
type GetChecklistInput struct {
	// ChecklistID is a positive decimal checklist ID.
	ChecklistID string `json:"checklistId"`
}

// ListTaskChecklistsOperation reads task checklists.
type ListTaskChecklistsOperation struct{ client *Client }

// ListTaskChecklists binds the Query to this client.
func (client *Client) ListTaskChecklists() ListTaskChecklistsOperation {
	return ListTaskChecklistsOperation{client}
}

// Definition returns the generated checklist-list Query contract.
func (ListTaskChecklistsOperation) Definition() sdkgo.QueryDefinition {
	return ListTaskChecklistsDefinition
}

// Invoke reads one page of checklist identities without persisting checklist text.
func (operation ListTaskChecklistsOperation) Invoke(call sdkgo.Call, input ListTaskChecklistsInput) sdkgo.QueryAttempt[TaskChecklistsPage] {
	if !validTaskID(input.TaskID) || input.Page < 0 || input.Items < 0 || input.Items > 100 {
		return checklistQueryBranch("listTaskChecklists", ListTaskChecklistsBranchDefect, TaskChecklistsPage{}, sdkgo.FailureValidation, "task ID or checklist page is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	response := operation.client.get(call, "/tasks/"+input.TaskID+"/checklists?"+query.Encode())
	if decision, handled := classifyChecklistQueryResponse("listTaskChecklists", response, TaskChecklistsPage{}, "", ListTaskChecklistsBranchProviderRejected, ListTaskChecklistsBranchInvalidResponse, ListTaskChecklistsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return checklistQueryBranch("listTaskChecklists", ListTaskChecklistsBranchInvalidResponse, TaskChecklistsPage{}, sdkgo.FailureProtocol, "MeisterTask returned invalid checklists JSON")
	}
	checklists := make([]ChecklistSummary, 0, len(wire))
	for _, raw := range wire {
		checklist, err := decodeChecklist(raw, "")
		if err != nil || checklist.TaskID != input.TaskID {
			return checklistQueryBranch("listTaskChecklists", ListTaskChecklistsBranchInvalidResponse, TaskChecklistsPage{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete checklist metadata")
		}
		checklists = append(checklists, checklist)
	}
	return sdkgo.NewQueryBranch(ListTaskChecklistsBranchListed, TaskChecklistsPage{Checklists: checklists, Page: page}, nil, sdkgo.Receipt{})
}

// GetChecklistOperation reads one checklist's identity.
type GetChecklistOperation struct{ client *Client }

// GetChecklist binds the Query to this client.
func (client *Client) GetChecklist() GetChecklistOperation { return GetChecklistOperation{client} }

// Definition returns the generated checklist Query contract.
func (GetChecklistOperation) Definition() sdkgo.QueryDefinition { return GetChecklistDefinition }

// Invoke reads a checklist without persisting its text or HTML.
func (operation GetChecklistOperation) Invoke(call sdkgo.Call, input GetChecklistInput) sdkgo.QueryAttempt[ChecklistSummary] {
	if !validTaskID(input.ChecklistID) {
		return checklistQueryBranch("getChecklist", GetChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureValidation, "checklist ID is invalid")
	}
	response := operation.client.get(call, "/checklists/"+input.ChecklistID)
	if decision, handled := classifyChecklistQueryResponse("getChecklist", response, ChecklistSummary{}, GetChecklistBranchNotFound, GetChecklistBranchProviderRejected, GetChecklistBranchInvalidResponse, GetChecklistBranchDefect); handled {
		return decision
	}
	checklist, err := decodeChecklist(response.body, input.ChecklistID)
	if err != nil {
		return checklistQueryBranch("getChecklist", GetChecklistBranchInvalidResponse, ChecklistSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid checklist JSON")
	}
	return sdkgo.NewQueryBranch(GetChecklistBranchFound, checklist, nil, sdkgo.Receipt{})
}

func decodeChecklist(body []byte, requestedID string) (ChecklistSummary, error) {
	var wire struct {
		ID     int64  `json:"id"`
		TaskID int64  `json:"task_id"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChecklistSummary{}, err
	}
	id, taskID := strconv.FormatInt(wire.ID, 10), strconv.FormatInt(wire.TaskID, 10)
	if !validTaskID(id) || !validTaskID(taskID) || wire.Name == "" || (requestedID != "" && requestedID != id) {
		return ChecklistSummary{}, errors.New("checklist identity or name missing")
	}
	return ChecklistSummary{ID: id, TaskID: taskID, Name: wire.Name}, nil
}

func classifyChecklistQueryResponse[Output any](operation string, response providerResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return checklistQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](checklistFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](checklistFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](checklistFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return checklistQueryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist"), true
	}
	if response.status != http.StatusOK {
		return checklistQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return checklistQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](checklistFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func checklistFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}
func checklistQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := checklistFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
