// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxCreateTaskCustomFields bounds the custom field values one createTask sets.
	MaxCreateTaskCustomFields = 50
	// MaxAssignees bounds the assignees one createTask or updateTask sends.
	MaxAssignees = 50
	// MaxTaskTags bounds the tags one createTask sets.
	MaxTaskTags = 20

	createTaskOperation = "createTask"

	// writeReconciliationSkew tolerates a difference between the Worker's and ClickUp's clocks.
	writeReconciliationSkew = 5 * time.Minute
)

// CreateTaskInput is one task to create in a List.
type CreateTaskInput struct {
	// ListID is the List to create the task in, the number after /li/ in the List's web address.
	ListID string `json:"listId"`
	// Name is the task name. A retried attempt finds its earlier task by this name, so a name that
	// carries the application's own request key, such as [REQ-1042], makes reconciliation unambiguous.
	Name string `json:"name"`
	// MarkdownDescription is the description in Markdown, at most MaxDescriptionBytes; empty sets none.
	MarkdownDescription string `json:"markdownDescription,omitempty"`
	// Status is a status name of the List's workflow, such as in progress; empty uses the List's first status.
	Status string `json:"status,omitempty"`
	// Priority is 1 urgent through 4 low; zero sets no priority.
	Priority TaskPriority `json:"priority,omitempty"`
	// DueAt is the due date and time; nil sets none.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// AssigneeIDs are the users to assign, such as a findMemberByEmail result's ID.
	AssigneeIDs []int64 `json:"assigneeIds,omitempty"`
	// Tags are tag names of the List's Space to add.
	Tags []string `json:"tags,omitempty"`
	// ParentTaskID creates a subtask of this task, which must be in the same List; empty creates a task.
	ParentTaskID string `json:"parentTaskId,omitempty"`
	// CustomFields set custom field values; ClickUp ignores fields that do not apply to the task.
	CustomFields []CustomFieldInput `json:"customFields,omitempty"`
	// IsCreatorNotified also notifies the token's user; assignees and watchers are always notified.
	IsCreatorNotified bool `json:"isCreatorNotified,omitempty"`
}

// CreateTaskOutput is the created task.
type CreateTaskOutput struct {
	// ListID echoes the requested List.
	ListID string `json:"listId"`
	// Name echoes the requested name.
	Name string `json:"name"`
	// Task is the task ClickUp created, or empty on every branch except created.
	Task Task `json:"task"`
	// WasAlreadyApplied reports that an earlier attempt of this Step created the task without a
	// confirmed outcome and this attempt found it in the List, so nothing was sent again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// CreateTaskOperation is the createTask Mutation.
type CreateTaskOperation struct {
	client *Client
}

type createTaskWire struct {
	Name            string                 `json:"name"`
	MarkdownContent string                 `json:"markdown_content,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Priority        int                    `json:"priority,omitempty"`
	DueDate         int64                  `json:"due_date,omitempty"`
	DueDateTime     bool                   `json:"due_date_time,omitempty"`
	Assignees       []int64                `json:"assignees,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	Parent          string                 `json:"parent,omitempty"`
	NotifyAll       bool                   `json:"notify_all,omitempty"`
	CustomFields    []customFieldInputWire `json:"custom_fields,omitempty"`
}

type customFieldInputWire struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}

// writeDispatchMarker is the Dex heartbeat checkpoint recorded before an unkeyed write is sent.
type writeDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"clickupDispatchedCallId"`
	// DispatchedAtMilliseconds is the Worker's Unix time in milliseconds just before the send.
	DispatchedAtMilliseconds int64 `json:"clickupDispatchedAt"`
}

// Definition returns the immutable connector operation definition.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey uses the stable connector Call ID. ClickUp documents no idempotency key, so the key
// only correlates the Receipt; duplicate protection comes from a Dex heartbeat checkpoint instead.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records a dispatch checkpoint and then sends POST /list/{list_id}/task. An attempt that finds
// the checkpoint never sends: it reads the List's tasks created since the dispatch and reports the one
// task with the requested name and parent, or selects uncertain. Only a 429 or a connection that never
// opened clears the checkpoint for a resend. Dex accepts the checkpoint when the Worker writes it to its
// stream, so a Worker lost before Dex stored it could still send the create twice.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[CreateTaskOutput] {
	output := CreateTaskOutput{ListID: input.ListID, Name: input.Name}
	if err := validateCreateTaskInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, createTaskOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, createTaskOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, output, failure, sdkgo.Receipt{})
	}
	var marker writeDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil || isMarkerFound {
		return operation.reconcileEarlierDispatch(call, credentials, input, marker)
	}
	if err := call.Context.RecordHeartbeat(writeDispatchMarker{DispatchedCallID: call.ID, DispatchedAtMilliseconds: operation.client.now().UnixMilli()}); err != nil {
		return sdkgo.NewMutationRetry[CreateTaskOutput](clickupFailure(sdkgo.FailureAvailability, createTaskOperation,
			"Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	}
	result := operation.client.exchange(call.Context, credentials, createTaskOperation, clickupRequest{
		method: http.MethodPost, path: "/list/" + input.ListID + "/task", payload: createTaskPayload(input),
	})
	receipt := operation.client.receipt(call, input.ListID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		releaseWriteDispatch(call)
		return sdkgo.NewMutationRetry[CreateTaskOutput](result.failure, result.retryAfter)
	case exchangeUnconfirmed, exchangeInvalid:
		// The checkpoint stays, so the next attempt reads the List instead of resending.
		return sdkgo.NewMutationRetry[CreateTaskOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(CreateTaskBranchNotFound, output, &result.failure, receipt)
	case exchangeDefect:
		releaseWriteDispatch(call)
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateTaskBranchProviderRejected, output, &result.failure, receipt)
	}
	task, err := decodeTaskBody(result.response.body, false)
	if err != nil {
		return sdkgo.NewMutationRetry[CreateTaskOutput](clickupFailure(sdkgo.FailureProtocol, createTaskOperation,
			"ClickUp accepted the task but returned an invalid task, so the next attempt reads the List back: "+err.Error()), 0)
	}
	output.Task, receipt.ProviderObjectID = task, task.ID
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, output, nil, receipt)
}

// reconcileEarlierDispatch never sends: it reports the earlier attempt's task found in the List, or uncertain.
func (operation CreateTaskOperation) reconcileEarlierDispatch(
	call sdkgo.Call, credentials Credentials, input CreateTaskInput, marker writeDispatchMarker,
) sdkgo.MutationAttempt[CreateTaskOutput] {
	output := CreateTaskOutput{ListID: input.ListID, Name: input.Name}
	since := time.Time{}
	if marker.DispatchedAtMilliseconds > 0 {
		since = time.UnixMilli(marker.DispatchedAtMilliseconds).Add(-writeReconciliationSkew)
	}
	query := url.Values{"include_closed": {"true"}, "subtasks": {"true"}, "page": {"0"}}
	if !since.IsZero() {
		query.Set("date_created_gt", strconv.FormatInt(since.UnixMilli(), 10))
	}
	result := operation.client.exchange(call.Context, credentials, createTaskOperation, clickupRequest{
		method: http.MethodGet, path: "/list/" + input.ListID + "/task", query: query,
	})
	receipt := operation.client.receipt(call, input.ListID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[CreateTaskOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(CreateTaskBranchNotFound, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationUncertain(output, clickupFailure(result.failure.Kind, createTaskOperation,
			"an earlier attempt of this Step sent the create, and the List could not be read back: "+result.failure.Message), receipt)
	}
	page, err := decodeSearchTasksPage(result.response.body, 0)
	if err != nil {
		return sdkgo.NewMutationUncertain(output, clickupFailure(sdkgo.FailureProtocol, createTaskOperation,
			"an earlier attempt of this Step sent the create, and the List read back is invalid: "+err.Error()), receipt)
	}
	var matches []Task
	for _, task := range page.Tasks {
		if strings.TrimSpace(task.Name) == strings.TrimSpace(input.Name) && task.ParentTaskID == input.ParentTaskID &&
			(since.IsZero() || !task.CreatedAt.Before(since)) {
			matches = append(matches, task)
		}
	}
	if len(matches) != 1 {
		return sdkgo.NewMutationUncertain(output, clickupFailure(sdkgo.FailureTransport, createTaskOperation, fmt.Sprintf(
			"an earlier attempt of this Step sent the create without a confirmed outcome, and the List shows %d tasks with the name created since, so it is not sent again",
			len(matches))), receipt)
	}
	output.Task, output.WasAlreadyApplied, receipt.ProviderObjectID = matches[0], true, matches[0].ID
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, output, nil, receipt)
}

func createTaskPayload(input CreateTaskInput) createTaskWire {
	payload := createTaskWire{
		Name: input.Name, MarkdownContent: input.MarkdownDescription, Status: input.Status, Priority: int(input.Priority),
		Assignees: input.AssigneeIDs, Tags: input.Tags, Parent: input.ParentTaskID, NotifyAll: input.IsCreatorNotified,
	}
	if input.DueAt != nil {
		payload.DueDate, payload.DueDateTime = input.DueAt.UnixMilli(), true
	}
	for _, field := range input.CustomFields {
		payload.CustomFields = append(payload.CustomFields, customFieldInputWire(field))
	}
	return payload
}

// releaseWriteDispatch clears the checkpoint after ClickUp provably did not receive or apply the write.
func releaseWriteDispatch(call sdkgo.Call) {
	// A failed clear leaves the checkpoint, so the next attempt reconciles instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}

func validateCreateTaskInput(input CreateTaskInput) error {
	if err := validateNumericID("listId", input.ListID); err != nil {
		return err
	}
	if err := validateTaskName("name", input.Name); err != nil {
		return err
	}
	if len(input.MarkdownDescription) > MaxDescriptionBytes || !utf8.ValidString(input.MarkdownDescription) {
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
	if len(input.AssigneeIDs) > MaxAssignees {
		return fmt.Errorf("assigneeIds accepts at most %d users", MaxAssignees)
	}
	if err := validateUserIDs("assigneeIds", input.AssigneeIDs); err != nil {
		return err
	}
	if len(input.Tags) > MaxTaskTags {
		return fmt.Errorf("tags accepts at most %d tags", MaxTaskTags)
	}
	if err := validateTagNames("tags", input.Tags); err != nil {
		return err
	}
	if input.ParentTaskID != "" {
		if err := validateTaskID("parentTaskId", input.ParentTaskID); err != nil {
			return err
		}
	}
	return validateCustomFieldInputs(input.CustomFields)
}

func validateCustomFieldInputs(fields []CustomFieldInput) error {
	if len(fields) > MaxCreateTaskCustomFields {
		return fmt.Errorf("customFields accepts at most %d values", MaxCreateTaskCustomFields)
	}
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		if !fieldIDPattern.MatchString(field.ID) {
			return errors.New("customFields must name each field by its ClickUp field ID")
		}
		if len(field.Value) == 0 || !json.Valid(field.Value) {
			return fmt.Errorf("custom field %s needs a JSON value", field.ID)
		}
		ids = append(ids, field.ID)
	}
	if containsDuplicate(ids) {
		return errors.New("customFields sets one field twice")
	}
	return nil
}
