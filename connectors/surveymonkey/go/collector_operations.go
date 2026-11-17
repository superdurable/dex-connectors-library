// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CollectorMetadata contains safe collector identity and display fields.
type CollectorMetadata struct {
	// ID is the provider collector ID.
	ID string `json:"id"`
	// SurveyID identifies the owning survey when supplied by the provider.
	SurveyID string `json:"surveyId,omitempty"`
	// Type is the provider collector type.
	Type string `json:"type,omitempty"`
	// Name is the collector display name.
	Name string `json:"name"`
	// URL is the public survey link for a weblink collector.
	URL string `json:"url,omitempty"`
}

// CollectorsPage is one page of collector identities and names.
type CollectorsPage struct {
	// Collectors contains the selected page without passwords or recipient data.
	Collectors []CollectorMetadata `json:"collectors"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the provider page size.
	PerPage int `json:"perPage"`
	// Total is the provider total count.
	Total int `json:"total"`
}

// ListCollectorsInput selects one collector page for a survey.
type ListCollectorsInput struct {
	// SurveyID is the numeric survey ID.
	SurveyID string `json:"surveyId"`
	// Page is one-based; zero defaults to page one.
	Page int `json:"page,omitempty"`
	// PerPage is from 1 through 100; zero defaults to 50.
	PerPage int `json:"perPage,omitempty"`
}

// GetCollectorInput identifies a collector to read.
type GetCollectorInput struct {
	// CollectorID is the numeric collector ID.
	CollectorID string `json:"collectorId"`
}

// CreateCollectorInput creates one weblink collector.
type CreateCollectorInput struct {
	// SurveyID is the numeric owning survey ID.
	SurveyID string `json:"surveyId"`
	// Name is the nonempty collector display name; it is not a unique identity.
	Name string `json:"name"`
}

// UpdateCollectorInput replaces a collector name.
type UpdateCollectorInput struct {
	// CollectorID is the numeric collector ID.
	CollectorID string `json:"collectorId"`
	// Name is the nonempty replacement name.
	Name string `json:"name"`
}

// DeleteCollectorInput identifies a collector to delete.
type DeleteCollectorInput struct {
	// CollectorID is the numeric collector ID.
	CollectorID string `json:"collectorId"`
}

// DeleteCollectorOutput identifies the deleted or already absent collector.
type DeleteCollectorOutput struct {
	// ID is the requested collector ID.
	ID string `json:"id"`
}

// ListCollectorsOperation lists survey collectors.
type ListCollectorsOperation struct{ client *Client }

// ListCollectors binds the collector query to this client.
func (client *Client) ListCollectors() ListCollectorsOperation {
	return ListCollectorsOperation{client}
}

// Definition returns the generated query contract.
func (ListCollectorsOperation) Definition() sdkgo.QueryDefinition { return ListCollectorsDefinition }

// Invoke reads one bounded collector page.
func (operation ListCollectorsOperation) Invoke(call sdkgo.Call, input ListCollectorsInput) sdkgo.QueryAttempt[CollectorsPage] {
	branches := queryBranches{found: ListCollectorsBranchListed, notFound: ListCollectorsBranchNotFound, rejected: ListCollectorsBranchProviderRejected, invalid: ListCollectorsBranchInvalidResponse, defect: ListCollectorsBranchDefect}
	if !numericID.MatchString(input.SurveyID) || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return queryBranch("listCollectors", branches.defect, CollectorsPage{}, sdkgo.FailureValidation, "survey ID or page selection is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PerPage == 0 {
		input.PerPage = 50
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	path := "/surveys/" + input.SurveyID + "/collectors?" + query.Encode()
	return invokeQuery(call, operation.client, "listCollectors", path, branches, func(body []byte) (CollectorsPage, error) {
		return decodeCollectorsPage(body, input)
	})
}

// GetCollectorOperation reads one collector.
type GetCollectorOperation struct{ client *Client }

// GetCollector binds the collector query to this client.
func (client *Client) GetCollector() GetCollectorOperation { return GetCollectorOperation{client} }

// Definition returns the generated query contract.
func (GetCollectorOperation) Definition() sdkgo.QueryDefinition { return GetCollectorDefinition }

// Invoke reads safe collector display fields.
func (operation GetCollectorOperation) Invoke(call sdkgo.Call, input GetCollectorInput) sdkgo.QueryAttempt[CollectorMetadata] {
	branches := queryBranches{found: GetCollectorBranchFound, notFound: GetCollectorBranchNotFound, rejected: GetCollectorBranchProviderRejected, invalid: GetCollectorBranchInvalidResponse, defect: GetCollectorBranchDefect}
	if !numericID.MatchString(input.CollectorID) {
		return queryBranch("getCollector", branches.defect, CollectorMetadata{}, sdkgo.FailureValidation, "collector ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getCollector", "/collectors/"+input.CollectorID, branches, decodeCollectorMetadata)
}

// CreateCollectorOperation creates a named weblink collector with a bounded send cap.
type CreateCollectorOperation struct{ client *Client }

// CreateCollector binds the collector mutation to this client.
func (client *Client) CreateCollector() CreateCollectorOperation {
	return CreateCollectorOperation{client}
}

// Definition returns the generated mutation contract.
func (CreateCollectorOperation) Definition() sdkgo.MutationDefinition {
	return CreateCollectorDefinition
}

// IdempotencyKey records stable Dex identity; SurveyMonkey does not deduplicate by it.
func (CreateCollectorOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCollectorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one weblink collector per allowed attempt.
func (operation CreateCollectorOperation) Invoke(call sdkgo.Call, input CreateCollectorInput) sdkgo.MutationAttempt[CollectorMetadata] {
	if !numericID.MatchString(input.SurveyID) || strings.TrimSpace(input.Name) == "" {
		return surveyMutationBranch(CreateCollectorBranchDefect, CollectorMetadata{}, "createCollector", sdkgo.FailureValidation, "survey ID and collector name are required")
	}
	body, _ := json.Marshal(map[string]string{"type": "weblink", "name": input.Name})
	fence, err := singledispatch.NewFence(call, CreateCollectorDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return surveyMutationBranch(CreateCollectorBranchDefect, CollectorMetadata{}, "createCollector", sdkgo.FailureLocalDefect, "create checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return surveyMutationBranch(CreateCollectorBranchDefect, CollectorMetadata{}, "createCollector", sdkgo.FailureLocalDefect, "create checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return collectorCreateUncertain("collector create could not be confirmed after the send cap")
	}
	var response surveyHTTPResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestSurvey(call, requestContext, http.MethodPost, "/surveys/"+input.SurveyID+"/collectors", body)
		return nil
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[CollectorMetadata](queryFailure("createCollector", sdkgo.FailureAvailability, "collector create was not dispatched"), 0)
		}
		return sdkgo.NewMutationRetry[CollectorMetadata](queryFailure("createCollector", sdkgo.FailureAvailability, "collector create outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[CollectorMetadata](queryFailure("createCollector", sdkgo.FailureAvailability, "retry checkpoint is unavailable"), 0)
		}
	}
	if retry, failure := surveyRetry("createCollector", response); retry {
		if response.status != http.StatusTooManyRequests && response.status != http.StatusServiceUnavailable && decision.MaximumPriorSends+1 >= call.MaxSends() {
			return collectorCreateUncertain("collector create could not be confirmed after the send cap")
		}
		return sdkgo.NewMutationRetry[CollectorMetadata](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(CreateCollectorBranchDefect, CollectorMetadata{}, "createCollector", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status >= 400 {
		return surveyMutationBranch(CreateCollectorBranchProviderRejected, CollectorMetadata{}, "createCollector", surveyFailureKind(response), "SurveyMonkey rejected the collector create")
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return collectorCreateRetryOrUncertain(call, decision, "collector create response was not confirmed")
	}
	collector, err := decodeCollectorMetadata(response.body)
	if err != nil {
		return collectorCreateRetryOrUncertain(call, decision, "collector create response was invalid")
	}
	return sdkgo.NewMutationBranch(CreateCollectorBranchCreated, collector, nil, sdkgo.Receipt{})
}

// UpdateCollectorOperation replaces a collector name.
type UpdateCollectorOperation struct{ client *Client }

// UpdateCollector binds the collector mutation to this client.
func (client *Client) UpdateCollector() UpdateCollectorOperation {
	return UpdateCollectorOperation{client}
}

// Definition returns the generated mutation contract.
func (UpdateCollectorOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCollectorDefinition
}

// IdempotencyKey derives stable identity for an absolute name replacement.
func (UpdateCollectorOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCollectorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a collector name.
func (operation UpdateCollectorOperation) Invoke(call sdkgo.Call, input UpdateCollectorInput) sdkgo.MutationAttempt[CollectorMetadata] {
	if !numericID.MatchString(input.CollectorID) || strings.TrimSpace(input.Name) == "" {
		return surveyMutationBranch(UpdateCollectorBranchDefect, CollectorMetadata{}, "updateCollector", sdkgo.FailureValidation, "collector ID and replacement name are required")
	}
	body, _ := json.Marshal(map[string]string{"name": input.Name})
	response := operation.client.requestSurvey(call, call.Context, http.MethodPatch, "/collectors/"+input.CollectorID, body)
	if retry, failure := surveyRetry("updateCollector", response); retry {
		return sdkgo.NewMutationRetry[CollectorMetadata](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(UpdateCollectorBranchDefect, CollectorMetadata{}, "updateCollector", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return surveyMutationBranch(UpdateCollectorBranchNotFound, CollectorMetadata{}, "updateCollector", sdkgo.FailureNotFound, "collector was not found")
	}
	if response.status != http.StatusOK {
		return surveyMutationBranch(UpdateCollectorBranchProviderRejected, CollectorMetadata{}, "updateCollector", surveyFailureKind(response), "SurveyMonkey rejected the collector update")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return surveyMutationBranch(UpdateCollectorBranchInvalidResponse, CollectorMetadata{}, "updateCollector", sdkgo.FailureResponseTooLarge, "collector response exceeded the limit")
	}
	collector, err := decodeCollectorMetadata(response.body)
	if err != nil {
		return surveyMutationBranch(UpdateCollectorBranchInvalidResponse, CollectorMetadata{}, "updateCollector", sdkgo.FailureProtocol, "collector response was invalid")
	}
	return sdkgo.NewMutationBranch(UpdateCollectorBranchUpdated, collector, nil, sdkgo.Receipt{})
}

// DeleteCollectorOperation deletes a collector by ID.
type DeleteCollectorOperation struct{ client *Client }

// DeleteCollector binds the collector mutation to this client.
func (client *Client) DeleteCollector() DeleteCollectorOperation {
	return DeleteCollectorOperation{client}
}

// Definition returns the generated mutation contract.
func (DeleteCollectorOperation) Definition() sdkgo.MutationDefinition {
	return DeleteCollectorDefinition
}

// IdempotencyKey derives stable identity for a repeatable delete.
func (DeleteCollectorOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCollectorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a collector, treating a definite 404 as already absent.
func (operation DeleteCollectorOperation) Invoke(call sdkgo.Call, input DeleteCollectorInput) sdkgo.MutationAttempt[DeleteCollectorOutput] {
	if !numericID.MatchString(input.CollectorID) {
		return surveyMutationBranch(DeleteCollectorBranchDefect, DeleteCollectorOutput{}, "deleteCollector", sdkgo.FailureValidation, "collector ID must be numeric")
	}
	response := operation.client.requestSurvey(call, call.Context, http.MethodDelete, "/collectors/"+input.CollectorID, nil)
	if retry, failure := surveyRetry("deleteCollector", response); retry {
		return sdkgo.NewMutationRetry[DeleteCollectorOutput](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(DeleteCollectorBranchDefect, DeleteCollectorOutput{}, "deleteCollector", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status != http.StatusNoContent && response.status != http.StatusOK && response.status != http.StatusNotFound {
		return surveyMutationBranch(DeleteCollectorBranchProviderRejected, DeleteCollectorOutput{}, "deleteCollector", surveyFailureKind(response), "SurveyMonkey rejected the collector delete")
	}
	return sdkgo.NewMutationBranch(DeleteCollectorBranchDeleted, DeleteCollectorOutput{ID: input.CollectorID}, nil, sdkgo.Receipt{})
}

func decodeCollectorsPage(body []byte, input ListCollectorsInput) (CollectorsPage, error) {
	var wire struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CollectorsPage{}, err
	}
	if wire.Data == nil || wire.Page != input.Page || wire.PerPage < 1 || wire.Total < 0 {
		return CollectorsPage{}, errors.New("collector page missing or inconsistent")
	}
	page := CollectorsPage{Collectors: make([]CollectorMetadata, 0, len(wire.Data)), Page: wire.Page, PerPage: wire.PerPage, Total: wire.Total}
	for _, item := range wire.Data {
		if !numericID.MatchString(item.ID) {
			return CollectorsPage{}, errors.New("collector ID missing")
		}
		page.Collectors = append(page.Collectors, CollectorMetadata{ID: item.ID, Name: item.Name})
	}
	return page, nil
}

func decodeCollectorMetadata(body []byte) (CollectorMetadata, error) {
	var wire struct {
		ID       string `json:"id"`
		SurveyID string `json:"survey_id"`
		Type     string `json:"type"`
		Name     string `json:"name"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CollectorMetadata{}, err
	}
	if !numericID.MatchString(wire.ID) || wire.Name == "" {
		return CollectorMetadata{}, errors.New("collector identity missing")
	}
	return CollectorMetadata{ID: wire.ID, SurveyID: wire.SurveyID, Type: wire.Type, Name: wire.Name, URL: wire.URL}, nil
}

func collectorCreateRetryOrUncertain(call sdkgo.Call, decision singledispatch.Decision, message string) sdkgo.MutationAttempt[CollectorMetadata] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return collectorCreateUncertain(message)
	}
	return sdkgo.NewMutationRetry[CollectorMetadata](queryFailure("createCollector", sdkgo.FailureProtocol, message), 0)
}

func collectorCreateUncertain(message string) sdkgo.MutationAttempt[CollectorMetadata] {
	return sdkgo.NewMutationUncertain(CollectorMetadata{}, queryFailure("createCollector", sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}
