// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package todoist lists, reads, creates, updates, and deletes tasks through typed Dex Steps.
// See examples/task-crud for a runnable Flow using all five generated factories.
package todoist

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport. New copies the client and disables redirects to protect credentials.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable configuration and resolves credentials for each call. It is safe for concurrent calls.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// GetTaskInput identifies an active, non-completed task.
type GetTaskInput struct {
	// TaskID is Todoist's opaque string task ID, not a task URL.
	TaskID string `json:"taskId"`
}

// UpdateTaskInput patches specified fields; nil preserves a field. At least one field must be specified.
type UpdateTaskInput struct {
	// TaskID identifies the existing task.
	TaskID string `json:"taskId"`
	// Content is an optional non-blank replacement title. Nil leaves the title unchanged.
	Content *string `json:"content,omitempty"`
	// Description replaces the description when non-nil. An empty string clears it.
	Description *string `json:"description,omitempty"`
}

// Task is a bounded projection of Todoist's API v1 task response. Provider text is untrusted.
type Task struct {
	// ID is the opaque task ID.
	ID string `json:"id"`
	// ProjectID identifies the task's project.
	ProjectID string `json:"projectId"`
	// Content is the task title.
	Content string `json:"content"`
	// Description is the task description, possibly empty.
	Description string `json:"description"`
	// IsCompleted reports Todoist's checked field.
	IsCompleted bool `json:"isCompleted"`
}

// GetTaskOperation reads one task in one HTTP request.
type GetTaskOperation struct{ client *Client }

// UpdateTaskOperation patches specified title and description fields in one HTTP request. An unusable or
// mismatched answer is confirmed by one more read of the task by ID within the same attempt.
type UpdateTaskOperation struct{ client *Client }

// ListTasksInput selects one page; retain identical filters when following NextCursor.
type ListTasksInput struct {
	// ProjectID limits results to one project. Empty lists active tasks across projects.
	ProjectID string `json:"projectId,omitempty"`
	// Cursor is the previous page's opaque NextCursor. Empty starts the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit is the page size, 1 through 200. Zero uses Todoist's default of 50.
	Limit int `json:"limit,omitempty"`
}

// TaskPage contains active tasks and a continuation cursor; it is not a consistent snapshot.
type TaskPage struct {
	// Tasks contains this page, possibly empty.
	Tasks []Task `json:"tasks"`
	// NextCursor is empty when there is no next page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// CreateTaskInput creates a task; omitted fields use Todoist defaults.
type CreateTaskInput struct {
	// Content is the complete non-blank UTF-8 task title.
	Content string `json:"content"`
	// Description is optional UTF-8 description text.
	Description string `json:"description,omitempty"`
	// ProjectID chooses a project. Empty creates the task in Inbox.
	ProjectID string `json:"projectId,omitempty"`
}

// DeleteTaskInput identifies the task to delete, including its subtasks.
type DeleteTaskInput struct {
	// TaskID is the immutable task ID; never a title or search expression.
	TaskID string `json:"taskId"`
}

// DeleteTaskOutcome identifies the requested task; inspect the branch to distinguish deletion from absence.
type DeleteTaskOutcome struct {
	// TaskID is the task requested for deletion.
	TaskID string `json:"taskId"`
}

// ListTasksOperation reads one bounded page in one request.
type ListTasksOperation struct{ client *Client }

// CreateTaskOperation creates one task with a stable provider request ID.
type CreateTaskOperation struct{ client *Client }

// DeleteTaskOperation deletes a task and its subtasks by immutable ID.
type DeleteTaskOperation struct{ client *Client }

type wireTask struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Content     *string `json:"content"`
	Description *string `json:"description"`
	Checked     *bool   `json:"checked"`
}

type exchangeResult struct {
	task       Task
	body       []byte
	status     int
	failure    *sdkgo.Failure
	retryAfter time.Duration
	// isLocalDefect marks credentials or a request rejected locally, before dispatch.
	isLocalDefect bool
	// isTransient marks a transport failure, an interrupted success body, or HTTP 408/5xx.
	isTransient bool
	// isUndispatched marks a transport failure with trace evidence that no request left the process.
	isUndispatched bool
}

// attemptBudget ends each request early enough for Invoke to return before the Execute deadline.
type attemptBudget struct {
	stepContext context.Context
	// writeDeadline ends a write at seven twelfths of the attempt, leaving a quarter for its read-back.
	writeDeadline time.Time
	// attemptDeadline ends every request at five sixths, leaving the rest to record the result.
	attemptDeadline    time.Time
	minimumRequestTime time.Duration
}

// requestWindow bounds one request; with less than minimumTime left, the request is not sent.
type requestWindow struct {
	parent      context.Context
	deadline    time.Time
	minimumTime time.Duration
}

// dispatchObservation records whether a request could have reached Todoist.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// New validates configuration without network access. Zero fields use manifest defaults.
// Credentials are resolved for every call; replacing a token does not require restarting the Worker.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Todoist credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Todoist base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetTask returns the query bound to this client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client} }

// UpdateTask returns the mutation bound to this client.
func (client *Client) UpdateTask() UpdateTaskOperation {
	return UpdateTaskOperation{client}
}

// Definition returns the generated query contract.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Definition returns the generated mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTaskDefinition
}

// IdempotencyKey derives stable identity. Absolute content replacement is replayable; no provider deduplication is assumed.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one active task. Transport errors and temporary HTTP failures retry under the Step policy.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[Task] {
	if !taskIDPattern.MatchString(input.TaskID) {
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, failure("getTask", sdkgo.FailureValidation, "taskId must be a non-empty opaque task ID"), sdkgo.Receipt{})
	}
	budget := newAttemptBudget(call.Context, GetTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "getTask")
	if hasCredentials {
		result = operation.client.sendTaskRequest(budget.remainingWindow(), credentials, "getTask", http.MethodGet, input.TaskID, nil)
	}
	switch {
	case result.failure == nil:
		return sdkgo.NewQueryBranch(GetTaskBranchFound, result.task, nil, sdkgo.Receipt{})
	case result.isLocalDefect:
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, result.failure, sdkgo.Receipt{})
	case result.isTransient || result.status == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[Task](*result.failure, result.retryAfter)
	case result.status == http.StatusNotFound:
		return sdkgo.NewQueryBranch(GetTaskBranchNotFound, Task{}, result.failure, sdkgo.Receipt{})
	case result.failure.Kind == sdkgo.FailureProtocol || result.failure.Kind == sdkgo.FailureResponseTooLarge:
		return sdkgo.NewQueryBranch(GetTaskBranchInvalidResponse, Task{}, result.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(GetTaskBranchProviderRejected, Task{}, result.failure, sdkgo.Receipt{})
	}
}

// Invoke replaces only specified fields. A mismatched response is checked by task ID before retrying the replacement.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[Task] {
	if !taskIDPattern.MatchString(input.TaskID) || input.Content == nil && input.Description == nil ||
		input.Content != nil && (!utf8.ValidString(*input.Content) || strings.TrimSpace(*input.Content) == "") ||
		input.Description != nil && !utf8.ValidString(*input.Description) {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, Task{}, failure("updateTaskContent", sdkgo.FailureValidation, "taskId and at least one valid field are required; specified content must be non-blank UTF-8"), sdkgo.Receipt{})
	}
	payload, err := json.Marshal(struct {
		Content     *string `json:"content,omitempty"`
		Description *string `json:"description,omitempty"`
	}{input.Content, input.Description})
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, Task{}, failure("updateTaskContent", sdkgo.FailureValidation, "encoded fields exceed Todoist's 1 MiB request limit"), sdkgo.Receipt{})
	}
	budget := newAttemptBudget(call.Context, UpdateTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "updateTaskContent")
	if hasCredentials {
		result = operation.client.sendTaskRequest(budget.writeWindow(), credentials, "updateTaskContent", http.MethodPost, input.TaskID, payload)
	}
	switch {
	case isSuccessStatus(result.status) && (result.failure != nil || !taskMatchesUpdate(result.task, input)):
		return operation.confirmUpdate(budget, credentials, input, result)
	case result.failure == nil:
		return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, result.task, nil, sdkgo.Receipt{})
	case result.isLocalDefect:
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, Task{}, result.failure, sdkgo.Receipt{})
	case result.isTransient || result.status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Task](*result.failure, result.retryAfter)
	case result.status == http.StatusNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskBranchNotFound, Task{}, result.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(UpdateTaskBranchProviderRejected, Task{}, result.failure, sdkgo.Receipt{})
	}
}

// confirmUpdate reads back an unconfirmed 2xx. GET returns only active tasks, so 404 means completed or deleted.
func (operation UpdateTaskOperation) confirmUpdate(budget attemptBudget, credentials Credentials, input UpdateTaskInput, answer exchangeResult) sdkgo.MutationAttempt[Task] {
	readBack := operation.client.sendTaskRequest(budget.remainingWindow(), credentials, "updateTaskContent", http.MethodGet, input.TaskID, nil)
	switch {
	case readBack.failure == nil && taskMatchesUpdate(readBack.task, input):
		return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, readBack.task, nil, sdkgo.Receipt{})
	case readBack.isLocalDefect:
		return sdkgo.NewMutationBranch(UpdateTaskBranchDefect, Task{}, readBack.failure, sdkgo.Receipt{})
	case readBack.status == http.StatusNotFound:
		return sdkgo.NewMutationBranch(UpdateTaskBranchNotFound, Task{}, failure("updateTaskContent", sdkgo.FailureNotFound, "Todoist answered the update, but the task is no longer active; it may be completed or deleted, so the specified fields are unconfirmed"), sdkgo.Receipt{})
	case readBack.status >= 300 && !readBack.isTransient && readBack.status != http.StatusTooManyRequests:
		return sdkgo.NewMutationBranch(UpdateTaskBranchProviderRejected, Task{}, failure("updateTaskContent", readBack.failure.Kind, "Todoist rejected the read-back with HTTP "+strconv.Itoa(readBack.status)+", so the update could not be confirmed; repeating it is safe because it sets absolute field values"), sdkgo.Receipt{})
	case readBack.failure != nil:
		return sdkgo.NewMutationRetry[Task](*readBack.failure, readBack.retryAfter)
	case answer.failure != nil:
		return sdkgo.NewMutationRetry[Task](*answer.failure, answer.retryAfter)
	default:
		return sdkgo.NewMutationRetry[Task](*failure("updateTaskContent", sdkgo.FailureProtocol, "Todoist did not confirm the specified fields"), 0)
	}
}

func taskMatchesUpdate(task Task, input UpdateTaskInput) bool {
	return (input.Content == nil || task.Content == *input.Content) &&
		(input.Description == nil || task.Description == *input.Description)
}

// ListTasks returns this client's paginated Query.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// CreateTask returns this client's create Mutation.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client} }

// DeleteTask returns this client's delete Mutation.
func (client *Client) DeleteTask() DeleteTaskOperation { return DeleteTaskOperation{client} }

// Definition returns the generated list contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Definition returns the generated create contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// Definition returns the generated delete contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition { return DeleteTaskDefinition }

// IdempotencyKey derives a stable UUID-shaped X-Request-ID from the Dex Call ID.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte("todoist/createTask/" + string(callID)))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return sdkgo.IdempotencyKey(fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16]))
}

// IdempotencyKey preserves the call identity; repeated deletion targets the same immutable task ID.
func (DeleteTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one page. Applications schedule another Step with NextCursor to continue.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TaskPage] {
	if input.ProjectID != "" && !taskIDPattern.MatchString(input.ProjectID) || input.Limit < 0 || input.Limit > 200 || !validCursor(input.Cursor) {
		return sdkgo.NewQueryBranch(ListTasksBranchDefect, TaskPage{}, failure("listTasks", sdkgo.FailureValidation, "invalid projectId, cursor, or limit"), sdkgo.Receipt{})
	}
	query := url.Values{}
	if input.ProjectID != "" {
		query.Set("project_id", input.ProjectID)
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.Limit != 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	endpoint := "/tasks"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	budget := newAttemptBudget(call.Context, ListTasksDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "listTasks")
	if hasCredentials {
		result = operation.client.sendRequest(budget.remainingWindow(), credentials, "listTasks", http.MethodGet, endpoint, nil, "")
	}
	if result.failure != nil {
		switch {
		case result.isLocalDefect:
			return sdkgo.NewQueryBranch(ListTasksBranchDefect, TaskPage{}, result.failure, sdkgo.Receipt{})
		case result.isTransient || result.status == 429:
			return sdkgo.NewQueryRetry[TaskPage](*result.failure, result.retryAfter)
		case result.failure.Kind == sdkgo.FailureProtocol || result.failure.Kind == sdkgo.FailureResponseTooLarge:
			return sdkgo.NewQueryBranch(ListTasksBranchInvalidResponse, TaskPage{}, result.failure, sdkgo.Receipt{})
		default:
			return sdkgo.NewQueryBranch(ListTasksBranchProviderRejected, TaskPage{}, result.failure, sdkgo.Receipt{})
		}
	}
	page, err := decodeTaskPage(result.body, input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListTasksBranchInvalidResponse, TaskPage{}, failure("listTasks", sdkgo.FailureProtocol, "Todoist returned an invalid task page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates one task. A 2xx with a task ID but unusable fields is confirmed by reading that task, never by
// resending the creation. A possibly dispatched write that no read confirms remains uncertain.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[Task] {
	if !utf8.ValidString(input.Content) || strings.TrimSpace(input.Content) == "" || !utf8.ValidString(input.Description) || input.ProjectID != "" && !taskIDPattern.MatchString(input.ProjectID) || !requestIDPattern.MatchString(string(call.IdempotencyKey)) {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, Task{}, failure("createTask", sdkgo.FailureValidation, "valid content, description, projectId, and stable request identity are required"), sdkgo.Receipt{})
	}
	payload, err := json.Marshal(struct {
		Content     string `json:"content"`
		Description string `json:"description,omitempty"`
		ProjectID   string `json:"project_id,omitempty"`
	}{input.Content, input.Description, input.ProjectID})
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, Task{}, failure("createTask", sdkgo.FailureValidation, "encoded task exceeds Todoist's 1 MiB request limit"), sdkgo.Receipt{})
	}
	budget := newAttemptBudget(call.Context, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "createTask")
	if hasCredentials {
		result = operation.client.sendRequest(budget.writeWindow(), credentials, "createTask", http.MethodPost, "/tasks", payload, string(call.IdempotencyKey))
	}
	if result.failure != nil {
		switch {
		case result.isLocalDefect:
			return sdkgo.NewMutationBranch(CreateTaskBranchDefect, Task{}, result.failure, sdkgo.Receipt{})
		case result.isUndispatched || result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable:
			return sdkgo.NewMutationRetry[Task](*result.failure, result.retryAfter)
		case result.status == http.StatusConflict || result.isTransient || result.failure.Kind == sdkgo.FailureProtocol || result.failure.Kind == sdkgo.FailureResponseTooLarge:
			// Todoist does not document whether 409 means this request ID already created a task.
			return sdkgo.NewMutationUncertain(Task{}, *result.failure, sdkgo.Receipt{})
		default:
			return sdkgo.NewMutationBranch(CreateTaskBranchProviderRejected, Task{}, result.failure, sdkgo.Receipt{})
		}
	}
	// A valid task is the created task as Todoist stored it; fields may differ from the request.
	if task, err := decodeTask(result.body); err == nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchCreated, task, nil, sdkgo.Receipt{})
	}
	taskID, hasTaskID := decodeCreatedTaskID(result.body)
	if !hasTaskID {
		return sdkgo.NewMutationUncertain(Task{}, *failure("createTask", sdkgo.FailureProtocol, "Todoist answered the creation without a usable task"), sdkgo.Receipt{})
	}
	return operation.readCreatedTask(budget, credentials, taskID)
}

// readCreatedTask confirms an accepted creation. Any unusable or skipped read stays uncertain; a retry would create again.
func (operation CreateTaskOperation) readCreatedTask(budget attemptBudget, credentials Credentials, taskID string) sdkgo.MutationAttempt[Task] {
	readBack := operation.client.sendTaskRequest(budget.remainingWindow(), credentials, "createTask", http.MethodGet, taskID, nil)
	if readBack.failure == nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchCreated, readBack.task, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationUncertain(Task{}, *failure("createTask", sdkgo.FailureProtocol, "Todoist accepted the creation of task "+taskID+" but it could not be read back in this attempt, so the task is unconfirmed"), sdkgo.Receipt{})
}

func decodeCreatedTaskID(body []byte) (string, bool) {
	var answer struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || !taskIDPattern.MatchString(answer.ID) {
		return "", false
	}
	return answer.ID, true
}

// Invoke deletes by task ID. Transport and transient server failures can safely retry to confirmed absence.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[DeleteTaskOutcome] {
	if !taskIDPattern.MatchString(input.TaskID) {
		return sdkgo.NewMutationBranch(DeleteTaskBranchDefect, DeleteTaskOutcome{}, failure("deleteTask", sdkgo.FailureValidation, "taskId must be a non-empty opaque task ID"), sdkgo.Receipt{})
	}
	budget := newAttemptBudget(call.Context, DeleteTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "deleteTask")
	if hasCredentials {
		result = operation.client.sendRequest(budget.remainingWindow(), credentials, "deleteTask", http.MethodDelete, "/tasks/"+input.TaskID, nil, "")
	}
	value := DeleteTaskOutcome{TaskID: input.TaskID}
	switch {
	case result.failure == nil:
		return sdkgo.NewMutationBranch(DeleteTaskBranchDeleted, value, nil, sdkgo.Receipt{})
	case result.isLocalDefect:
		return sdkgo.NewMutationBranch(DeleteTaskBranchDefect, DeleteTaskOutcome{}, result.failure, sdkgo.Receipt{})
	case result.isTransient || result.status == 429:
		return sdkgo.NewMutationRetry[DeleteTaskOutcome](*result.failure, result.retryAfter)
	case result.status == 404:
		return sdkgo.NewMutationBranch(DeleteTaskBranchAlreadyAbsent, value, result.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(DeleteTaskBranchProviderRejected, DeleteTaskOutcome{}, result.failure, sdkgo.Receipt{})
	}
}

// resolveCredentials runs once per Invoke, so a read-back cannot wait on a second, unbudgeted resolution.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, exchangeResult, bool) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil && isTerminalCredentialError(err) {
		return Credentials{}, exchangeResult{isLocalDefect: true, failure: failure(operation, sdkgo.FailureAuthentication, "connection credentials are missing or need reauthorization")}, false
	}
	if err != nil {
		return Credentials{}, exchangeResult{isTransient: true, isUndispatched: true, failure: failure(operation, sdkgo.FailureAvailability, "connection credentials could not be read, so the request was not sent")}, false
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return Credentials{}, exchangeResult{isLocalDefect: true, failure: failure(operation, sdkgo.FailureAuthentication, "connection credentials are invalid")}, false
	}
	return credentials, exchangeResult{}, true
}

func (client *Client) sendTaskRequest(window requestWindow, credentials Credentials, operation, method, taskID string, payload []byte) exchangeResult {
	result := client.sendRequest(window, credentials, operation, method, "/tasks/"+taskID, payload, "")
	if result.failure != nil {
		return result
	}
	task, err := decodeTask(result.body)
	if err != nil || task.ID != taskID {
		result.failure = failure(operation, sdkgo.FailureProtocol, "Todoist returned an invalid task response")
		return result
	}
	result.task = task
	return result
}

func (client *Client) sendRequest(window requestWindow, credentials Credentials, operation, method, endpoint string, payload []byte, requestID string) exchangeResult {
	if !window.hasTimeToStart() {
		return exchangeResult{isTransient: true, isUndispatched: true, failure: failure(operation, sdkgo.FailureAvailability, "too little attempt time remained, so the request was not sent")}
	}
	// The window deadline also cuts a longer caller-supplied http.Client.Timeout.
	windowContext, cancelWindow := context.WithDeadline(window.parent, window.deadline)
	defer cancelWindow()
	observation := &dispatchObservation{}
	requestContext := httptrace.WithClientTrace(windowContext, observation.clientTrace())
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+endpoint, bytes.NewReader(payload))
	if err != nil {
		return exchangeResult{isLocalDefect: true, failure: failure(operation, sdkgo.FailureLocalDefect, "Todoist request could not be built")}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil && observation.isProvablyUndispatched() {
		return exchangeResult{isTransient: true, isUndispatched: true, failure: failure(operation, sdkgo.FailureTransport, "Todoist could not be reached, so the request was not sent")}
	}
	if err != nil {
		return exchangeResult{isTransient: true, failure: failure(operation, sdkgo.FailureTransport, "Todoist request did not return a complete response")}
	}
	// Closing a response is best effort and cannot alter the classified provider outcome.
	defer func() { _ = response.Body.Close() }()
	// Every 2xx is an answer, never a rejection; DELETE needs no body.
	if method == http.MethodDelete && isSuccessStatus(response.StatusCode) {
		return exchangeResult{status: response.StatusCode}
	}
	if !isSuccessStatus(response.StatusCode) {
		return classifyHTTPFailure(operation, response)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		kind := sdkgo.FailureProtocol
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			kind = sdkgo.FailureResponseTooLarge
		}
		return exchangeResult{status: response.StatusCode, failure: failure(operation, kind, "Todoist response was interrupted or exceeded the response limit"), isTransient: !errors.Is(err, providerhttp.ErrBodyTooLarge)}
	}
	if bytes.Contains(body, []byte(credentials.APIToken.Reveal())) {
		return exchangeResult{status: response.StatusCode, failure: failure(operation, sdkgo.FailureProtocol, "Todoist returned credential material in the response")}
	}
	return exchangeResult{status: response.StatusCode, body: body}
}

// isTerminalCredentialError reports a credential resolution failure that no retry fixes; others, such as store reads, may pass.
func isTerminalCredentialError(err error) bool {
	return errors.Is(err, projectconfig.ErrObjectNotFound) || errors.Is(err, sdkgo.ErrReauthorizationRequired) ||
		errors.Is(err, projectconfig.ErrConnectionNotSelected) || errors.Is(err, projectconfig.ErrCredentialBrokerRejected)
}

// newAttemptBudget uses the Execute timeout, shortened by the Step deadline; an async first attempt may get seven seconds.
func newAttemptBudget(stepContext context.Context, executeTimeout time.Duration) attemptBudget {
	start := time.Now()
	available := executeTimeout
	if stepDeadline, hasDeadline := stepContext.Deadline(); hasDeadline {
		available = min(available, stepDeadline.Sub(start))
	}
	available = max(available, 0)
	return attemptBudget{
		stepContext:        stepContext,
		writeDeadline:      start.Add(available * 7 / 12),
		attemptDeadline:    start.Add(available * 5 / 6),
		minimumRequestTime: available / 10,
	}
}

func (budget attemptBudget) writeWindow() requestWindow {
	return requestWindow{parent: budget.stepContext, deadline: budget.writeDeadline, minimumTime: budget.minimumRequestTime}
}

func (budget attemptBudget) remainingWindow() requestWindow {
	return requestWindow{parent: budget.stepContext, deadline: budget.attemptDeadline, minimumTime: budget.minimumRequestTime}
}

func (window requestWindow) hasTimeToStart() bool {
	return window.minimumTime > 0 && time.Until(window.deadline) >= window.minimumTime
}

func decodeTask(body []byte) (Task, error) {
	var task wireTask
	if err := json.Unmarshal(body, &task); err != nil || !taskIDPattern.MatchString(task.ID) || !taskIDPattern.MatchString(task.ProjectID) || task.Content == nil || task.Description == nil || task.Checked == nil {
		return Task{}, fmt.Errorf("invalid Todoist task")
	}
	return Task{ID: task.ID, ProjectID: task.ProjectID, Content: *task.Content, Description: *task.Description, IsCompleted: *task.Checked}, nil
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func classifyHTTPFailure(operation string, response *http.Response) exchangeResult {
	result := exchangeResult{status: response.StatusCode}
	kind := sdkgo.FailureProviderRejection
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusConflict:
		kind = sdkgo.FailureConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = sdkgo.FailureValidation
	case http.StatusTooManyRequests:
		kind = sdkgo.FailureRateLimit
		result.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	default:
		if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
			kind = sdkgo.FailureAvailability
			result.isTransient = true
			result.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}
	}
	result.failure = failure(operation, kind, "Todoist returned HTTP "+strconv.Itoa(response.StatusCode))
	return result
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure before any connection was obtained.
func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *dispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

func failure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "todoist", Operation: operation, Message: message}
}

var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validCursor(cursor string) bool {
	return cursor == "" || len(cursor) <= 4096 && cursorPattern.MatchString(cursor)
}
func decodeTaskPage(body []byte, input ListTasksInput) (TaskPage, error) {
	var wirePage struct {
		Results    *[]json.RawMessage `json:"results"`
		NextCursor json.RawMessage    `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &wirePage); err != nil || wirePage.Results == nil || len(wirePage.NextCursor) == 0 {
		return TaskPage{}, fmt.Errorf("invalid task page")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if len(*wirePage.Results) > limit {
		return TaskPage{}, fmt.Errorf("oversized task page")
	}
	page := TaskPage{Tasks: make([]Task, 0, len(*wirePage.Results))}
	if err := json.Unmarshal(wirePage.NextCursor, &page.NextCursor); err != nil || !validCursor(page.NextCursor) || page.NextCursor != "" && page.NextCursor == input.Cursor {
		return TaskPage{}, fmt.Errorf("invalid continuation cursor")
	}
	seen := make(map[string]bool)
	for _, encoded := range *wirePage.Results {
		task, err := decodeTask(encoded)
		if err != nil || seen[task.ID] || input.ProjectID != "" && task.ProjectID != input.ProjectID {
			return TaskPage{}, fmt.Errorf("invalid task page item")
		}
		seen[task.ID] = true
		page.Tasks = append(page.Tasks, task)
	}
	return page, nil
}
