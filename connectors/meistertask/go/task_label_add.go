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
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// AddTaskLabelInput attaches one project label to a task.
type AddTaskLabelInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// LabelID is the positive project label ID.
	LabelID string `json:"labelId"`
}

// AddTaskLabelOperation creates an attachment through a bounded dispatch fence.
type AddTaskLabelOperation struct{ client *Client }

// AddTaskLabel binds the Mutation to this client.
func (client *Client) AddTaskLabel() AddTaskLabelOperation { return AddTaskLabelOperation{client} }

// Definition returns the generated task-label Mutation contract.
func (AddTaskLabelOperation) Definition() sdkgo.MutationDefinition { return AddTaskLabelDefinition }

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (AddTaskLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ AddTaskLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an attachment, repeating unknown sends only within the configured cap.
func (operation AddTaskLabelOperation) Invoke(call sdkgo.Call, input AddTaskLabelInput) sdkgo.MutationAttempt[TaskLabelSummary] {
	if !validTaskID(input.TaskID) || !validTaskID(input.LabelID) {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureValidation, "task ID or label ID is invalid")
	}
	fence, err := singledispatch.NewFence(call, AddTaskLabelDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureLocalDefect, "attachment dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return addTaskLabelUncertain(sdkgo.FailureTransport, "an earlier attachment create may have succeeded")
	}
	// A repeated label attachment may be accepted, so list readback cannot prove this call's result.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return addTaskLabelUncertain(sdkgo.FailureAuthentication, "an earlier attachment create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return addTaskLabelRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	labelID, err := strconv.ParseInt(input.LabelID, 10, 32)
	if err != nil {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureValidation, "label ID is invalid")
	}
	payload, err := json.Marshal(struct {
		LabelID int64 `json:"label_id"`
	}{labelID})
	if err != nil {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureLocalDefect, "attachment create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/tasks/"+input.TaskID+"/task_labels", bytes.NewReader(payload))
	if err != nil {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureLocalDefect, "attachment create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchDefect, TaskLabelSummary{}, sdkgo.FailureLocalDefect, "attachment create request is invalid")
	}
	dispatch := &addTaskLabelDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return addTaskLabelRetry(sdkgo.FailureTransport, "attachment dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return addTaskLabelRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainAddTaskLabel(canResendUnknown, sdkgo.FailureTransport, "attachment create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return addTaskLabelRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return addTaskLabelRetry(kind, "MeisterTask did not process the attachment create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainAddTaskLabel(canResendUnknown, sdkgo.FailureAvailability, "attachment create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return addTaskLabelUncertain(sdkgo.FailureProviderRejection, "an earlier attachment create may have succeeded")
		}
		return labelMutationBranch("addTaskLabel", AddTaskLabelBranchProviderRejected, TaskLabelSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainAddTaskLabel(canResendUnknown, sdkgo.FailureTransport, "attachment create response could not be confirmed", 0)
	}
	attachment, err := decodeTaskLabel(body, "")
	if err != nil || attachment.TaskID != input.TaskID || attachment.LabelID != input.LabelID {
		return retryOrUncertainAddTaskLabel(canResendUnknown, sdkgo.FailureProtocol, "attachment create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(attachment.ID); err != nil {
		return addTaskLabelUncertain(sdkgo.FailureLocalDefect, "attachment ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(AddTaskLabelBranchAdded, attachment, nil, sdkgo.Receipt{})
}

type addTaskLabelDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *addTaskLabelDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func addTaskLabelRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[TaskLabelSummary] {
	return sdkgo.NewMutationRetry[TaskLabelSummary](labelFailure("addTaskLabel", kind, message), retryAfter)
}
func addTaskLabelUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskLabelSummary] {
	return sdkgo.NewMutationUncertain(TaskLabelSummary{}, labelFailure("addTaskLabel", kind, message), sdkgo.Receipt{})
}
func retryOrUncertainAddTaskLabel(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[TaskLabelSummary] {
	if canResend {
		return addTaskLabelRetry(kind, message, retryAfter)
	}
	return addTaskLabelUncertain(kind, message)
}
