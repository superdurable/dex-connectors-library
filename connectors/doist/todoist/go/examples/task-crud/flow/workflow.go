// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskcrud demonstrates read-only listing and a disposable task's complete lifecycle.
package taskcrud

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ConnectionName is the static project connection used by every connector Step.
const ConnectionName = "todoist"

// Request selects read-only list mode or lifecycle mode, which creates and deletes its own task.
type Request struct {
	// Mode is list (the default) or lifecycle.
	Mode string `json:"mode,omitempty"`
	// Content is required only for lifecycle mode, as the new task's original title.
	Content string `json:"content,omitempty"`
	// Description is the new task's original description in lifecycle mode.
	Description string `json:"description,omitempty"`
	// UpdateContent optionally replaces the title in lifecycle mode.
	UpdateContent *string `json:"updateContent,omitempty"`
	// UpdateDescription optionally replaces or clears the description in lifecycle mode.
	UpdateDescription *string `json:"updateDescription,omitempty"`
	// ProjectID optionally filters listing or chooses the creation project; blank creates in Inbox.
	ProjectID string `json:"projectId,omitempty"`
	// Cursor continues list mode with unchanged filters.
	Cursor string `json:"cursor,omitempty"`
	// Limit controls list mode page size; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
}

// Outcome contains the returned list page or facts from the completed disposable lifecycle.
type Outcome struct {
	// Status is listed or deletedAndVerified; optional failure branches fail the Flow.
	Status string `json:"status"`
	// Page contains one page in list mode.
	Page todoist.TaskPage `json:"page"`
	// Created contains the task created by lifecycle mode.
	Created todoist.Task `json:"created"`
	// Updated contains the task whose content was updated and found in a list page.
	Updated todoist.Task `json:"updated"`
	// After is the task observed by the post-update read, when available.
	After todoist.Task `json:"after"`
	// MutationBranch preserves the provider update outcome even when read-back matches.
	MutationBranch string `json:"mutationBranch,omitempty"`
	// MatchesRequestedFields reports whether read-back matches every requested replacement.
	MatchesRequestedFields bool `json:"matchesRequestedFields"`
	// DeleteBranch is deleted or alreadyAbsent in lifecycle mode.
	DeleteBranch string `json:"deleteBranch,omitempty"`
}

var (
	requestAttribute   = dex.DefineAttribute[Request]("todoist-crud-request")
	createdAttribute   = dex.DefineAttribute[todoist.CreateTaskResult]("todoist-crud-created")
	updatedAttribute   = dex.DefineAttribute[todoist.UpdateTaskResult]("todoist-crud-updated")
	readBackAttribute  = dex.DefineAttribute[todoist.GetTaskResult]("todoist-crud-read-back")
	deletedAttribute   = dex.DefineAttribute[todoist.DeleteTaskResult]("todoist-crud-deleted")
	pageCountAttribute = dex.DefineAttribute[int]("todoist-crud-page-count")
	outcomeAttribute   = dex.DefineAttribute[Outcome]("todoist-crud-outcome")
)

// TodoistTaskCRUDFlow lists tasks or runs create, get, update, list, delete, and deletion verification.
type TodoistTaskCRUDFlow struct {
	dex.FlowDefaults
	connection todoist.Connection
}

// NewFlow binds the connection at Worker construction.
func NewFlow(connection todoist.Connection) *TodoistTaskCRUDFlow {
	return &TodoistTaskCRUDFlow{connection: connection}
}

// GetSteps registers every checkpoint and connector call.
func (flow *TodoistTaskCRUDFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(StartTaskCRUD{}),
		dex.DefineStep(todoist.NewListTasksStep(todoist.ListTasksStepConfig[todoist.ListTasksInput]{
			StepType:            "ListCurrentTasks",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Read one page without changing any task."},
			MapToOperationInput: func(input todoist.ListTasksInput) todoist.ListTasksInput { return input },
			Listed:              sdkgo.GoTo(CompleteTaskList{}),
		})),
		dex.DefineStep(CompleteTaskList{}),
		dex.DefineStep(todoist.NewCreateTaskStep(todoist.CreateTaskStepConfig[Request]{
			StepType:       "CreateLifecycleTask",
			ConnectionName: ConnectionName,
			Connection:     flow.connection,
			Annotations:    sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Create a new disposable task with a stable provider request ID."},
			MapToOperationInput: func(input Request) todoist.CreateTaskInput {
				return todoist.CreateTaskInput{Content: input.Content, Description: input.Description, ProjectID: input.ProjectID}
			},
			ResultAttribute: &createdAttribute,
			Created:         sdkgo.GoTo(ReadCreatedTask{}),
		})),
		dex.DefineStep(ReadCreatedTask{}),
		dex.DefineStep(todoist.NewGetTaskStep(todoist.GetTaskStepConfig[todoist.GetTaskInput]{
			StepType:            "GetLifecycleTask",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Read the exact task created by this Flow."},
			MapToOperationInput: func(input todoist.GetTaskInput) todoist.GetTaskInput { return input },
			Found:               sdkgo.GoTo(UpdateCreatedTask{}),
		})),
		dex.DefineStep(UpdateCreatedTask{}),
		dex.DefineStep(todoist.NewUpdateTaskStep(todoist.UpdateTaskStepConfig[todoist.UpdateTaskInput]{
			StepType:            "UpdateLifecycleTask",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Update only the specified disposable task fields."},
			MapToOperationInput: func(input todoist.UpdateTaskInput) todoist.UpdateTaskInput { return input },
			ResultAttribute:     &updatedAttribute,
			Updated:             sdkgo.GoTo(ReadUpdatedTask{}),
		})),
		dex.DefineStep(ReadUpdatedTask{}),
		dex.DefineStep(todoist.NewGetTaskStep(todoist.GetTaskStepConfig[todoist.GetTaskInput]{
			StepType:            "GetUpdatedLifecycleTask",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Read back the confirmed update before deleting the task."},
			MapToOperationInput: func(input todoist.GetTaskInput) todoist.GetTaskInput { return input },
			ResultAttribute:     &readBackAttribute,
			Found:               sdkgo.GoTo(CheckTaskReadBack{}),
		})),
		dex.DefineStep(CheckTaskReadBack{}),
		dex.DefineStep(ListCreatedTaskProject{}),
		dex.DefineStep(todoist.NewListTasksStep(todoist.ListTasksStepConfig[todoist.ListTasksInput]{
			StepType:            "ListLifecycleProject",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Read pages of the created task project to verify the update."},
			MapToOperationInput: func(input todoist.ListTasksInput) todoist.ListTasksInput { return input },
			Listed:              sdkgo.GoTo(FindLifecycleTask{}),
		})),
		dex.DefineStep(FindLifecycleTask{}),
		dex.DefineStep(todoist.NewDeleteTaskStep(todoist.DeleteTaskStepConfig[todoist.DeleteTaskInput]{
			StepType:            "DeleteLifecycleTask",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Delete only the task created by this Flow."},
			MapToOperationInput: func(input todoist.DeleteTaskInput) todoist.DeleteTaskInput { return input },
			ResultAttribute:     &deletedAttribute,
			Deleted:             sdkgo.GoTo(VerifyDeletedTask{}),
			// An already absent task satisfies deletion; final read-back still verifies it.
			AlreadyAbsent: sdkgo.GoTo(VerifyDeletedTask{}),
		})),
		dex.DefineStep(VerifyDeletedTask{}),
		dex.DefineStep(todoist.NewGetTaskStep(todoist.GetTaskStepConfig[todoist.GetTaskInput]{
			StepType:            "GetDeletedLifecycleTask",
			ConnectionName:      ConnectionName,
			Connection:          flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "todoist", GroupLabel: "Todoist", Explanation: "Confirm the created task is no longer retrievable."},
			MapToOperationInput: func(input todoist.GetTaskInput) todoist.GetTaskInput { return input },
			// A still retrievable task must fail deletion verification.
			Found: sdkgo.GoTo(FailDeletionVerification{}),
			// A missing task after deletion confirms the disposable lifecycle completed.
			NotFound: sdkgo.GoTo(CompleteTaskLifecycle{}),
		})),
		dex.DefineStep(FailDeletionVerification{}),
		dex.DefineStep(CompleteTaskLifecycle{}),
	}
}

// GetPersistenceSchema registers every durable result and lifecycle fact.
func (*TodoistTaskCRUDFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, createdAttribute, updatedAttribute, readBackAttribute, deletedAttribute, pageCountAttribute, outcomeAttribute}}
}

// GetRPCs registers retained state inspection without provider calls.
func (flow *TodoistTaskCRUDFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary reads the persisted outcome.
// dex:field attribute-key:todoist-crud-outcome value-type:json editable:false description:"List page or disposable lifecycle outcome"
func (*TodoistTaskCRUDFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"todoist-crud-outcome": value}}, nil
}

// GetDexDisplay reads persisted outcomes, including created task identity after a later failure.
// dex:field attribute-key:todoist-crud-created value-type:object editable:false description:"The task created by this Flow"
// dex:field attribute-key:todoist-crud-updated value-type:object editable:false description:"Confirmed mutation result"
// dex:field attribute-key:todoist-crud-read-back value-type:object editable:false description:"Observed task after the update attempt"
// dex:field attribute-key:todoist-crud-outcome value-type:json editable:false description:"List page or lifecycle verification"
func (*TodoistTaskCRUDFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	created, err := optionalAttribute(ctx, createdAttribute)
	if err != nil {
		return nil, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return nil, err
	}
	mutation, err := optionalAttribute(ctx, updatedAttribute)
	if err != nil {
		return nil, err
	}
	readBack, err := optionalAttribute(ctx, readBackAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"todoist-crud-created": created, "todoist-crud-updated": mutation, "todoist-crud-read-back": readBack, "todoist-crud-outcome": outcome}}, nil
}

// StartTaskCRUD fixes the request before choosing read-only listing or creation.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Default to listing; require explicit lifecycle mode for the disposable write test."
type StartTaskCRUD struct{ dex.StepDefaults }

// WaitFor lets Dex Web start the Flow immediately.
func (StartTaskCRUD) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

// Execute persists input and selects the first provider call.
func (StartTaskCRUD) Execute(ctx dex.Context, input Request) (*dex.StepDecision, error) {
	if input.Mode == "" {
		input.Mode = "list"
	}
	if input.Mode != "list" && input.Mode != "lifecycle" || input.Mode == "lifecycle" && strings.TrimSpace(input.Content) == "" {
		return dex.ForceFail("mode must be list or lifecycle; lifecycle requires content"), nil
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Mode == "list" {
		return dex.GoTo(sdkgo.StepRef[todoist.ListTasksInput]("ListCurrentTasks"), todoist.ListTasksInput{ProjectID: input.ProjectID, Cursor: input.Cursor, Limit: input.Limit}), nil
	}
	if err := pageCountAttribute.Set(ctx, 0); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Request]("CreateLifecycleTask"), input), nil
}

// CompleteTaskList retains the one-page result.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Complete the read-only run with tasks and a continuation cursor."
type CompleteTaskList struct {
	dex.StepDefaultsNoWaitFor[todoist.ListTasksResult]
}

// Execute commits the listed page and completes.
func (CompleteTaskList) Execute(ctx dex.Context, result todoist.ListTasksResult) (*dex.StepDecision, error) {
	outcome := Outcome{Status: "listed", Page: result.Value}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// ReadCreatedTask schedules a read using the returned task ID.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Use the create response identity instead of matching a task by title."
type ReadCreatedTask struct {
	dex.StepDefaultsNoWaitFor[todoist.CreateTaskResult]
}

// Execute schedules the exact created task's read.
func (ReadCreatedTask) Execute(_ dex.Context, result todoist.CreateTaskResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todoist.GetTaskInput]("GetLifecycleTask"), todoist.GetTaskInput{TaskID: result.Value.ID}), nil
}

// UpdateCreatedTask verifies the original read and schedules specified field updates.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Check creation read-back before updating the created task."
type UpdateCreatedTask struct {
	dex.StepDefaultsNoWaitFor[todoist.GetTaskResult]
}

// Execute compares the read with the task Todoist returned on creation, which may normalize requested fields, and
// defaults to a title change only when neither update field was specified.
func (UpdateCreatedTask) Execute(ctx dex.Context, result todoist.GetTaskResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	created, err := createdAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Value.Content != created.Value.Content || result.Value.Description != created.Value.Description {
		return dex.ForceFail("created fields did not match"), nil
	}
	input := todoist.UpdateTaskInput{TaskID: result.Value.ID, Content: request.UpdateContent, Description: request.UpdateDescription}
	if input.Content == nil && input.Description == nil {
		content := request.Content + " - updated"
		input.Content = &content
	}
	return dex.GoTo(sdkgo.StepRef[todoist.UpdateTaskInput]("UpdateLifecycleTask"), input), nil
}

// ReadUpdatedTask schedules a read using creation's known identity.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Read the known task ID after the update."
type ReadUpdatedTask struct {
	dex.StepDefaultsNoWaitFor[todoist.UpdateTaskResult]
}

// Execute restores the task identity from its confirmed creation result.
func (ReadUpdatedTask) Execute(ctx dex.Context, _ todoist.UpdateTaskResult) (*dex.StepDecision, error) {
	created, err := createdAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[todoist.GetTaskInput]("GetUpdatedLifecycleTask"), todoist.GetTaskInput{TaskID: created.Value.ID}), nil
}

// CheckTaskReadBack verifies the update against an independent read.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Continue to list and delete only when the updated fields match."
type CheckTaskReadBack struct {
	dex.StepDefaultsNoWaitFor[todoist.GetTaskResult]
}

// Execute compares specified fields before listing and deleting the task.
func (CheckTaskReadBack) Execute(ctx dex.Context, result todoist.GetTaskResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	mutation, err := updatedAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	matches := matchesRequestedFields(request, result.Value)
	if !matches {
		return dex.ForceFail("read-back fields did not match the confirmed update"), nil
	}
	return dex.GoTo(ListCreatedTaskProject{}, mutation), nil
}

// ListCreatedTaskProject schedules the first page in the created task's project.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Verify the update using the list Query."
type ListCreatedTaskProject struct {
	dex.StepDefaultsNoWaitFor[todoist.UpdateTaskResult]
}

// Execute selects the created task's project.
func (ListCreatedTaskProject) Execute(_ dex.Context, result todoist.UpdateTaskResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todoist.ListTasksInput]("ListLifecycleProject"), todoist.ListTasksInput{ProjectID: result.Value.ProjectID, Limit: 200}), nil
}

// FindLifecycleTask follows bounded pagination until it verifies the created task.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Check only the created task, following at most fifty pages before deleting it."
type FindLifecycleTask struct {
	dex.StepDefaultsNoWaitFor[todoist.ListTasksResult]
}

// Execute verifies content or schedules the next page; unrelated tasks are never mutation targets.
func (FindLifecycleTask) Execute(ctx dex.Context, result todoist.ListTasksResult) (*dex.StepDecision, error) {
	updated, err := updatedAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	for _, task := range result.Value.Tasks {
		if task.ID == updated.Value.ID {
			if task.Content != updated.Value.Content || task.Description != updated.Value.Description || task.ProjectID != updated.Value.ProjectID {
				return dex.ForceFail("listed fields did not match"), nil
			}
			return dex.GoTo(sdkgo.StepRef[todoist.DeleteTaskInput]("DeleteLifecycleTask"), todoist.DeleteTaskInput{TaskID: task.ID}), nil
		}
	}
	pages, err := pageCountAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Value.NextCursor == "" || pages >= 49 {
		return dex.ForceFail("created task was not found within the page bound"), nil
	}
	if err := pageCountAttribute.Set(ctx, pages+1); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[todoist.ListTasksInput]("ListLifecycleProject"), todoist.ListTasksInput{ProjectID: updated.Value.ProjectID, Limit: 200, Cursor: result.Value.NextCursor}), nil
}

// VerifyDeletedTask schedules authoritative task lookup after deletion.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Verify deletion by looking up the same immutable task ID."
type VerifyDeletedTask struct {
	dex.StepDefaultsNoWaitFor[todoist.DeleteTaskResult]
}

// Execute reads only the deletion target.
func (VerifyDeletedTask) Execute(_ dex.Context, result todoist.DeleteTaskResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todoist.GetTaskInput]("GetDeletedLifecycleTask"), todoist.GetTaskInput{TaskID: result.Value.TaskID}), nil
}

// FailDeletionVerification fails when Todoist still returns the deletion target.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Do not claim deletion when the task remains retrievable."
type FailDeletionVerification struct {
	dex.StepDefaultsNoWaitFor[todoist.GetTaskResult]
}

// Execute reports the failed invariant without resending deletion.
func (FailDeletionVerification) Execute(dex.Context, todoist.GetTaskResult) (*dex.StepDecision, error) {
	return dex.ForceFail("deleted task remained retrievable"), nil
}

// CompleteTaskLifecycle commits the confirmed disposable lifecycle outcome.
// dex:group group-id:crud group-label:"Task CRUD"
// dex:explanation text:"Retain create, update, and deletion facts after the final not-found lookup."
type CompleteTaskLifecycle struct {
	dex.StepDefaultsNoWaitFor[todoist.GetTaskResult]
}

// Execute completes only after the target task is no longer retrievable.
func (CompleteTaskLifecycle) Execute(ctx dex.Context, _ todoist.GetTaskResult) (*dex.StepDecision, error) {
	created, err := createdAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := updatedAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	deleted, err := deletedAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	readBack, err := readBackAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := Outcome{Status: "deletedAndVerified", After: readBack.Value, MutationBranch: string(updated.Branch), MatchesRequestedFields: true,
		Created: created.Value,
		Updated: updated.Value, DeleteBranch: string(deleted.Branch)}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		var zero T
		return zero, nil
	}
	return value, err
}

func matchesRequestedFields(request Request, task todoist.Task) bool {
	if request.UpdateContent == nil && request.UpdateDescription == nil {
		return task.Content == request.Content+" - updated"
	}
	return (request.UpdateContent == nil || task.Content == *request.UpdateContent) &&
		(request.UpdateDescription == nil || task.Description == *request.UpdateDescription)
}
