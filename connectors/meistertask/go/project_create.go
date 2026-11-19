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

// CreateProjectInput names a new project.
type CreateProjectInput struct {
	// Name is the nonempty project name.
	Name string `json:"name"`
	// Notes is optional plain-text project detail; blank omits it.
	Notes string `json:"notes"`
}

// CreateProjectOperation creates a project through a bounded dispatch fence.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds the Mutation to this client.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the generated project Mutation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a project, repeating unknown sends only within the configured cap.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[ProjectSummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || len(input.Notes) > 65536 {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureValidation, "project name or notes are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureLocalDefect, "project dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createProjectUncertain(sdkgo.FailureTransport, "an earlier project create may have succeeded")
	}
	// Project names are not unique, so a list lookup cannot confirm this call's result.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return createProjectUncertain(sdkgo.FailureAuthentication, "an earlier project create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createProjectRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Name  string `json:"name"`
		Notes string `json:"notes,omitempty"`
	}{input.Name, input.Notes})
	if err != nil {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureLocalDefect, "project create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/projects", bytes.NewReader(payload))
	if err != nil {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureLocalDefect, "project create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return projectMutationBranch("createProject", CreateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureLocalDefect, "project create request is invalid")
	}
	dispatch := &createProjectDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createProjectRetry(sdkgo.FailureTransport, "project dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createProjectRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainCreateProject(canResendUnknown, sdkgo.FailureTransport, "project create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createProjectRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createProjectRetry(kind, "MeisterTask did not process the project create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainCreateProject(canResendUnknown, sdkgo.FailureAvailability, "project create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return createProjectUncertain(sdkgo.FailureProviderRejection, "an earlier project create may have succeeded")
		}
		return projectMutationBranch("createProject", CreateProjectBranchProviderRejected, ProjectSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainCreateProject(canResendUnknown, sdkgo.FailureTransport, "project create response could not be confirmed", 0)
	}
	project, err := decodeProject(body, "")
	if err != nil || project.Name != input.Name {
		return retryOrUncertainCreateProject(canResendUnknown, sdkgo.FailureProtocol, "project create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(project.ID); err != nil {
		return createProjectUncertain(sdkgo.FailureLocalDefect, "project ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateProjectBranchCreated, project, nil, sdkgo.Receipt{})
}

type createProjectDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createProjectDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func createProjectRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ProjectSummary] {
	return sdkgo.NewMutationRetry[ProjectSummary](projectFailure("createProject", kind, message), retryAfter)
}
func createProjectUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ProjectSummary] {
	return sdkgo.NewMutationUncertain(ProjectSummary{}, projectFailure("createProject", kind, message), sdkgo.Receipt{})
}
func retryOrUncertainCreateProject(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ProjectSummary] {
	if canResend {
		return createProjectRetry(kind, message, retryAfter)
	}
	return createProjectUncertain(kind, message)
}
