// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GetSurveyInput identifies a survey to read.
type GetSurveyInput struct {
	// SurveyID is the numeric SurveyMonkey survey ID.
	SurveyID string `json:"surveyId"`
}

// CreateSurveyInput names an empty survey to create.
type CreateSurveyInput struct {
	// Title is the nonempty survey title; it is not a unique identity.
	Title string `json:"title"`
}

// UpdateSurveyInput replaces the title of an existing survey.
type UpdateSurveyInput struct {
	// SurveyID is the numeric SurveyMonkey survey ID.
	SurveyID string `json:"surveyId"`
	// Title is the replacement title.
	Title string `json:"title"`
}

// DeleteSurveyInput identifies a survey to delete.
type DeleteSurveyInput struct {
	// SurveyID is the numeric SurveyMonkey survey ID.
	SurveyID string `json:"surveyId"`
}

// DeleteSurveyOutput identifies the deleted or already absent survey.
type DeleteSurveyOutput struct {
	// ID is the requested survey ID.
	ID string `json:"id"`
}

// GetSurveyOperation reads survey identity and title.
type GetSurveyOperation struct{ client *Client }

// GetSurvey binds the survey query to this client.
func (client *Client) GetSurvey() GetSurveyOperation { return GetSurveyOperation{client} }

// Definition returns the generated query contract.
func (GetSurveyOperation) Definition() sdkgo.QueryDefinition { return GetSurveyDefinition }

// Invoke reads one survey without exposing other provider fields.
func (operation GetSurveyOperation) Invoke(call sdkgo.Call, input GetSurveyInput) sdkgo.QueryAttempt[SurveyMetadata] {
	branches := queryBranches{found: GetSurveyBranchFound, notFound: GetSurveyBranchNotFound, rejected: GetSurveyBranchProviderRejected, invalid: GetSurveyBranchInvalidResponse, defect: GetSurveyBranchDefect}
	if !numericID.MatchString(input.SurveyID) {
		return queryBranch("getSurvey", branches.defect, SurveyMetadata{}, sdkgo.FailureValidation, "survey ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getSurvey", "/surveys/"+input.SurveyID, branches, decodeSurveyMetadata)
}

// CreateSurveyOperation creates an empty survey with a bounded send cap.
type CreateSurveyOperation struct{ client *Client }

// CreateSurvey binds the survey mutation to this client.
func (client *Client) CreateSurvey() CreateSurveyOperation { return CreateSurveyOperation{client} }

// Definition returns the generated mutation contract.
func (CreateSurveyOperation) Definition() sdkgo.MutationDefinition { return CreateSurveyDefinition }

// IdempotencyKey records stable Dex call identity; SurveyMonkey does not deduplicate by it.
func (CreateSurveyOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSurveyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one create request per attempt and fails uncertain after the bounded cap.
func (operation CreateSurveyOperation) Invoke(call sdkgo.Call, input CreateSurveyInput) sdkgo.MutationAttempt[SurveyMetadata] {
	if strings.TrimSpace(input.Title) == "" {
		return surveyMutationBranch(CreateSurveyBranchDefect, SurveyMetadata{}, "createSurvey", sdkgo.FailureValidation, "survey title is required")
	}
	body, err := json.Marshal(map[string]string{"title": input.Title})
	if err != nil {
		return surveyMutationBranch(CreateSurveyBranchDefect, SurveyMetadata{}, "createSurvey", sdkgo.FailureValidation, "survey title cannot be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateSurveyDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return surveyMutationBranch(CreateSurveyBranchDefect, SurveyMetadata{}, "createSurvey", sdkgo.FailureLocalDefect, "create checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return surveyMutationBranch(CreateSurveyBranchDefect, SurveyMetadata{}, "createSurvey", sdkgo.FailureLocalDefect, "create checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return createSurveyUncertain("survey create could not be confirmed after the send cap")
	}
	var response surveyHTTPResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestSurvey(call, requestContext, http.MethodPost, "/surveys", body)
		return nil
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[SurveyMetadata](queryFailure("createSurvey", sdkgo.FailureAvailability, "survey create was not dispatched"), 0)
		}
		return sdkgo.NewMutationRetry[SurveyMetadata](queryFailure("createSurvey", sdkgo.FailureAvailability, "survey create outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SurveyMetadata](queryFailure("createSurvey", sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
		}
	}
	if retry, failure := surveyRetry("createSurvey", response); retry {
		if response.status != http.StatusTooManyRequests && response.status != http.StatusServiceUnavailable && decision.MaximumPriorSends+1 >= call.MaxSends() {
			return createSurveyUncertain("survey create could not be confirmed after the send cap")
		}
		return sdkgo.NewMutationRetry[SurveyMetadata](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(CreateSurveyBranchDefect, SurveyMetadata{}, "createSurvey", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status >= 400 {
		return surveyMutationBranch(CreateSurveyBranchProviderRejected, SurveyMetadata{}, "createSurvey", surveyFailureKind(response), "SurveyMonkey rejected the survey create")
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return createSurveyUncertain("survey create response could not be confirmed")
		}
		return sdkgo.NewMutationRetry[SurveyMetadata](queryFailure("createSurvey", sdkgo.FailureProtocol, "survey create response was not confirmed"), 0)
	}
	survey, err := decodeSurveyMetadata(response.body)
	if err != nil {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return createSurveyUncertain("survey create response could not be confirmed")
		}
		return sdkgo.NewMutationRetry[SurveyMetadata](queryFailure("createSurvey", sdkgo.FailureProtocol, "survey create response was invalid"), 0)
	}
	return sdkgo.NewMutationBranch(CreateSurveyBranchCreated, survey, nil, sdkgo.Receipt{})
}

// UpdateSurveyOperation replaces an existing survey title.
type UpdateSurveyOperation struct{ client *Client }

// UpdateSurvey binds the survey mutation to this client.
func (client *Client) UpdateSurvey() UpdateSurveyOperation { return UpdateSurveyOperation{client} }

// Definition returns the generated mutation contract.
func (UpdateSurveyOperation) Definition() sdkgo.MutationDefinition { return UpdateSurveyDefinition }

// IdempotencyKey derives a stable call key for the absolute title replacement.
func (UpdateSurveyOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSurveyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a survey title and classifies a definite missing survey.
func (operation UpdateSurveyOperation) Invoke(call sdkgo.Call, input UpdateSurveyInput) sdkgo.MutationAttempt[SurveyMetadata] {
	if !numericID.MatchString(input.SurveyID) || strings.TrimSpace(input.Title) == "" {
		return surveyMutationBranch(UpdateSurveyBranchDefect, SurveyMetadata{}, "updateSurvey", sdkgo.FailureValidation, "survey ID and replacement title are required")
	}
	body, _ := json.Marshal(map[string]string{"title": input.Title})
	response := operation.client.requestSurvey(call, call.Context, http.MethodPatch, "/surveys/"+input.SurveyID, body)
	if retry, failure := surveyRetry("updateSurvey", response); retry {
		return sdkgo.NewMutationRetry[SurveyMetadata](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(UpdateSurveyBranchDefect, SurveyMetadata{}, "updateSurvey", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return surveyMutationBranch(UpdateSurveyBranchNotFound, SurveyMetadata{}, "updateSurvey", sdkgo.FailureNotFound, "survey was not found")
	}
	if response.status != http.StatusOK {
		return surveyMutationBranch(UpdateSurveyBranchProviderRejected, SurveyMetadata{}, "updateSurvey", surveyFailureKind(response), "SurveyMonkey rejected the survey update with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return surveyMutationBranch(UpdateSurveyBranchInvalidResponse, SurveyMetadata{}, "updateSurvey", sdkgo.FailureResponseTooLarge, "survey response exceeded the limit")
	}
	survey, err := decodeSurveyMetadata(response.body)
	if err != nil {
		return surveyMutationBranch(UpdateSurveyBranchInvalidResponse, SurveyMetadata{}, "updateSurvey", sdkgo.FailureProtocol, "survey response was invalid")
	}
	return sdkgo.NewMutationBranch(UpdateSurveyBranchUpdated, survey, nil, sdkgo.Receipt{})
}

// DeleteSurveyOperation deletes a survey by ID.
type DeleteSurveyOperation struct{ client *Client }

// DeleteSurvey binds the survey mutation to this client.
func (client *Client) DeleteSurvey() DeleteSurveyOperation { return DeleteSurveyOperation{client} }

// Definition returns the generated mutation contract.
func (DeleteSurveyOperation) Definition() sdkgo.MutationDefinition { return DeleteSurveyDefinition }

// IdempotencyKey derives stable identity for a repeatable delete.
func (DeleteSurveyOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSurveyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a survey, treating a definite 404 as already absent.
func (operation DeleteSurveyOperation) Invoke(call sdkgo.Call, input DeleteSurveyInput) sdkgo.MutationAttempt[DeleteSurveyOutput] {
	if !numericID.MatchString(input.SurveyID) {
		return surveyMutationBranch(DeleteSurveyBranchDefect, DeleteSurveyOutput{}, "deleteSurvey", sdkgo.FailureValidation, "survey ID must be numeric")
	}
	response := operation.client.requestSurvey(call, call.Context, http.MethodDelete, "/surveys/"+input.SurveyID, nil)
	if retry, failure := surveyRetry("deleteSurvey", response); retry {
		return sdkgo.NewMutationRetry[DeleteSurveyOutput](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(DeleteSurveyBranchDefect, DeleteSurveyOutput{}, "deleteSurvey", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status != http.StatusNoContent && response.status != http.StatusOK && response.status != http.StatusNotFound {
		return surveyMutationBranch(DeleteSurveyBranchProviderRejected, DeleteSurveyOutput{}, "deleteSurvey", surveyFailureKind(response), "SurveyMonkey rejected the survey delete with HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(DeleteSurveyBranchDeleted, DeleteSurveyOutput{ID: input.SurveyID}, nil, sdkgo.Receipt{})
}

type surveyHTTPResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
	err        error
}

func (client *Client) requestSurvey(call sdkgo.Call, requestContext context.Context, method, path string, body []byte) surveyHTTPResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return surveyHTTPResponse{err: errCredential}
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return surveyHTTPResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return surveyHTTPResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return surveyHTTPResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return surveyHTTPResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func surveyRetry(operation string, response surveyHTTPResponse) (bool, sdkgo.Failure) {
	if response.err == errRequest {
		return true, queryFailure(operation, sdkgo.FailureTransport, "SurveyMonkey request failed")
	}
	if response.status == http.StatusTooManyRequests {
		return true, queryFailure(operation, sdkgo.FailureRateLimit, "SurveyMonkey rate limited the request")
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) && response.err != errCredential {
		return true, queryFailure(operation, sdkgo.FailureAvailability, "SurveyMonkey outcome is temporarily unknown")
	}
	return false, sdkgo.Failure{}
}

func surveyFailureKind(response surveyHTTPResponse) sdkgo.FailureKind {
	if response.status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if response.status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

func surveyMutationBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}

func createSurveyUncertain(message string) sdkgo.MutationAttempt[SurveyMetadata] {
	return sdkgo.NewMutationUncertain(SurveyMetadata{}, queryFailure("createSurvey", sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}

func decodeSurveyMetadata(body []byte) (SurveyMetadata, error) {
	var survey SurveyMetadata
	if err := json.Unmarshal(body, &survey); err != nil {
		return SurveyMetadata{}, err
	}
	if !numericID.MatchString(survey.ID) {
		return SurveyMetadata{}, errors.New("survey ID missing")
	}
	return survey, nil
}
