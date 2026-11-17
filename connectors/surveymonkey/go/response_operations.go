// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListResponsesInput selects a bounded page of response IDs for one survey.
type ListResponsesInput struct {
	// SurveyID is the numeric survey ID.
	SurveyID string `json:"surveyId"`
	// Page is one-based; zero defaults to the first page.
	Page int `json:"page,omitempty"`
	// PerPage is from 1 through 100; zero defaults to 50.
	PerPage int `json:"perPage,omitempty"`
}

// ResponseIDsPage holds IDs and pagination without respondent data or answers.
type ResponseIDsPage struct {
	// ResponseIDs are provider response IDs.
	ResponseIDs []string `json:"responseIds"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the provider page size.
	PerPage int `json:"perPage"`
	// Total is the provider total response count.
	Total int `json:"total"`
}

// DeleteResponseInput identifies a response through its collector.
type DeleteResponseInput struct {
	// CollectorID is the numeric collector ID.
	CollectorID string `json:"collectorId"`
	// ResponseID is the numeric response ID.
	ResponseID string `json:"responseId"`
}

// DeleteResponseOutput identifies the deleted or already absent response.
type DeleteResponseOutput struct {
	// ID is the requested response ID.
	ID string `json:"id"`
}

// ListResponsesOperation lists response IDs for one survey.
type ListResponsesOperation struct{ client *Client }

// ListResponses binds the response query to this client.
func (client *Client) ListResponses() ListResponsesOperation { return ListResponsesOperation{client} }

// Definition returns the generated query contract.
func (ListResponsesOperation) Definition() sdkgo.QueryDefinition { return ListResponsesDefinition }

// Invoke reads one response ID page without answer content.
func (operation ListResponsesOperation) Invoke(call sdkgo.Call, input ListResponsesInput) sdkgo.QueryAttempt[ResponseIDsPage] {
	branches := queryBranches{found: ListResponsesBranchListed, notFound: ListResponsesBranchNotFound, rejected: ListResponsesBranchProviderRejected, invalid: ListResponsesBranchInvalidResponse, defect: ListResponsesBranchDefect}
	if !numericID.MatchString(input.SurveyID) || input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return queryBranch("listResponses", branches.defect, ResponseIDsPage{}, sdkgo.FailureValidation, "survey ID or page selection is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PerPage == 0 {
		input.PerPage = 50
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	path := "/surveys/" + input.SurveyID + "/responses?" + query.Encode()
	return invokeQuery(call, operation.client, "listResponses", path, branches, func(body []byte) (ResponseIDsPage, error) {
		return decodeResponseIDsPage(body, input)
	})
}

// DeleteResponseOperation deletes one collector response by ID.
type DeleteResponseOperation struct{ client *Client }

// DeleteResponse binds the response mutation to this client.
func (client *Client) DeleteResponse() DeleteResponseOperation {
	return DeleteResponseOperation{client}
}

// Definition returns the generated mutation contract.
func (DeleteResponseOperation) Definition() sdkgo.MutationDefinition { return DeleteResponseDefinition }

// IdempotencyKey derives stable identity for a repeatable delete.
func (DeleteResponseOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteResponseInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one response, treating a definite 404 as already absent.
func (operation DeleteResponseOperation) Invoke(call sdkgo.Call, input DeleteResponseInput) sdkgo.MutationAttempt[DeleteResponseOutput] {
	if !numericID.MatchString(input.CollectorID) || !numericID.MatchString(input.ResponseID) {
		return surveyMutationBranch(DeleteResponseBranchDefect, DeleteResponseOutput{}, "deleteResponse", sdkgo.FailureValidation, "collector and response IDs must be numeric")
	}
	path := "/collectors/" + input.CollectorID + "/responses/" + input.ResponseID
	response := operation.client.requestSurvey(call, call.Context, http.MethodDelete, path, nil)
	if retry, failure := surveyRetry("deleteResponse", response); retry {
		return sdkgo.NewMutationRetry[DeleteResponseOutput](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return surveyMutationBranch(DeleteResponseBranchDefect, DeleteResponseOutput{}, "deleteResponse", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if response.status != http.StatusNoContent && response.status != http.StatusOK && response.status != http.StatusNotFound {
		return surveyMutationBranch(DeleteResponseBranchProviderRejected, DeleteResponseOutput{}, "deleteResponse", surveyFailureKind(response), "SurveyMonkey rejected the response delete")
	}
	return sdkgo.NewMutationBranch(DeleteResponseBranchDeleted, DeleteResponseOutput{ID: input.ResponseID}, nil, sdkgo.Receipt{})
}

func decodeResponseIDsPage(body []byte, input ListResponsesInput) (ResponseIDsPage, error) {
	var wire struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ResponseIDsPage{}, err
	}
	if wire.Data == nil || wire.Page != input.Page || wire.PerPage < 1 || wire.Total < 0 {
		return ResponseIDsPage{}, errors.New("response page missing or inconsistent")
	}
	page := ResponseIDsPage{ResponseIDs: make([]string, 0, len(wire.Data)), Page: wire.Page, PerPage: wire.PerPage, Total: wire.Total}
	for _, item := range wire.Data {
		if !numericID.MatchString(item.ID) {
			return ResponseIDsPage{}, errors.New("response ID missing")
		}
		page.ResponseIDs = append(page.ResponseIDs, item.ID)
	}
	return page, nil
}
