// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wrike

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListTasksOperation searches one Wrike folder.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the Query to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client: client} }

// Definition returns the manifest-generated Query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke reads one task page and classifies provider outcomes.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TaskPage] {
	if !validTaskID(input.FolderID) || input.PageSize < 0 || input.PageSize > 1000 || len(input.NextPageToken) > 2048 {
		return taskPageBranch(ListTasksBranchDefect, sdkgo.FailureValidation, "folder ID or pagination is invalid")
	}
	parameters := url.Values{}
	if input.Title != "" {
		parameters.Set("title", input.Title)
	}
	if input.PageSize > 0 {
		parameters.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.NextPageToken != "" {
		parameters.Set("nextPageToken", input.NextPageToken)
	}
	path := "/folders/" + url.PathEscape(input.FolderID) + "/tasks"
	if len(parameters) > 0 {
		path += "?" + parameters.Encode()
	}
	response := operation.client.get(call, path)
	if response.err == errCredential {
		return taskPageBranch(ListTasksBranchDefect, sdkgo.FailureAuthentication, "Wrike credentials are unavailable")
	}
	if response.err == errRequest {
		return taskPageRetry(sdkgo.FailureTransport, "Wrike request failed")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TaskPage](taskPageFailure(sdkgo.FailureRateLimit, "Wrike rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TaskPage](taskPageFailure(sdkgo.FailureAvailability, "Wrike is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return taskPageBranch(ListTasksBranchProviderRejected, sdkgo.FailureProviderRejection, "Wrike rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err == providerhttp.ErrBodyTooLarge {
		return taskPageBranch(ListTasksBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wrike response exceeded the configured limit")
	}
	if response.err != nil {
		return taskPageRetry(sdkgo.FailureTransport, "Wrike response was interrupted")
	}
	var wire struct {
		Kind          string       `json:"kind"`
		Data          []TaskStatus `json:"data"`
		NextPageToken string       `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Kind != "tasks" || wire.Data == nil {
		return taskPageBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "Wrike returned invalid task page JSON")
	}
	for _, task := range wire.Data {
		if !validTaskID(task.ID) || strings.TrimSpace(task.Title) == "" || task.Status == "" {
			return taskPageBranch(ListTasksBranchInvalidResponse, sdkgo.FailureProtocol, "Wrike returned an incomplete task")
		}
	}
	return sdkgo.NewQueryBranch(ListTasksBranchFound, TaskPage{Data: wire.Data, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

func taskPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "wrike", Operation: "listTasks", Message: message}
}
func taskPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TaskPage] {
	failure := taskPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TaskPage{}, &failure, sdkgo.Receipt{})
}
func taskPageRetry(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TaskPage] {
	return sdkgo.NewQueryRetry[TaskPage](taskPageFailure(kind, message), 0)
}
