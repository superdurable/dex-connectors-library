// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTasksInput selects one Copper search page with optional filters.
type ListTasksInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Status optionally selects Open or Completed tasks.
	Status string `json:"status,omitempty"`
}

// TasksPage contains one returned page of task summaries.
type TasksPage struct {
	// Tasks are the records returned by Copper for this page.
	Tasks []TaskMetadata `json:"tasks"`
}

// ListTasksOperation searches Copper tasks without changing them.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the Query to this client.
func (client *Client) ListTasks() ListTasksOperation {
	return ListTasksOperation{client}
}

// Definition returns the generated Query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition {
	return ListTasksDefinition
}

// Invoke reads one page through Copper's tasks search endpoint.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TasksPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		(input.Status != "" && input.Status != "Open" && input.Status != "Completed") {
		return listTasksBranch(ListTasksBranchDefect, sdkgo.FailureValidation, "invalid tasks page or filter")
	}
	request := struct {
		PageNumber int      `json:"page_number,omitempty"`
		PageSize   int      `json:"page_size,omitempty"`
		Statuses   []string `json:"statuses,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize}
	if input.Status != "" {
		request.Statuses = []string{input.Status}
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listTasksBranch(ListTasksBranchDefect, sdkgo.FailureLocalDefect, "tasks search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/tasks/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listTasksBranch(ListTasksBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TasksPage](queryFailure("listTasks", sdkgo.FailureRateLimit, "Copper rate limited tasks search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TasksPage](queryFailure("listTasks", sdkgo.FailureAvailability, "Copper tasks search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper tasks response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TasksPage](queryFailure("listTasks", sdkgo.FailureTransport, "Copper tasks response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listTasksBranch(ListTasksBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid tasks JSON")
	}
	page := TasksPage{Tasks: make([]TaskMetadata, 0, len(records))}
	for _, record := range records {
		task, decodeError := decodeTask(record)
		if decodeError != nil {
			return listTasksBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid task")
		}
		page.Tasks = append(page.Tasks, task)
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, page, nil, sdkgo.Receipt{})
}

func listTasksBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TasksPage] {
	failure := queryFailure("listTasks", kind, message)
	return sdkgo.NewQueryBranch(branch, TasksPage{}, &failure, sdkgo.Receipt{})
}
