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

// ListTasksInput selects one page from the MeisterTask tasks collection.
type ListTasksInput struct {
	// Page is the positive one-based page number; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive maximum number of tasks in a page; zero uses the provider default.
	Items int `json:"items"`
	// Status filters by open, completed, completed_archived, or trashed; blank includes all statuses.
	Status string `json:"status"`
}

// TasksPage contains one requested page of public task summaries.
type TasksPage struct {
	// Tasks is the ordered set of tasks in this response.
	Tasks []TaskSummary `json:"tasks"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListTasksOperation reads one page of tasks visible to the token owner.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the Query to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke requests one task page and classifies provider failures.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TasksPage] {
	if input.Page < 0 || input.Items < 0 || input.Items > 100 || !validTaskStatus(input.Status) {
		return listTasksBranch(ListTasksBranchDefect, sdkgo.FailureValidation, "task page, size, or status is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	response := operation.client.get(call, "/tasks?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return listTasksBranch(ListTasksBranchDefect, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TasksPage](listTasksFailure(sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TasksPage](listTasksFailure(sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TasksPage](listTasksFailure(sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listTasksBranch(ListTasksBranchProviderRejected, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TasksPage](listTasksFailure(sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	var wireTasks []json.RawMessage
	if err := json.Unmarshal(response.body, &wireTasks); err != nil {
		return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "MeisterTask returned invalid task list JSON")
	}
	tasks := make([]TaskSummary, 0, len(wireTasks))
	for _, wireTask := range wireTasks {
		var identity struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(wireTask, &identity); err != nil || identity.ID <= 0 {
			return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "MeisterTask returned an invalid task ID")
		}
		task, err := decodeTask(wireTask, strconv.FormatInt(identity.ID, 10))
		if err != nil {
			return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "MeisterTask returned incomplete task metadata")
		}
		tasks = append(tasks, task)
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, TasksPage{Tasks: tasks, Page: page}, nil, sdkgo.Receipt{})
}

func validTaskStatus(status string) bool {
	switch status {
	case "", "open", "completed", "completed_archived", "trashed":
		return true
	}
	return false
}

func listTasksFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: "listTasks", Message: message}
}

func listTasksBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TasksPage] {
	failure := listTasksFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TasksPage{}, &failure, sdkgo.Receipt{})
}
