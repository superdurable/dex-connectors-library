// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateTaskOperationID     = "updateTask"
	updateTaskFailureSubject  = "task update"
	sectionMoveFailureSubject = "section move"
)

// UpdateTaskInput changes one task. Every set field is an absolute value, so sending the same update twice
// leaves the same task. Set at least one change.
type UpdateTaskInput struct {
	// TaskID is the task's gid.
	TaskID string `json:"taskId"`
	// IsCompleted completes the task when true and reopens it when false; nil leaves it.
	IsCompleted *bool `json:"isCompleted,omitempty"`
	// AssigneeID assigns the task to me, a user gid, or a workspace member's email address; a pointer to
	// "" unassigns it, and nil leaves the assignee.
	AssigneeID *string `json:"assigneeId,omitempty"`
	// DueOn sets the due date as YYYY-MM-DD; a pointer to "" clears it, and nil leaves it.
	DueOn *string `json:"dueOn,omitempty"`
	// CustomFields sets up to 20 text, number, enum, or multi-enum custom field values.
	CustomFields []CustomFieldValueInput `json:"customFields,omitempty"`
	// SectionID moves the task to this section, removing it from the project's other sections; blank leaves
	// its sections. Asana places a moved task at the top of the section.
	SectionID string `json:"sectionId,omitempty"`
}

// UpdateTaskOutput reports the update. On updated, Task is the task read back after every change.
type UpdateTaskOutput struct {
	// TaskID echoes the requested task.
	TaskID string `json:"taskId"`
	// Task is the task after the update, set only on updated.
	Task *Task `json:"task,omitempty"`
	// IsMovedToSection reports that Asana confirmed the section move, which happens before field changes.
	IsMovedToSection bool `json:"isMovedToSection,omitempty"`
}

// UpdateTaskOperation implements the updateTask Mutation.
type UpdateTaskOperation struct{ client *Client }

type taskUpdate struct {
	taskID    string
	sectionID string
	fields    map[string]any
}

type addTaskToSectionFields struct {
	Task string `json:"task"`
}

// Definition returns the immutable connector operation definition.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Asana needs none: every
// change is an absolute value, so a repeated update converges on the same task.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke moves the task to the requested section, applies the field changes, and returns the task read back.
// Any ambiguous outcome is retried, because resending an absolute change cannot change the task twice.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[UpdateTaskOutput] {
	client := operation.client
	update, err := buildTaskUpdate(input)
	output := UpdateTaskOutput{TaskID: strings.TrimSpace(input.TaskID)}
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, failurePointer(updateTaskOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, updateTaskOperationID)
	if startFailure != nil {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	receipt := client.receipt(session, update.taskID)
	if update.sectionID != "" {
		result := client.exchange(session, asanaRequest{
			method: http.MethodPost, path: joinPath("sections", update.sectionID, "addTask"),
			payload: dataEnvelope[addTaskToSectionFields]{Data: addTaskToSectionFields{Task: update.taskID}},
		})
		if attempt, isFinal := writeAttempt(client.classifyRepeatableWrite(updateTaskOperationID, sectionMoveFailureSubject, result), output, receipt); isFinal {
			return attempt
		}
		output.IsMovedToSection = true
	}
	if len(update.fields) == 0 {
		task, readReceipt, classification := client.readTask(session, updateTaskOperationID, update.taskID)
		if classification.outcome != readSucceeded {
			return readBackAttempt(classification, output, readReceipt)
		}
		output.Task = &task
		return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, output, nil, readReceipt)
	}
	result := client.exchange(session, asanaRequest{
		method: http.MethodPut, path: joinPath("tasks", update.taskID), query: url.Values{"opt_fields": {strings.Join(taskDetailFields, ",")}},
		payload: dataEnvelope[map[string]any]{Data: update.fields},
	})
	if attempt, isFinal := writeAttempt(client.classifyRepeatableWrite(updateTaskOperationID, updateTaskFailureSubject, result), output, receipt); isFinal {
		return attempt
	}
	task, err := decodeTaskBody(result.response.body)
	if err != nil || task.ID != update.taskID {
		return sdkgo.NewMutationBranch(UpdateTaskBranchInvalidResponse, output, failurePointer(updateTaskOperationID, sdkgo.FailureProtocol, "Asana returned an invalid task after the update"), receipt)
	}
	output.Task = &task
	return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, output, nil, receipt)
}

// writeAttempt ends the operation for every outcome except an accepted write.
func writeAttempt(classification writeClassification, output UpdateTaskOutput, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[UpdateTaskOutput], bool) {
	switch classification.outcome {
	case writeAccepted:
		return sdkgo.MutationAttempt[UpdateTaskOutput]{}, false
	case writeRetry:
		return sdkgo.NewMutationRetry[UpdateTaskOutput](classification.failure, classification.retryAfter), true
	case writeNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskBranchNotFound, output, &classification.failure, receipt), true
	case writeRejected:
		return sdkgo.NewMutationBranch(UpdateTaskBranchProviderRejected, output, &classification.failure, receipt), true
	case writeDefect:
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, &classification.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateTaskBranchInvalidResponse, output, &classification.failure, receipt), true
	}
}

// readBackAttempt maps a failed read-back after a section move.
func readBackAttempt(classification readClassification, output UpdateTaskOutput, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTaskOutput] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[UpdateTaskOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskBranchNotFound, output, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, output, &classification.failure, receipt)
	case readRejected:
		return sdkgo.NewMutationBranch(UpdateTaskBranchProviderRejected, output, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateTaskBranchInvalidResponse, output, &classification.failure, receipt)
	}
}

// buildTaskUpdate validates input; fields holds Asana's PUT data with JSON null for a cleared value.
func buildTaskUpdate(input UpdateTaskInput) (taskUpdate, error) {
	taskID, err := validateGID(input.TaskID, "taskId")
	if err != nil {
		return taskUpdate{}, err
	}
	update := taskUpdate{taskID: taskID, fields: map[string]any{}}
	if update.sectionID, err = validateOptionalGID(input.SectionID, "sectionId"); err != nil {
		return taskUpdate{}, err
	}
	if input.IsCompleted != nil {
		update.fields["completed"] = *input.IsCompleted
	}
	if input.AssigneeID != nil {
		if strings.TrimSpace(*input.AssigneeID) == "" {
			update.fields["assignee"] = nil
		} else if update.fields["assignee"], err = validateAssignee(*input.AssigneeID, "assigneeId"); err != nil {
			return taskUpdate{}, err
		}
	}
	if input.DueOn != nil {
		if strings.TrimSpace(*input.DueOn) == "" {
			update.fields["due_on"] = nil
		} else if update.fields["due_on"], err = validateDueDate(*input.DueOn, "dueOn"); err != nil {
			return taskUpdate{}, err
		}
	}
	customFields, err := buildCustomFieldValues(input.CustomFields)
	if err != nil {
		return taskUpdate{}, err
	}
	if customFields != nil {
		update.fields["custom_fields"] = customFields
	}
	if len(update.fields) == 0 && update.sectionID == "" {
		return taskUpdate{}, errors.New("set at least one of isCompleted, assigneeId, dueOn, customFields, and sectionId")
	}
	return update, nil
}
