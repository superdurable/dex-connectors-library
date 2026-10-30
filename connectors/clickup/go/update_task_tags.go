// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxTagChanges bounds the tags one updateTaskTags adds and removes together.
	MaxTagChanges = 10

	updateTaskTagsOperation = "updateTaskTags"
)

// UpdateTaskTagsInput adds tags to and removes tags from one task.
type UpdateTaskTagsInput struct {
	// TaskID is ClickUp's task ID.
	TaskID string `json:"taskId"`
	// AddTags are tag names to add; a tag the task already has stays.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags are tag names to remove from the task; the tag stays in the Space.
	RemoveTags []string `json:"removeTags,omitempty"`
}

// UpdateTaskTagsOutput is the task's tags after the update.
type UpdateTaskTagsOutput struct {
	// TaskID is the updated task.
	TaskID string `json:"taskId"`
	// Tags are the task's tag names read back after every change, as ClickUp stores them.
	Tags []string `json:"tags"`
	// AppliedChanges counts the tag requests ClickUp accepted, also on providerRejected.
	AppliedChanges int `json:"appliedChanges"`
}

// UpdateTaskTagsOperation is the updateTaskTags Mutation.
type UpdateTaskTagsOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (UpdateTaskTagsOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskTagsDefinition }

// IdempotencyKey uses the stable connector Call ID; adding or removing a tag is safe to repeat.
func (UpdateTaskTagsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskTagsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /task/{task_id}/tag/{tag_name} for each added tag and DELETE for each removed tag,
// then reads the task back with GET /task/{task_id}. Every ambiguous outcome retries the whole set.
func (operation UpdateTaskTagsOperation) Invoke(call sdkgo.Call, input UpdateTaskTagsInput) sdkgo.MutationAttempt[UpdateTaskTagsOutput] {
	output := UpdateTaskTagsOutput{TaskID: input.TaskID, Tags: []string{}}
	if err := validateUpdateTaskTagsInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, updateTaskTagsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateTaskTagsOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchDefect, output, failure, sdkgo.Receipt{})
	}
	ctx, cancel := withOperationBudget(call)
	defer cancel()
	receipt := operation.client.receipt(call, input.TaskID)
	changes := make([]clickupRequest, 0, len(input.AddTags)+len(input.RemoveTags))
	for _, tag := range input.AddTags {
		changes = append(changes, clickupRequest{method: http.MethodPost, path: taskPath(input.TaskID) + "/tag/" + url.PathEscape(tag)})
	}
	for _, tag := range input.RemoveTags {
		changes = append(changes, clickupRequest{method: http.MethodDelete, path: taskPath(input.TaskID) + "/tag/" + url.PathEscape(tag)})
	}
	for _, change := range changes {
		result := operation.client.exchange(ctx, credentials, updateTaskTagsOperation, change)
		switch {
		case result.outcome == exchangeSucceeded, result.outcome == exchangeInvalid:
			// Any 2xx applied the change, even when its body is unusable.
			output.AppliedChanges++
			continue
		case result.isRetryableRead():
			return sdkgo.NewMutationRetry[UpdateTaskTagsOutput](result.failure, result.retryAfter)
		case result.outcome == exchangeNotFound:
			return sdkgo.NewMutationBranch(UpdateTaskTagsBranchNotFound, output, &result.failure, receipt)
		case result.outcome == exchangeDefect:
			return sdkgo.NewMutationBranch(UpdateTaskTagsBranchDefect, output, &result.failure, receipt)
		default:
			return sdkgo.NewMutationBranch(UpdateTaskTagsBranchProviderRejected, output, &result.failure, receipt)
		}
	}
	result := operation.client.exchange(ctx, credentials, updateTaskTagsOperation, clickupRequest{method: http.MethodGet, path: taskPath(input.TaskID)})
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[UpdateTaskTagsOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchNotFound, output, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchInvalidResponse, output, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchProviderRejected, output, &result.failure, receipt)
	}
	task, err := decodeTaskBody(result.response.body, false)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTaskTagsBranchInvalidResponse, output, clickupFailurePointer(sdkgo.FailureProtocol, updateTaskTagsOperation,
			"ClickUp applied the tag changes but returned an invalid task: "+err.Error()), receipt)
	}
	output.Tags = task.Tags
	return sdkgo.NewMutationBranch(UpdateTaskTagsBranchUpdated, output, nil, receipt)
}

func validateUpdateTaskTagsInput(input UpdateTaskTagsInput) error {
	if err := validateTaskID("taskId", input.TaskID); err != nil {
		return err
	}
	total := len(input.AddTags) + len(input.RemoveTags)
	if total == 0 || total > MaxTagChanges {
		return fmt.Errorf("addTags and removeTags need 1 to %d tags together", MaxTagChanges)
	}
	if err := validateTagNames("addTags", input.AddTags); err != nil {
		return err
	}
	if err := validateTagNames("removeTags", input.RemoveTags); err != nil {
		return err
	}
	if containsDuplicate(append(append([]string(nil), input.AddTags...), input.RemoveTags...)) {
		return errors.New("a tag cannot be both added and removed")
	}
	return nil
}
