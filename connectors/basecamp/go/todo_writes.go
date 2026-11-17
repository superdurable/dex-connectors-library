// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTodoInput supplies a to-do list and its initial fields.
type CreateTodoInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoListID comes from the project's to-do set.
	TodoListID string `json:"todoListId"`
	// Content is a non-blank to-do title.
	Content string `json:"content"`
	// Description is optional rich text.
	Description string `json:"description,omitempty"`
	// DueOn is an optional YYYY-MM-DD due date.
	DueOn string `json:"dueOn,omitempty"`
	// StartsOn is an optional YYYY-MM-DD start date.
	StartsOn string `json:"startsOn,omitempty"`
	// AssigneeIDs are Basecamp person IDs.
	AssigneeIDs []string `json:"assigneeIds,omitempty"`
	// CompletionSubscriberIDs are people notified on completion.
	CompletionSubscriberIDs []string `json:"completionSubscriberIds,omitempty"`
}

// UpdateTodoInput supplies the complete editable to-do state.
// Passing empty lists clears assignments and subscribers.
type UpdateTodoInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoID comes from getTodo or listTodos.
	TodoID string `json:"todoId"`
	// Content is the replacement title.
	Content string `json:"content"`
	// Description is replacement rich text; blank clears it.
	Description string `json:"description"`
	// DueOn is an optional YYYY-MM-DD date; blank clears it.
	DueOn string `json:"dueOn"`
	// StartsOn is an optional YYYY-MM-DD date; blank clears it.
	StartsOn string `json:"startsOn"`
	// AssigneeIDs is the complete person ID list; an empty list clears it.
	AssigneeIDs []string `json:"assigneeIds"`
	// CompletionSubscriberIDs is the complete notification list; empty clears it.
	CompletionSubscriberIDs []string `json:"completionSubscriberIds"`
}

// RepositionTodoInput sets one to-do's absolute position and optional new parent.
type RepositionTodoInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoID comes from getTodo or listTodos.
	TodoID string `json:"todoId"`
	// Position is one or greater.
	Position int `json:"position"`
	// ParentID is a to-do list or to-do set ID in the same project; blank retains the parent.
	ParentID string `json:"parentId,omitempty"`
}

// TodoActionResult identifies the to-do and requested state change.
type TodoActionResult struct {
	// TodoID is the selected to-do.
	TodoID string `json:"todoId"`
	// Status names the applied action.
	Status string `json:"status"`
}

type todoWriteFields struct {
	Content                 string  `json:"content"`
	Description             string  `json:"description,omitempty"`
	DueOn                   string  `json:"due_on,omitempty"`
	StartsOn                string  `json:"starts_on,omitempty"`
	AssigneeIDs             []int64 `json:"assignee_ids"`
	CompletionSubscriberIDs []int64 `json:"completion_subscriber_ids"`
}

// CreateTodoOperation creates a to-do with bounded possible duplicate sends.
type CreateTodoOperation struct{ client *Client }

// CreateTodo binds the to-do creation Mutation.
func (client *Client) CreateTodo() CreateTodoOperation { return CreateTodoOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateTodoOperation) Definition() sdkgo.MutationDefinition { return CreateTodoDefinition }

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (CreateTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one to-do with a bounded send count.
func (operation CreateTodoOperation) Invoke(call sdkgo.Call, input CreateTodoInput) sdkgo.MutationAttempt[Todo] {
	fields, valid := todoFields(input.Content, input.Description, input.DueOn, input.StartsOn, input.AssigneeIDs, input.CompletionSubscriberIDs)
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) || !valid {
		return projectMutationBranch[Todo]("createTodo", CreateTodoBranchDefect, sdkgo.FailureValidation, "to-do input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateTodoDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[Todo]("createTodo", CreateTodoBranchDefect, sdkgo.FailureLocalDefect, "to-do dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[Todo]("createTodo", "to-do creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/todolists/"+input.TodoListID+"/todos.json", fields)
		return nil
	})
	if err != nil {
		return projectMutationRetry[Todo]("createTodo", sdkgo.FailureAvailability, "to-do dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[Todo]("createTodo", sdkgo.FailureAvailability, "to-do resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[Todo]("createTodo", kind, "Basecamp temporarily rejected to-do creation", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Todo]("createTodo", "an earlier to-do creation may have completed")
		}
		return projectMutationBranch[Todo]("createTodo", CreateTodoBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[Todo]("createTodo", sdkgo.FailureTransport, "to-do creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Todo]("createTodo", "an earlier to-do creation may have completed")
		}
		return projectMutationBranch[Todo]("createTodo", CreateTodoBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected to-do creation")
	}
	todo, err := decodeTodo(response.body, "")
	if err != nil || todo.Content != input.Content {
		return projectMutationRetry[Todo]("createTodo", sdkgo.FailureProtocol, "Basecamp returned invalid created to-do JSON", 0)
	}
	return sdkgo.NewMutationBranch(CreateTodoBranchCreated, todo, nil, sdkgo.Receipt{})
}

// UpdateTodoOperation replaces every editable to-do field.
type UpdateTodoOperation struct{ client *Client }

// UpdateTodo binds the to-do replacement Mutation.
func (client *Client) UpdateTodo() UpdateTodoOperation { return UpdateTodoOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateTodoOperation) Definition() sdkgo.MutationDefinition { return UpdateTodoDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces every editable field so Basecamp does not silently clear omitted fields.
func (operation UpdateTodoOperation) Invoke(call sdkgo.Call, input UpdateTodoInput) sdkgo.MutationAttempt[Todo] {
	fields, valid := todoFields(input.Content, input.Description, input.DueOn, input.StartsOn, input.AssigneeIDs, input.CompletionSubscriberIDs)
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoID) || input.AssigneeIDs == nil || input.CompletionSubscriberIDs == nil || !valid {
		return projectMutationBranch[Todo]("updateTodo", UpdateTodoBranchDefect, sdkgo.FailureValidation, "complete to-do state is required")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/todos/"+input.TodoID+".json", fields)
	if attempt, done := projectMutationResponse[Todo](response, "updateTodo", http.StatusOK, UpdateTodoBranchNotFound, UpdateTodoBranchProviderRejected, UpdateTodoBranchDefect, UpdateTodoBranchInvalidResponse); done {
		return attempt
	}
	todo, err := decodeTodo(response.body, input.TodoID)
	if err != nil || todo.Content != input.Content {
		return projectMutationBranch[Todo]("updateTodo", UpdateTodoBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid updated to-do JSON")
	}
	return sdkgo.NewMutationBranch(UpdateTodoBranchUpdated, todo, nil, sdkgo.Receipt{})
}

// CompleteTodoOperation marks a to-do complete.
type CompleteTodoOperation struct{ client *Client }

// CompleteTodo binds the completion Mutation.
func (client *Client) CompleteTodo() CompleteTodoOperation { return CompleteTodoOperation{client} }

// Definition returns the generated Mutation contract.
func (CompleteTodoOperation) Definition() sdkgo.MutationDefinition { return CompleteTodoDefinition }

// IdempotencyKey records the stable Dex call identity.
func (CompleteTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke marks the selected to-do complete.
func (operation CompleteTodoOperation) Invoke(call sdkgo.Call, input GetTodoInput) sdkgo.MutationAttempt[TodoActionResult] {
	return operation.client.changeTodoState(call, input, http.MethodPost, "/todos/", "/completion.json", "completeTodo", "completed", CompleteTodoBranchCompleted, CompleteTodoBranchNotFound, CompleteTodoBranchProviderRejected, CompleteTodoBranchDefect)
}

// UncompleteTodoOperation reopens a completed to-do.
type UncompleteTodoOperation struct{ client *Client }

// UncompleteTodo binds the reopen Mutation.
func (client *Client) UncompleteTodo() UncompleteTodoOperation {
	return UncompleteTodoOperation{client}
}

// Definition returns the generated Mutation contract.
func (UncompleteTodoOperation) Definition() sdkgo.MutationDefinition { return UncompleteTodoDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UncompleteTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reopens the selected to-do.
func (operation UncompleteTodoOperation) Invoke(call sdkgo.Call, input GetTodoInput) sdkgo.MutationAttempt[TodoActionResult] {
	return operation.client.changeTodoState(call, input, http.MethodDelete, "/todos/", "/completion.json", "uncompleteTodo", "active", UncompleteTodoBranchActive, UncompleteTodoBranchNotFound, UncompleteTodoBranchProviderRejected, UncompleteTodoBranchDefect)
}

// TrashTodoOperation trashes a to-do recording.
type TrashTodoOperation struct{ client *Client }

// TrashTodo binds the trash Mutation.
func (client *Client) TrashTodo() TrashTodoOperation { return TrashTodoOperation{client} }

// Definition returns the generated Mutation contract.
func (TrashTodoOperation) Definition() sdkgo.MutationDefinition { return TrashTodoDefinition }

// IdempotencyKey records the stable Dex call identity.
func (TrashTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke trashes the selected to-do recording.
func (operation TrashTodoOperation) Invoke(call sdkgo.Call, input GetTodoInput) sdkgo.MutationAttempt[TodoActionResult] {
	return operation.client.changeTodoState(call, input, http.MethodPut, "/recordings/", "/status/trashed.json", "trashTodo", "trashed", TrashTodoBranchTrashed, TrashTodoBranchNotFound, TrashTodoBranchProviderRejected, TrashTodoBranchDefect)
}

// RepositionTodoOperation sets a to-do's absolute position and optional parent.
type RepositionTodoOperation struct{ client *Client }

// RepositionTodo binds the reposition Mutation.
func (client *Client) RepositionTodo() RepositionTodoOperation {
	return RepositionTodoOperation{client}
}

// Definition returns the generated Mutation contract.
func (RepositionTodoOperation) Definition() sdkgo.MutationDefinition { return RepositionTodoDefinition }

// IdempotencyKey records the stable Dex call identity.
func (RepositionTodoOperation) IdempotencyKey(callID sdkgo.CallID, _ RepositionTodoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the selected to-do's absolute position.
func (operation RepositionTodoOperation) Invoke(call sdkgo.Call, input RepositionTodoInput) sdkgo.MutationAttempt[TodoActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoID) || input.Position < 1 || (input.ParentID != "" && !validResourceID(input.ParentID)) {
		return projectMutationBranch[TodoActionResult]("repositionTodo", RepositionTodoBranchDefect, sdkgo.FailureValidation, "to-do position input is invalid")
	}
	request := struct {
		Position int    `json:"position"`
		ParentID *int64 `json:"parent_id,omitempty"`
	}{Position: input.Position}
	if input.ParentID != "" {
		parentID, _ := strconv.ParseInt(input.ParentID, 10, 64)
		request.ParentID = &parentID
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/todos/"+input.TodoID+"/position.json", request)
	if attempt, done := projectMutationResponse[TodoActionResult](response, "repositionTodo", http.StatusNoContent, RepositionTodoBranchNotFound, RepositionTodoBranchProviderRejected, RepositionTodoBranchDefect, RepositionTodoBranchProviderRejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(RepositionTodoBranchRepositioned, TodoActionResult{TodoID: input.TodoID, Status: "repositioned"}, nil, sdkgo.Receipt{})
}

func (client *Client) changeTodoState(call sdkgo.Call, input GetTodoInput, method, prefix, suffix, operation, status string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[TodoActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoID) {
		return projectMutationBranch[TodoActionResult](operation, defect, sdkgo.FailureValidation, "to-do IDs are invalid")
	}
	response := client.accountMutation(call, call.Context, method, input.AccountID, prefix+input.TodoID+suffix, nil)
	if attempt, done := projectMutationResponse[TodoActionResult](response, operation, http.StatusNoContent, notFound, rejected, defect, rejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(success, TodoActionResult{TodoID: input.TodoID, Status: status}, nil, sdkgo.Receipt{})
}

func todoFields(content, description, dueOn, startsOn string, assigneeIDs, subscriberIDs []string) (todoWriteFields, bool) {
	if strings.TrimSpace(content) == "" || len(content) > 512 || len(description) > 10000 || !validTodoDate(dueOn) || !validTodoDate(startsOn) {
		return todoWriteFields{}, false
	}
	fields := todoWriteFields{Content: content, Description: description, DueOn: dueOn, StartsOn: startsOn, AssigneeIDs: make([]int64, 0, len(assigneeIDs)), CompletionSubscriberIDs: make([]int64, 0, len(subscriberIDs))}
	for _, value := range assigneeIDs {
		if !validResourceID(value) {
			return todoWriteFields{}, false
		}
		id, _ := strconv.ParseInt(value, 10, 64)
		fields.AssigneeIDs = append(fields.AssigneeIDs, id)
	}
	for _, value := range subscriberIDs {
		if !validResourceID(value) {
			return todoWriteFields{}, false
		}
		id, _ := strconv.ParseInt(value, 10, 64)
		fields.CompletionSubscriberIDs = append(fields.CompletionSubscriberIDs, id)
	}
	return fields, true
}

func validTodoDate(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}
