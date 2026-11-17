// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateProjectInput creates a project under a Sentry team.
type CreateProjectInput struct {
	// Organization is the Sentry organization slug.
	Organization string `json:"organization"`
	// Team is the owning Sentry team slug.
	Team string `json:"team"`
	// Name is the project display name.
	Name string `json:"name"`
	// Slug is a stable project slug unique within the organization.
	Slug string `json:"slug"`
}

// CreateProjectOperation creates a project with bounded resend.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds project creation to this client.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the generated createProject contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey correlates attempts; Sentry does not document POST deduplication.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or confirms the project identified by its organization and slug.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validSentrySegments(input.Organization, input.Team, input.Name, input.Slug) {
		return mutationFailure[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "organization, team, name, and slug are required")
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}{input.Name, input.Slug})
	if err != nil {
		return mutationFailure[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "project cannot be encoded")
	}
	path := "/teams/" + url.PathEscape(input.Organization) + "/" + url.PathEscape(input.Team) + "/projects/"
	request := sentryLimitedCreate[Project]{client: operation.client, call: call, operation: "createProject", path: path, body: body, timeout: CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateProjectBranchCreated, rejected: CreateProjectBranchProviderRejected, defect: CreateProjectBranchDefect, decode: decodeCreatedProject, readBack: func(call sdkgo.Call) (Project, readBackState) { return operation.readBackProject(call, input) }}
	return request.invoke()
}

func decodeCreatedProject(body []byte) (Project, error) {
	var project Project
	err := json.Unmarshal(body, &project)
	if err == nil && (project.ID == "" || project.Slug == "" || project.Name == "") {
		err = fmt.Errorf("project identity missing")
	}
	return project, err
}

func (operation CreateProjectOperation) readBackProject(call sdkgo.Call, input CreateProjectInput) (Project, readBackState) {
	path := "/projects/" + url.PathEscape(input.Organization) + "/" + url.PathEscape(input.Slug) + "/"
	body, state := operation.client.readBackResource(call, path)
	if state != readBackMatched {
		return Project{}, state
	}
	project, err := decodeCreatedProject(body)
	if err != nil {
		return Project{}, readBackRetry
	}
	if project.Slug != input.Slug || project.Name != input.Name {
		return Project{}, readBackConflicted
	}
	return project, readBackMatched
}

type readBackState uint8

const (
	readBackRetry readBackState = iota
	readBackAbsent
	readBackMatched
	readBackConflicted
)

type sentryLimitedCreate[T any] struct {
	client    *Client
	call      sdkgo.Call
	operation string
	path      string
	body      []byte
	timeout   time.Duration
	created   sdkgo.BranchID
	rejected  sdkgo.BranchID
	defect    sdkgo.BranchID
	decode    func([]byte) (T, error)
	readBack  func(sdkgo.Call) (T, readBackState)
}

func (request sentryLimitedCreate[T]) invoke() sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(request.call, request.timeout, singledispatch.WithMaxSends(request.call.MaxSends()))
	if err != nil {
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return request.uncertain("create checkpoint cannot be trusted")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		value, state := request.readBack(request.call)
		switch state {
		case readBackMatched:
			return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
		case readBackRetry:
			return request.retry(sdkgo.FailureAvailability, "provider read-back is temporarily unavailable", 0)
		case readBackConflicted:
			return request.uncertain("create outcome conflicts with the provider resource")
		case readBackAbsent:
			if decision.Kind == singledispatch.ReadBackRequired {
				return request.uncertain("send cap reached without confirming creation")
			}
		}
	}
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	canResendUnknown := decision.MaximumPriorSends+1 < request.call.MaxSends()
	httpRequest, err := request.client.newRequest(request.call, http.MethodPost, request.path, request.body)
	if err != nil {
		if hasPossibleEarlierSend {
			return request.uncertain("connection unavailable after a possible create")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "no-send checkpoint unavailable", 0)
		}
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	if err := singledispatch.PrepareHTTPWrite(httpRequest); err != nil {
		return mutationFailure[T](request.operation, request.defect, sdkgo.FailureLocalDefect, "create request cannot be prepared")
	}
	dispatch := sentryCreateDispatch{client: request.client, request: httpRequest}
	err = fence.Dispatch(dispatch.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return request.retry(sdkgo.FailureAvailability, "create dispatch did not start", 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return request.retry(sdkgo.FailureAvailability, "retry checkpoint unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if dispatch.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return request.retry(kind, "provider did not process create", dispatch.retryAfter)
	}
	if err != nil || dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		if canResendUnknown {
			return request.retry(sdkgo.FailureAvailability, "create outcome is unknown", dispatch.retryAfter)
		}
		return request.uncertain("send cap reached with unknown create outcome")
	}
	if dispatch.status == http.StatusConflict {
		if !hasPossibleEarlierSend {
			return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider reports the resource already exists")
		}
		value, state := request.readBack(request.call)
		if state == readBackMatched {
			return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
		}
		if state == readBackRetry {
			return request.retry(sdkgo.FailureAvailability, "provider read-back is temporarily unavailable", 0)
		}
		if hasPossibleEarlierSend {
			return request.uncertain("earlier create may have succeeded")
		}
		return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider reports the resource already exists")
	}
	if dispatch.status != http.StatusOK && dispatch.status != http.StatusCreated {
		if hasPossibleEarlierSend {
			return request.uncertain("earlier create may have succeeded")
		}
		return mutationFailure[T](request.operation, request.rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(dispatch.status))
	}
	value, err := request.decode(dispatch.body)
	if err != nil {
		confirmed, state := request.readBack(request.call)
		if state == readBackMatched {
			return sdkgo.NewMutationBranch(request.created, confirmed, nil, sdkgo.Receipt{})
		}
		return request.uncertain("provider accepted create without a confirmable ID")
	}
	return sdkgo.NewMutationBranch(request.created, value, nil, sdkgo.Receipt{})
}

func (request sentryLimitedCreate[T]) retry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](newFailure(request.operation, kind, message), retryAfter)
}
func (request sentryLimitedCreate[T]) uncertain(message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), newFailure(request.operation, sdkgo.FailureTransport, message), sdkgo.Receipt{})
}

type sentryCreateDispatch struct {
	client     *Client
	request    *http.Request
	status     int
	body       []byte
	retryAfter time.Duration
}

func (dispatch *sentryCreateDispatch) send(ctx context.Context) error {
	response, err := dispatch.client.httpClient.Do(dispatch.request.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	dispatch.status = response.StatusCode
	dispatch.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	dispatch.body, err = providerhttp.ReadBoundedBody(response.Body, dispatch.client.maxResponseBytes)
	return err
}

func (client *Client) readBackResource(call sdkgo.Call, path string) ([]byte, readBackState) {
	request, err := client.newRequest(call, http.MethodGet, path, nil)
	if err != nil {
		return nil, readBackRetry
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, readBackRetry
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil, readBackAbsent
	}
	if response.StatusCode != http.StatusOK {
		return nil, readBackRetry
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return nil, readBackRetry
	}
	return body, readBackMatched
}
