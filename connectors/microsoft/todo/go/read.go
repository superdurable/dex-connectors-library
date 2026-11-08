// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todo

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const graphRoot = "https://graph.microsoft.com/v1.0"
const maxPages = 20
const maxItems = 2000

// ListTaskListsInput requests the signed-in person's task lists.
type ListTaskListsInput struct{}

// TaskList identifies one To Do list.
type TaskList struct {
	// ID is the stable Graph task-list ID.
	ID string `json:"id"`
	// DisplayName is the user-visible list name.
	DisplayName string `json:"displayName"`
}

// TaskListsPage contains a complete bounded collection of task lists.
type TaskListsPage struct {
	// Lists contains every fetched list, up to the documented safety limit.
	Lists []TaskList `json:"lists"`
}

// ListTasksInput selects a task list returned by ListTaskLists.
type ListTasksInput struct {
	// ListID is the stable non-secret ID returned by ListTaskLists.
	ListID string `json:"listId"`
}

// Task describes one selected To Do task.
type Task struct {
	// ID is the stable Graph task ID.
	ID string `json:"id"`
	// Title is the task title entered in To Do.
	Title string `json:"title"`
	// Status is the Graph task status, such as notStarted or completed.
	Status string `json:"status"`
}

// TasksPage contains a complete bounded collection of tasks.
type TasksPage struct {
	// Tasks contains every fetched task, up to the documented safety limit.
	Tasks []Task `json:"tasks"`
}

// ListTaskListsOperation reads the account's task lists.
type ListTaskListsOperation struct{ client *Client }

// ListTaskLists binds the Query to this client.
func (client *Client) ListTaskLists() ListTaskListsOperation { return ListTaskListsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListTaskListsOperation) Definition() sdkgo.QueryDefinition { return ListTaskListsDefinition }

// Invoke reads the bounded collection and classifies provider responses.
func (operation ListTaskListsOperation) Invoke(call sdkgo.Call, _ ListTaskListsInput) sdkgo.QueryAttempt[TaskListsPage] {
	records, result := operation.client.readCollection(call, "/me/todo/lists")
	if result != nil {
		return listFailureAttempt(*result)
	}
	output := TaskListsPage{Lists: make([]TaskList, 0, len(records))}
	for _, record := range records {
		var item TaskList
		if json.Unmarshal(record, &item) != nil || item.ID == "" || item.DisplayName == "" {
			return listFailureAttempt(readFailure{branch: ListTaskListsBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Graph returned an invalid task list"})
		}
		output.Lists = append(output.Lists, item)
	}
	return sdkgo.NewQueryBranch(ListTaskListsBranchListed, output, nil, sdkgo.Receipt{})
}

// ListTasksOperation reads tasks in a selected list.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the Query to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke reads the bounded collection and classifies provider responses.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TasksPage] {
	if strings.TrimSpace(input.ListID) == "" || strings.ContainsAny(input.ListID, "\r\n") {
		return taskFailureAttempt(readFailure{branch: ListTasksBranchDefect, kind: sdkgo.FailureValidation, message: "To Do list ID is required"})
	}
	records, result := operation.client.readCollection(call, "/me/todo/lists/"+url.PathEscape(input.ListID)+"/tasks")
	if result != nil {
		if result.branch == "notFound" {
			result.branch = ListTasksBranchNotFound
		}
		return taskFailureAttempt(*result)
	}
	output := TasksPage{Tasks: make([]Task, 0, len(records))}
	for _, record := range records {
		var item Task
		if json.Unmarshal(record, &item) != nil || item.ID == "" || item.Title == "" {
			return taskFailureAttempt(readFailure{branch: ListTasksBranchInvalidResponse, kind: sdkgo.FailureProtocol, message: "Graph returned an invalid task"})
		}
		output.Tasks = append(output.Tasks, item)
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, output, nil, sdkgo.Receipt{})
}

type readFailure struct {
	branch     sdkgo.BranchID
	kind       sdkgo.FailureKind
	message    string
	retryAfter time.Duration
}

func (client *Client) readCollection(call sdkgo.Call, path string) ([]json.RawMessage, *readFailure) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return nil, &readFailure{branch: sdkgo.DefectBranchID, kind: sdkgo.FailureAuthentication, message: "To Do connection needs authorization"}
		}
		return nil, &readFailure{kind: sdkgo.FailureTransport, message: "To Do credential refresh failed"}
	}
	token := credentials.AccessToken.Reveal()
	if token == "" || !providerhttp.IsHeaderSafeCredential(token) {
		return nil, &readFailure{branch: sdkgo.DefectBranchID, kind: sdkgo.FailureAuthentication, message: "To Do access token is unavailable"}
	}
	target := graphRoot + path
	records := make([]json.RawMessage, 0)
	seen := make(map[string]bool)
	for page := 0; page < maxPages; page++ {
		if seen[target] {
			return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureProtocol, message: "Graph repeated a page"}
		}
		seen[target] = true
		request, err := http.NewRequestWithContext(call.Context, http.MethodGet, target, nil)
		if err != nil {
			return nil, &readFailure{branch: sdkgo.DefectBranchID, kind: sdkgo.FailureValidation, message: "To Do request could not be built"}
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/json")
		response, err := client.httpClient.Do(request)
		if err != nil {
			return nil, &readFailure{kind: sdkgo.FailureTransport, message: "Graph request failed"}
		}
		body, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
		closeErr := response.Body.Close()
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
			return nil, &readFailure{kind: sdkgo.FailureAvailability, message: "Graph is temporarily unavailable", retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
		}
		if response.StatusCode == http.StatusNotFound {
			return nil, &readFailure{branch: "notFound", kind: sdkgo.FailureNotFound, message: "To Do list was not found"}
		}
		if response.StatusCode != http.StatusOK {
			return nil, &readFailure{branch: "providerRejected", kind: sdkgo.FailureProviderRejection, message: "Graph rejected the request"}
		}
		if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
			return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureResponseTooLarge, message: "Graph response exceeded the limit"}
		}
		if readErr != nil || closeErr != nil {
			return nil, &readFailure{kind: sdkgo.FailureTransport, message: "Graph response was interrupted"}
		}
		var envelope struct {
			Value    *[]json.RawMessage `json:"value"`
			NextLink string             `json:"@odata.nextLink"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Value == nil {
			return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureProtocol, message: "Graph returned invalid collection JSON"}
		}
		records = append(records, *envelope.Value...)
		if len(records) > maxItems {
			return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureResponseTooLarge, message: "Graph collection exceeded the item limit"}
		}
		if envelope.NextLink == "" {
			return records, nil
		}
		next, err := url.Parse(envelope.NextLink)
		if err != nil || next.Scheme != "https" || next.Host != "graph.microsoft.com" || next.User != nil || next.Fragment != "" || next.Path != "/v1.0"+path {
			return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureProtocol, message: "Graph returned an unsafe next page URL"}
		}
		target = envelope.NextLink
	}
	return nil, &readFailure{branch: "invalidResponse", kind: sdkgo.FailureResponseTooLarge, message: "Graph collection exceeded the page limit"}
}

func listFailureAttempt(result readFailure) sdkgo.QueryAttempt[TaskListsPage] {
	failure := sdkgo.Failure{Kind: result.kind, Provider: "microsoft-todo", Operation: "listTaskLists", Message: result.message}
	if result.branch == "" {
		return sdkgo.NewQueryRetry[TaskListsPage](failure, result.retryAfter)
	}
	if result.branch == "notFound" {
		result.branch = ListTaskListsBranchProviderRejected
	}
	return sdkgo.NewQueryBranch(result.branch, TaskListsPage{}, &failure, sdkgo.Receipt{})
}
func taskFailureAttempt(result readFailure) sdkgo.QueryAttempt[TasksPage] {
	failure := sdkgo.Failure{Kind: result.kind, Provider: "microsoft-todo", Operation: "listTasks", Message: result.message}
	if result.branch == "" {
		return sdkgo.NewQueryRetry[TasksPage](failure, result.retryAfter)
	}
	return sdkgo.NewQueryBranch(result.branch, TasksPage{}, &failure, sdkgo.Receipt{})
}
