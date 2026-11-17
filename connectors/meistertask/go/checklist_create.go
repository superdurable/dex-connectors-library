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

// CreateTaskChecklistInput names a new checklist.
type CreateTaskChecklistInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// Name is the nonempty checklist name.
	Name string `json:"name"`
}

// CreateTaskChecklistOperation creates a checklist through a bounded dispatch fence.
type CreateTaskChecklistOperation struct{ client *Client }

// CreateTaskChecklist binds the Mutation to this client.
func (client *Client) CreateTaskChecklist() CreateTaskChecklistOperation {
	return CreateTaskChecklistOperation{client}
}

// Definition returns the generated checklist Mutation contract.
func (CreateTaskChecklistOperation) Definition() sdkgo.MutationDefinition {
	return CreateTaskChecklistDefinition
}

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (CreateTaskChecklistOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskChecklistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a checklist, repeating unknown sends only within the configured cap.
func (operation CreateTaskChecklistOperation) Invoke(call sdkgo.Call, input CreateTaskChecklistInput) sdkgo.MutationAttempt[ChecklistSummary] {
	if !validTaskID(input.TaskID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureValidation, "parent task ID or checklist name is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateTaskChecklistDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureLocalDefect, "checklist dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createTaskChecklistUncertain(sdkgo.FailureTransport, "an earlier checklist create may have succeeded")
	}
	// Checklist names are not unique, so a list lookup cannot confirm this call's result.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return createTaskChecklistUncertain(sdkgo.FailureAuthentication, "an earlier checklist create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createTaskChecklistRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
	}{input.Name})
	if err != nil {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureLocalDefect, "checklist create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/tasks/"+input.TaskID+"/checklists", bytes.NewReader(payload))
	if err != nil {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureLocalDefect, "checklist create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchDefect, ChecklistSummary{}, sdkgo.FailureLocalDefect, "checklist create request is invalid")
	}
	dispatch := &createTaskChecklistDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createTaskChecklistRetry(sdkgo.FailureTransport, "checklist dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createTaskChecklistRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainCreateTaskChecklist(canResendUnknown, sdkgo.FailureTransport, "checklist create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createTaskChecklistRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createTaskChecklistRetry(kind, "MeisterTask did not process the checklist create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainCreateTaskChecklist(canResendUnknown, sdkgo.FailureAvailability, "checklist create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return createTaskChecklistUncertain(sdkgo.FailureProviderRejection, "an earlier checklist create may have succeeded")
		}
		return checklistMutationBranch("createTaskChecklist", CreateTaskChecklistBranchProviderRejected, ChecklistSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainCreateTaskChecklist(canResendUnknown, sdkgo.FailureTransport, "checklist create response could not be confirmed", 0)
	}
	checklist, err := decodeChecklist(body, "")
	if err != nil || checklist.Name != input.Name || checklist.TaskID != input.TaskID {
		return retryOrUncertainCreateTaskChecklist(canResendUnknown, sdkgo.FailureProtocol, "checklist create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(checklist.ID); err != nil {
		return createTaskChecklistUncertain(sdkgo.FailureLocalDefect, "checklist ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateTaskChecklistBranchCreated, checklist, nil, sdkgo.Receipt{})
}

type createTaskChecklistDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createTaskChecklistDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func createTaskChecklistRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ChecklistSummary] {
	return sdkgo.NewMutationRetry[ChecklistSummary](checklistFailure("createTaskChecklist", kind, message), retryAfter)
}
func createTaskChecklistUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ChecklistSummary] {
	return sdkgo.NewMutationUncertain(ChecklistSummary{}, checklistFailure("createTaskChecklist", kind, message), sdkgo.Receipt{})
}
func retryOrUncertainCreateTaskChecklist(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ChecklistSummary] {
	if canResend {
		return createTaskChecklistRetry(kind, message, retryAfter)
	}
	return createTaskChecklistUncertain(kind, message)
}
