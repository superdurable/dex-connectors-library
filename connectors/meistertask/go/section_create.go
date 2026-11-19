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

// CreateSectionInput names a new section.
type CreateSectionInput struct {
	// ProjectID is the positive parent project ID.
	ProjectID string `json:"projectId"`
	// Name is the nonempty section name.
	Name string `json:"name"`
}

// CreateSectionOperation creates a section through a bounded dispatch fence.
type CreateSectionOperation struct{ client *Client }

// CreateSection binds the Mutation to this client.
func (client *Client) CreateSection() CreateSectionOperation { return CreateSectionOperation{client} }

// Definition returns the generated section Mutation contract.
func (CreateSectionOperation) Definition() sdkgo.MutationDefinition { return CreateSectionDefinition }

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (CreateSectionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSectionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a section, repeating unknown sends only within the configured cap.
func (operation CreateSectionOperation) Invoke(call sdkgo.Call, input CreateSectionInput) sdkgo.MutationAttempt[SectionSummary] {
	if !validTaskID(input.ProjectID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureValidation, "parent project ID or section name is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateSectionDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureLocalDefect, "section dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createSectionUncertain(sdkgo.FailureTransport, "an earlier section create may have succeeded")
	}
	// Section names are not unique, so a list lookup cannot confirm this call's result.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return createSectionUncertain(sdkgo.FailureAuthentication, "an earlier section create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createSectionRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
	}{input.Name})
	if err != nil {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureLocalDefect, "section create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/projects/"+input.ProjectID+"/sections", bytes.NewReader(payload))
	if err != nil {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureLocalDefect, "section create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return sectionMutationBranch("createSection", CreateSectionBranchDefect, SectionSummary{}, sdkgo.FailureLocalDefect, "section create request is invalid")
	}
	dispatch := &createSectionDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createSectionRetry(sdkgo.FailureTransport, "section dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createSectionRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainCreateSection(canResendUnknown, sdkgo.FailureTransport, "section create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createSectionRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createSectionRetry(kind, "MeisterTask did not process the section create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainCreateSection(canResendUnknown, sdkgo.FailureAvailability, "section create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return createSectionUncertain(sdkgo.FailureProviderRejection, "an earlier section create may have succeeded")
		}
		return sectionMutationBranch("createSection", CreateSectionBranchProviderRejected, SectionSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainCreateSection(canResendUnknown, sdkgo.FailureTransport, "section create response could not be confirmed", 0)
	}
	section, err := decodeSection(body, "")
	if err != nil || section.Name != input.Name || section.ProjectID != input.ProjectID {
		return retryOrUncertainCreateSection(canResendUnknown, sdkgo.FailureProtocol, "section create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(section.ID); err != nil {
		return createSectionUncertain(sdkgo.FailureLocalDefect, "section ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateSectionBranchCreated, section, nil, sdkgo.Receipt{})
}

type createSectionDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createSectionDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func createSectionRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SectionSummary] {
	return sdkgo.NewMutationRetry[SectionSummary](sectionFailure("createSection", kind, message), retryAfter)
}
func createSectionUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SectionSummary] {
	return sdkgo.NewMutationUncertain(SectionSummary{}, sectionFailure("createSection", kind, message), sdkgo.Receipt{})
}
func retryOrUncertainCreateSection(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SectionSummary] {
	if canResend {
		return createSectionRetry(kind, message, retryAfter)
	}
	return createSectionUncertain(kind, message)
}
