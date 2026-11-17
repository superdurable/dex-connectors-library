// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTaskInput names a task and its parent section.
type CreateTaskInput struct {
	// SectionID is the positive section ID from MeisterTask.
	SectionID string `json:"sectionId"`
	// Name is the nonempty new task name.
	Name string `json:"name"`
	// Notes is optional plain-text task detail; blank creates no notes.
	Notes string `json:"notes"`
}

// CreateTaskOperation creates one task through a bounded dispatch fence.
type CreateTaskOperation struct{ client *Client }

// CreateTask binds the Mutation to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a task, repeating unknown sends only within the configured cap.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[TaskSummary] {
	if !validTaskID(input.SectionID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || len(input.Notes) > 65536 {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureValidation, "section ID, task name, or notes are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "MeisterTask dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createTaskUncertain(sdkgo.FailureTransport, "an earlier task create may have succeeded")
	}
	// MeisterTask permits duplicate names, so a section lookup cannot identify this call's task.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return createTaskUncertain(sdkgo.FailureAuthentication, "an earlier task create may have succeeded; credentials are unavailable")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createTaskRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Name  string `json:"name"`
		Notes string `json:"notes,omitempty"`
	}{Name: input.Name, Notes: input.Notes})
	if err != nil {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "task create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/sections/"+input.SectionID+"/tasks", bytes.NewReader(payload))
	if err != nil {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "task create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return createTaskBranch(CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "task create request is invalid")
	}
	dispatch := &createTaskDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createTaskRetry(sdkgo.FailureTransport, "task dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createTaskRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainCreateTask(canResendUnknown, sdkgo.FailureTransport, "task create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createTaskRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createTaskRetry(kind, "MeisterTask did not process the task create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainCreateTask(canResendUnknown, sdkgo.FailureAvailability, "task create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return createTaskUncertain(sdkgo.FailureProviderRejection, "an earlier task create may have succeeded")
		}
		return createTaskBranch(CreateTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainCreateTask(canResendUnknown, sdkgo.FailureTransport, "task create response could not be confirmed", 0)
	}
	var identity struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(body, &identity) != nil || identity.ID <= 0 {
		return retryOrUncertainCreateTask(canResendUnknown, sdkgo.FailureProtocol, "task create response was incomplete", 0)
	}
	task, err := decodeTask(body, strconv.FormatInt(identity.ID, 10))
	if err != nil || task.Name != input.Name {
		return retryOrUncertainCreateTask(canResendUnknown, sdkgo.FailureProtocol, "task create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(task.ID); err != nil {
		return createTaskUncertain(sdkgo.FailureLocalDefect, "task ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, task, nil, sdkgo.Receipt{})
}

type createTaskDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createTaskDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func createTaskFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: "createTask", Message: message}
}

func createTaskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskSummary] {
	failure := createTaskFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, TaskSummary{}, &failure, sdkgo.Receipt{})
}

func createTaskRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[TaskSummary] {
	return sdkgo.NewMutationRetry[TaskSummary](createTaskFailure(kind, message), retryAfter)
}

func createTaskUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskSummary] {
	return sdkgo.NewMutationUncertain(TaskSummary{}, createTaskFailure(kind, message), sdkgo.Receipt{})
}

func retryOrUncertainCreateTask(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[TaskSummary] {
	if canResend {
		return createTaskRetry(kind, message, retryAfter)
	}
	return createTaskUncertain(kind, message)
}
