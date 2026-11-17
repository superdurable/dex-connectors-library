// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// TodoList contains one Basecamp to-do list identity and display fields.
type TodoList struct {
	// ID is the numeric to-do list ID.
	ID string `json:"id"`
	// Name is the to-do list title.
	Name string `json:"name"`
	// Description is optional rich text.
	Description string `json:"description,omitempty"`
}

// TodoListsPage contains one provider page of to-do lists.
type TodoListsPage struct {
	// TodoLists are the visible lists on this page.
	TodoLists []TodoList `json:"todoLists"`
}

// ListTodoListsInput identifies a to-do set.
type ListTodoListsInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoSetID comes from getProject or the project dock.
	TodoSetID string `json:"todoSetId"`
}

// GetTodoListInput identifies one to-do list.
type GetTodoListInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoListID comes from listTodoLists or a Basecamp URL.
	TodoListID string `json:"todoListId"`
}

// CreateTodoListInput supplies a to-do set and initial title.
type CreateTodoListInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoSetID comes from getProject.
	TodoSetID string `json:"todoSetId"`
	// Name is the non-blank list title.
	Name string `json:"name"`
	// Description is optional rich text.
	Description string `json:"description,omitempty"`
}

// UpdateTodoListInput replaces the title and description.
type UpdateTodoListInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoListID comes from listTodoLists.
	TodoListID string `json:"todoListId"`
	// Name is the non-blank replacement title.
	Name string `json:"name"`
	// Description is replacement rich text; blank clears it.
	Description string `json:"description"`
}

// RepositionTodoListInput sets a list's one-based position.
type RepositionTodoListInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoListID comes from listTodoLists.
	TodoListID string `json:"todoListId"`
	// Position counts incomplete lists and starts at one.
	Position int `json:"position"`
}

// TodoListActionResult identifies the list and applied action.
type TodoListActionResult struct {
	// TodoListID is the selected to-do list.
	TodoListID string `json:"todoListId"`
	// Status names the applied action.
	Status string `json:"status"`
}

// ListTodoListsOperation reads one page of active lists.
type ListTodoListsOperation struct{ client *Client }

// ListTodoLists binds the list Query.
func (client *Client) ListTodoLists() ListTodoListsOperation { return ListTodoListsOperation{client} }

// Definition returns the generated Query contract.
func (ListTodoListsOperation) Definition() sdkgo.QueryDefinition { return ListTodoListsDefinition }

// Invoke reads active lists from the selected to-do set.
func (operation ListTodoListsOperation) Invoke(call sdkgo.Call, input ListTodoListsInput) sdkgo.QueryAttempt[TodoListsPage] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoSetID) {
		return resourceBranch[TodoListsPage]("listTodoLists", ListTodoListsBranchDefect, sdkgo.FailureValidation, "to-do set IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/todosets/"+input.TodoSetID+"/todolists.json")
	return resourceQueryAttempt(response, "listTodoLists", ListTodoListsBranchListed, "", ListTodoListsBranchProviderRejected, ListTodoListsBranchInvalidResponse, ListTodoListsBranchDefect, decodeTodoLists)
}

// GetTodoListOperation reads one to-do list.
type GetTodoListOperation struct{ client *Client }

// GetTodoList binds the singular Query.
func (client *Client) GetTodoList() GetTodoListOperation { return GetTodoListOperation{client} }

// Definition returns the generated Query contract.
func (GetTodoListOperation) Definition() sdkgo.QueryDefinition { return GetTodoListDefinition }

// Invoke reads the selected to-do list.
func (operation GetTodoListOperation) Invoke(call sdkgo.Call, input GetTodoListInput) sdkgo.QueryAttempt[TodoList] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) {
		return resourceBranch[TodoList]("getTodoList", GetTodoListBranchDefect, sdkgo.FailureValidation, "to-do list IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/todolists/"+input.TodoListID+".json")
	return resourceQueryAttempt(response, "getTodoList", GetTodoListBranchFound, GetTodoListBranchNotFound, GetTodoListBranchProviderRejected, GetTodoListBranchInvalidResponse, GetTodoListBranchDefect, func(body []byte) (TodoList, error) { return decodeTodoList(body, input.TodoListID) })
}

// CreateTodoListOperation creates one list with a bounded send count.
type CreateTodoListOperation struct{ client *Client }

// CreateTodoList binds the list creation Mutation.
func (client *Client) CreateTodoList() CreateTodoListOperation {
	return CreateTodoListOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateTodoListOperation) Definition() sdkgo.MutationDefinition { return CreateTodoListDefinition }

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (CreateTodoListOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTodoListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a list and bounds possibly repeated sends.
func (operation CreateTodoListOperation) Invoke(call sdkgo.Call, input CreateTodoListInput) sdkgo.MutationAttempt[TodoList] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoSetID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 || len(input.Description) > 10000 {
		return projectMutationBranch[TodoList]("createTodoList", CreateTodoListBranchDefect, sdkgo.FailureValidation, "to-do list input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateTodoListDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[TodoList]("createTodoList", CreateTodoListBranchDefect, sdkgo.FailureLocalDefect, "list dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[TodoList]("createTodoList", "list creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/todosets/"+input.TodoSetID+"/todolists.json", struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		}{input.Name, input.Description})
		return nil
	})
	if err != nil {
		return projectMutationRetry[TodoList]("createTodoList", sdkgo.FailureAvailability, "list dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[TodoList]("createTodoList", sdkgo.FailureAvailability, "list resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[TodoList]("createTodoList", kind, "Basecamp temporarily rejected list creation", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[TodoList]("createTodoList", "an earlier list creation may have completed")
		}
		return projectMutationBranch[TodoList]("createTodoList", CreateTodoListBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[TodoList]("createTodoList", sdkgo.FailureTransport, "list creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[TodoList]("createTodoList", "an earlier list creation may have completed")
		}
		return projectMutationBranch[TodoList]("createTodoList", CreateTodoListBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected list creation")
	}
	list, err := decodeTodoList(response.body, "")
	if err != nil || list.Name != input.Name {
		return projectMutationRetry[TodoList]("createTodoList", sdkgo.FailureProtocol, "Basecamp returned invalid created list JSON", 0)
	}
	return sdkgo.NewMutationBranch(CreateTodoListBranchCreated, list, nil, sdkgo.Receipt{})
}

// UpdateTodoListOperation replaces a list's title and description.
type UpdateTodoListOperation struct{ client *Client }

// UpdateTodoList binds the list update Mutation.
func (client *Client) UpdateTodoList() UpdateTodoListOperation {
	return UpdateTodoListOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateTodoListOperation) Definition() sdkgo.MutationDefinition { return UpdateTodoListDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateTodoListOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTodoListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the selected list's title and description.
func (operation UpdateTodoListOperation) Invoke(call sdkgo.Call, input UpdateTodoListInput) sdkgo.MutationAttempt[TodoList] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 || len(input.Description) > 10000 {
		return projectMutationBranch[TodoList]("updateTodoList", UpdateTodoListBranchDefect, sdkgo.FailureValidation, "to-do list input is invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/todolists/"+input.TodoListID+".json", struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{input.Name, input.Description})
	if attempt, done := projectMutationResponse[TodoList](response, "updateTodoList", http.StatusOK, UpdateTodoListBranchNotFound, UpdateTodoListBranchProviderRejected, UpdateTodoListBranchDefect, UpdateTodoListBranchInvalidResponse); done {
		return attempt
	}
	list, err := decodeTodoList(response.body, input.TodoListID)
	if err != nil || list.Name != input.Name {
		return projectMutationBranch[TodoList]("updateTodoList", UpdateTodoListBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid updated list JSON")
	}
	return sdkgo.NewMutationBranch(UpdateTodoListBranchUpdated, list, nil, sdkgo.Receipt{})
}

// RepositionTodoListOperation sets a list's absolute position.
type RepositionTodoListOperation struct{ client *Client }

// RepositionTodoList binds the list reposition Mutation.
func (client *Client) RepositionTodoList() RepositionTodoListOperation {
	return RepositionTodoListOperation{client}
}

// Definition returns the generated Mutation contract.
func (RepositionTodoListOperation) Definition() sdkgo.MutationDefinition {
	return RepositionTodoListDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (RepositionTodoListOperation) IdempotencyKey(callID sdkgo.CallID, _ RepositionTodoListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the selected list's absolute position.
func (operation RepositionTodoListOperation) Invoke(call sdkgo.Call, input RepositionTodoListInput) sdkgo.MutationAttempt[TodoListActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) || input.Position < 1 {
		return projectMutationBranch[TodoListActionResult]("repositionTodoList", RepositionTodoListBranchDefect, sdkgo.FailureValidation, "to-do list position is invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/todosets/todolists/"+input.TodoListID+"/position.json", struct {
		Position int `json:"position"`
	}{input.Position})
	if attempt, done := projectMutationResponse[TodoListActionResult](response, "repositionTodoList", http.StatusNoContent, RepositionTodoListBranchNotFound, RepositionTodoListBranchProviderRejected, RepositionTodoListBranchDefect, RepositionTodoListBranchProviderRejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(RepositionTodoListBranchRepositioned, TodoListActionResult{TodoListID: input.TodoListID, Status: "repositioned"}, nil, sdkgo.Receipt{})
}

// TrashTodoListOperation trashes a list recording.
type TrashTodoListOperation struct{ client *Client }

// TrashTodoList binds the list trash Mutation.
func (client *Client) TrashTodoList() TrashTodoListOperation { return TrashTodoListOperation{client} }

// Definition returns the generated Mutation contract.
func (TrashTodoListOperation) Definition() sdkgo.MutationDefinition { return TrashTodoListDefinition }

// IdempotencyKey records the stable Dex call identity.
func (TrashTodoListOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTodoListInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke trashes the selected list recording.
func (operation TrashTodoListOperation) Invoke(call sdkgo.Call, input GetTodoListInput) sdkgo.MutationAttempt[TodoListActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) {
		return projectMutationBranch[TodoListActionResult]("trashTodoList", TrashTodoListBranchDefect, sdkgo.FailureValidation, "to-do list IDs are invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/recordings/"+input.TodoListID+"/status/trashed.json", nil)
	if attempt, done := projectMutationResponse[TodoListActionResult](response, "trashTodoList", http.StatusNoContent, TrashTodoListBranchNotFound, TrashTodoListBranchProviderRejected, TrashTodoListBranchDefect, TrashTodoListBranchProviderRejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(TrashTodoListBranchTrashed, TodoListActionResult{TodoListID: input.TodoListID, Status: "trashed"}, nil, sdkgo.Receipt{})
}

func decodeTodoLists(body []byte) (TodoListsPage, error) {
	var records []struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return TodoListsPage{}, fmt.Errorf("to-do lists are invalid")
	}
	page := TodoListsPage{TodoLists: make([]TodoList, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || strings.TrimSpace(record.Title) == "" {
			return TodoListsPage{}, fmt.Errorf("to-do list is invalid")
		}
		page.TodoLists = append(page.TodoLists, TodoList{ID: strconv.FormatInt(record.ID, 10), Name: record.Title, Description: record.Description})
	}
	return page, nil
}

func decodeTodoList(body []byte, expectedID string) (TodoList, error) {
	var record struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || strings.TrimSpace(record.Title) == "" {
		return TodoList{}, fmt.Errorf("to-do list is invalid")
	}
	return TodoList{ID: strconv.FormatInt(record.ID, 10), Name: record.Title, Description: record.Description}, nil
}
