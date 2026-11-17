// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTaskListInput selects a list by the ID returned by ListTaskLists.
type GetTaskListInput struct {
	// ListID is a stable non-secret Graph list ID.
	ListID string `json:"listId"`
}

// GetTaskInput selects a task within one list.
type GetTaskInput struct {
	// ListID is a stable non-secret Graph list ID.
	ListID string `json:"listId"`
	// TaskID is a stable non-secret Graph task ID.
	TaskID string `json:"taskId"`
}

// ListLinkedResourcesInput selects a task's linked resources.
type ListLinkedResourcesInput struct {
	// ListID is a stable non-secret Graph list ID.
	ListID string `json:"listId"`
	// TaskID is a stable non-secret Graph task ID.
	TaskID string `json:"taskId"`
}

// GetLinkedResourceInput selects one linked resource within a task.
type GetLinkedResourceInput struct {
	// ListID is a stable non-secret Graph list ID.
	ListID string `json:"listId"`
	// TaskID is a stable non-secret Graph task ID.
	TaskID string `json:"taskId"`
	// ResourceID is a stable non-secret Graph linked-resource ID.
	ResourceID string `json:"resourceId"`
}

// LinkedResource describes a source item associated with a To Do task.
type LinkedResource struct {
	// ID is the server-generated linked-resource ID.
	ID string `json:"id"`
	// DisplayName is the source item's title.
	DisplayName string `json:"displayName"`
	// ApplicationName is the source application's name.
	ApplicationName string `json:"applicationName"`
	// ExternalID identifies the source item in its application.
	ExternalID string `json:"externalId"`
	// WebURL is the source application's deep link, if provided.
	WebURL string `json:"webUrl"`
}

// LinkedResourcesPage contains a complete bounded resource collection.
type LinkedResourcesPage struct {
	// Resources contains every fetched linked resource within the safety limit.
	Resources []LinkedResource `json:"resources"`
}

// GetTaskListOperation reads one To Do list.
type GetTaskListOperation struct{ client *Client }

// GetTaskList binds the Query to this client.
func (client *Client) GetTaskList() GetTaskListOperation { return GetTaskListOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetTaskListOperation) Definition() sdkgo.QueryDefinition { return GetTaskListDefinition }

// Invoke reads one list and classifies the Graph response.
func (operation GetTaskListOperation) Invoke(call sdkgo.Call, input GetTaskListInput) sdkgo.QueryAttempt[TaskList] {
	path, valid := graphResourcePath(input.ListID)
	if !valid {
		return oneFailureAttempt[TaskList]("getTaskList", GetTaskListBranchDefect, sdkgo.FailureValidation, "To Do list ID is required", 0)
	}
	var list TaskList
	failure := operation.client.readOne(call, "/me/todo/lists/"+path, &list)
	if failure != nil {
		return oneReadFailureAttempt[TaskList]("getTaskList", *failure)
	}
	if list.ID == "" || list.DisplayName == "" {
		return oneFailureAttempt[TaskList]("getTaskList", GetTaskListBranchInvalidResponse, sdkgo.FailureProtocol, "Graph returned an invalid task list", 0)
	}
	return sdkgo.NewQueryBranch(GetTaskListBranchRetrieved, list, nil, sdkgo.Receipt{})
}

// GetTaskOperation reads one task.
type GetTaskOperation struct{ client *Client }

// GetTask binds the Query to this client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads one task and classifies the Graph response.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[Task] {
	path, valid := graphTaskPath(input.ListID, input.TaskID)
	if !valid {
		return oneFailureAttempt[Task]("getTask", GetTaskBranchDefect, sdkgo.FailureValidation, "To Do list and task IDs are required", 0)
	}
	var task Task
	failure := operation.client.readOne(call, path, &task)
	if failure != nil {
		return oneReadFailureAttempt[Task]("getTask", *failure)
	}
	if task.ID == "" || task.Title == "" {
		return oneFailureAttempt[Task]("getTask", GetTaskBranchInvalidResponse, sdkgo.FailureProtocol, "Graph returned an invalid task", 0)
	}
	return sdkgo.NewQueryBranch(GetTaskBranchRetrieved, task, nil, sdkgo.Receipt{})
}

// ListLinkedResourcesOperation reads a task's linked resources.
type ListLinkedResourcesOperation struct{ client *Client }

// ListLinkedResources binds the Query to this client.
func (client *Client) ListLinkedResources() ListLinkedResourcesOperation {
	return ListLinkedResourcesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListLinkedResourcesOperation) Definition() sdkgo.QueryDefinition {
	return ListLinkedResourcesDefinition
}

// Invoke reads the bounded linked-resource collection.
func (operation ListLinkedResourcesOperation) Invoke(call sdkgo.Call, input ListLinkedResourcesInput) sdkgo.QueryAttempt[LinkedResourcesPage] {
	path, valid := graphTaskPath(input.ListID, input.TaskID)
	if !valid {
		return oneFailureAttempt[LinkedResourcesPage]("listLinkedResources", ListLinkedResourcesBranchDefect, sdkgo.FailureValidation, "To Do list and task IDs are required", 0)
	}
	records, failure := operation.client.readCollection(call, path+"/linkedResources")
	if failure != nil {
		return oneReadFailureAttempt[LinkedResourcesPage]("listLinkedResources", *failure)
	}
	output := LinkedResourcesPage{Resources: make([]LinkedResource, 0, len(records))}
	for _, record := range records {
		var resource LinkedResource
		if json.Unmarshal(record, &resource) != nil || resource.ID == "" || resource.DisplayName == "" {
			return oneFailureAttempt[LinkedResourcesPage]("listLinkedResources", ListLinkedResourcesBranchInvalidResponse, sdkgo.FailureProtocol, "Graph returned an invalid linked resource", 0)
		}
		output.Resources = append(output.Resources, resource)
	}
	return sdkgo.NewQueryBranch(ListLinkedResourcesBranchListed, output, nil, sdkgo.Receipt{})
}

// GetLinkedResourceOperation reads one linked resource.
type GetLinkedResourceOperation struct{ client *Client }

// GetLinkedResource binds the Query to this client.
func (client *Client) GetLinkedResource() GetLinkedResourceOperation {
	return GetLinkedResourceOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetLinkedResourceOperation) Definition() sdkgo.QueryDefinition {
	return GetLinkedResourceDefinition
}

// Invoke reads one linked resource and classifies the Graph response.
func (operation GetLinkedResourceOperation) Invoke(call sdkgo.Call, input GetLinkedResourceInput) sdkgo.QueryAttempt[LinkedResource] {
	path, valid := graphTaskPath(input.ListID, input.TaskID)
	resourceID, resourceValid := graphResourcePath(input.ResourceID)
	if !valid || !resourceValid {
		return oneFailureAttempt[LinkedResource]("getLinkedResource", GetLinkedResourceBranchDefect, sdkgo.FailureValidation, "To Do list, task, and resource IDs are required", 0)
	}
	var resource LinkedResource
	failure := operation.client.readOne(call, path+"/linkedResources/"+resourceID, &resource)
	if failure != nil {
		return oneReadFailureAttempt[LinkedResource]("getLinkedResource", *failure)
	}
	if resource.ID == "" || resource.DisplayName == "" {
		return oneFailureAttempt[LinkedResource]("getLinkedResource", GetLinkedResourceBranchInvalidResponse, sdkgo.FailureProtocol, "Graph returned an invalid linked resource", 0)
	}
	return sdkgo.NewQueryBranch(GetLinkedResourceBranchRetrieved, resource, nil, sdkgo.Receipt{})
}

func graphResourcePath(identifier string) (string, bool) {
	if strings.TrimSpace(identifier) == "" || strings.ContainsAny(identifier, "\r\n") {
		return "", false
	}
	return url.PathEscape(identifier), true
}

func graphTaskPath(listID, taskID string) (string, bool) {
	listPath, listValid := graphResourcePath(listID)
	taskPath, taskValid := graphResourcePath(taskID)
	return "/me/todo/lists/" + listPath + "/tasks/" + taskPath, listValid && taskValid
}

func (client *Client) readOne(call sdkgo.Call, path string, output any) *readFailure {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return &readFailure{branch: "defect", kind: sdkgo.FailureAuthentication, message: "To Do connection needs authorization"}
		}
		return &readFailure{kind: sdkgo.FailureTransport, message: "To Do credential refresh failed"}
	}
	token := credentials.AccessToken.Reveal()
	if token == "" || !providerhttp.IsHeaderSafeCredential(token) {
		return &readFailure{branch: "defect", kind: sdkgo.FailureAuthentication, message: "To Do access token is unavailable"}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, graphRoot+path, nil)
	if err != nil {
		return &readFailure{branch: "defect", kind: sdkgo.FailureValidation, message: "To Do request could not be built"}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return &readFailure{kind: sdkgo.FailureTransport, message: "Graph request failed"}
	}
	body, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	closeErr := response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return &readFailure{kind: sdkgo.FailureAvailability, message: "Graph is temporarily unavailable", retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	if response.StatusCode == http.StatusNotFound {
		return &readFailure{branch: "notFound", kind: sdkgo.FailureNotFound, message: "To Do resource was not found"}
	}
	if response.StatusCode != http.StatusOK {
		return &readFailure{branch: "providerRejected", kind: sdkgo.FailureProviderRejection, message: "Graph rejected the request"}
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return &readFailure{branch: "invalidResponse", kind: sdkgo.FailureResponseTooLarge, message: "Graph response exceeded the limit"}
	}
	if readErr != nil || closeErr != nil {
		return &readFailure{kind: sdkgo.FailureTransport, message: "Graph response was interrupted"}
	}
	if json.Unmarshal(body, output) != nil {
		return &readFailure{branch: "invalidResponse", kind: sdkgo.FailureProtocol, message: "Graph returned invalid JSON"}
	}
	return nil
}

func oneReadFailureAttempt[T any](operation string, result readFailure) sdkgo.QueryAttempt[T] {
	return oneFailureAttempt[T](operation, result.branch, result.kind, result.message, result.retryAfter)
}

func oneFailureAttempt[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "microsoft-todo", Operation: operation, Message: message}
	if branch == "" {
		return sdkgo.NewQueryRetry[T](failure, retryAfter)
	}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
