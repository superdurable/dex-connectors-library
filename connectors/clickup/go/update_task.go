// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateTaskOperation = "updateTask"

// UpdateTaskInput changes one task. Only set fields change; every change is an absolute value or a set
// membership, so a repeated update leaves the same task.
type UpdateTaskInput struct {
	// TaskID is ClickUp's task ID.
	TaskID string `json:"taskId"`
	// Name renames the task when set.
	Name *string `json:"name,omitempty"`
	// MarkdownDescription replaces the description with Markdown when set; a pointer to "" clears it.
	MarkdownDescription *string `json:"markdownDescription,omitempty"`
	// Status moves the task to a status name of its List's workflow, such as in progress; empty keeps it.
	Status string `json:"status,omitempty"`
	// Priority sets 1 urgent through 4 low; zero keeps the current priority.
	Priority TaskPriority `json:"priority,omitempty"`
	// DueAt sets the due date and time; nil keeps it.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// AddAssigneeIDs assigns these users; an existing assignee stays assigned.
	AddAssigneeIDs []int64 `json:"addAssigneeIds,omitempty"`
	// RemoveAssigneeIDs unassigns these users; a user who is not assigned is ignored.
	RemoveAssigneeIDs []int64 `json:"removeAssigneeIds,omitempty"`
}

// UpdateTaskOutput is the task after the update.
type UpdateTaskOutput struct {
	// Task is the task ClickUp returned after applying the change.
	Task Task `json:"task"`
}

// UpdateTaskOperation is the updateTask Mutation.
type UpdateTaskOperation struct {
	client *Client
}

type updateTaskWire struct {
	Name            *string              `json:"name,omitempty"`
	Description     *string              `json:"description,omitempty"`
	MarkdownContent *string              `json:"markdown_content,omitempty"`
	Status          string               `json:"status,omitempty"`
	Priority        int                  `json:"priority,omitempty"`
	DueDate         int64                `json:"due_date,omitempty"`
	DueDateTime     bool                 `json:"due_date_time,omitempty"`
	Assignees       *assigneeChangesWire `json:"assignees,omitempty"`
}

type assigneeChangesWire struct {
	Add    []int64 `json:"add"`
	Remove []int64 `json:"rem"`
}

// Definition returns the immutable connector operation definition.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey uses the stable connector Call ID; the update itself is safe to repeat.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends PUT /task/{task_id}, whose response is the updated task. Because a repeated PUT leaves
// the same task, every ambiguous outcome, including a timeout after dispatch or a 5xx, retries.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[UpdateTaskOutput] {
	output := UpdateTaskOutput{}
	if err := validateUpdateTaskInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, updateTaskOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateTaskOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call.Context, credentials, updateTaskOperation, clickupRequest{
		method: http.MethodPut, path: taskPath(input.TaskID), payload: updateTaskPayload(input),
	})
	receipt := operation.client.receipt(call, input.TaskID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[UpdateTaskOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateTaskBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateTaskBranchProviderRejected, output, &result.failure, receipt)
	}
	task, err := decodeTaskBody(result.response.body, false)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTaskBranchInvalidResponse, output, clickupFailurePointer(sdkgo.FailureProtocol, updateTaskOperation,
			"ClickUp accepted the update but returned an invalid task: "+err.Error()), receipt)
	}
	output.Task = task
	return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, output, nil, receipt)
}

func updateTaskPayload(input UpdateTaskInput) updateTaskWire {
	payload := updateTaskWire{Name: input.Name, Status: input.Status, Priority: int(input.Priority)}
	if input.MarkdownDescription != nil {
		if *input.MarkdownDescription == "" {
			// ClickUp documents a single space as the value that clears a description.
			clearedDescription := " "
			payload.Description = &clearedDescription
		} else {
			payload.MarkdownContent = input.MarkdownDescription
		}
	}
	if input.DueAt != nil {
		payload.DueDate, payload.DueDateTime = input.DueAt.UnixMilli(), true
	}
	if len(input.AddAssigneeIDs) != 0 || len(input.RemoveAssigneeIDs) != 0 {
		payload.Assignees = &assigneeChangesWire{Add: nonNilUserIDs(input.AddAssigneeIDs), Remove: nonNilUserIDs(input.RemoveAssigneeIDs)}
	}
	return payload
}

func validateUpdateTaskInput(input UpdateTaskInput) error {
	if err := validateTaskID("taskId", input.TaskID); err != nil {
		return err
	}
	isChanging := input.Name != nil || input.MarkdownDescription != nil || input.Status != "" || input.Priority != 0 ||
		input.DueAt != nil || len(input.AddAssigneeIDs) != 0 || len(input.RemoveAssigneeIDs) != 0
	if !isChanging {
		return errors.New("updateTask needs at least one change")
	}
	if input.Name != nil {
		if err := validateTaskName("name", *input.Name); err != nil {
			return err
		}
	}
	if input.MarkdownDescription != nil && (len(*input.MarkdownDescription) > MaxDescriptionBytes || !utf8.ValidString(*input.MarkdownDescription)) {
		return fmt.Errorf("markdownDescription must be valid UTF-8 of at most %d bytes", MaxDescriptionBytes)
	}
	if err := validateStatusName(input.Status); err != nil {
		return err
	}
	if err := validatePriority(input.Priority); err != nil {
		return err
	}
	if input.DueAt != nil && input.DueAt.UnixMilli() <= 0 {
		return errors.New("dueAt must be after the Unix epoch")
	}
	if len(input.AddAssigneeIDs)+len(input.RemoveAssigneeIDs) > MaxAssignees {
		return fmt.Errorf("addAssigneeIds and removeAssigneeIds accept at most %d users together", MaxAssignees)
	}
	if err := validateUserIDs("addAssigneeIds", input.AddAssigneeIDs); err != nil {
		return err
	}
	if err := validateUserIDs("removeAssigneeIds", input.RemoveAssigneeIDs); err != nil {
		return err
	}
	if containsDuplicate(append(append([]int64(nil), input.AddAssigneeIDs...), input.RemoveAssigneeIDs...)) {
		return errors.New("a user cannot be both added and removed")
	}
	return nil
}

// nonNilUserIDs keeps ClickUp's required add and rem arrays present even when empty.
func nonNilUserIDs(userIDs []int64) []int64 {
	if userIDs == nil {
		return []int64{}
	}
	return userIDs
}
