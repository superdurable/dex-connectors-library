// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListSurveysInput selects a one-based page of at most 100 surveys.
type ListSurveysInput struct {
	// Page is one-based; zero defaults to the first page.
	Page int `json:"page,omitempty"`
	// PerPage is the maximum survey count; zero defaults to 50.
	PerPage int `json:"perPage,omitempty"`
}

// SurveyMetadata contains survey identity without questions or answers.
type SurveyMetadata struct {
	// ID is the provider survey ID.
	ID string `json:"id"`
	// Title is the untrusted survey display title.
	Title string `json:"title"`
}

// SurveysPage is one page of survey metadata.
type SurveysPage struct {
	// Surveys contains the selected page.
	Surveys []SurveyMetadata `json:"surveys"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the provider page size.
	PerPage int `json:"perPage"`
	// Total is the provider total survey count.
	Total int `json:"total"`
}

// GetResponseInput selects one response within one survey.
type GetResponseInput struct {
	// SurveyID is the numeric survey ID.
	SurveyID string `json:"surveyId"`
	// ResponseID is the numeric response ID.
	ResponseID string `json:"responseId"`
}

// ResponseMetadata omits answers and respondent personal data.
type ResponseMetadata struct {
	// ID is the stable response ID.
	ID string `json:"id"`
	// SurveyID identifies the owning survey.
	SurveyID string `json:"surveyId"`
	// Status is the provider response status.
	Status string `json:"status"`
	// CreatedAt is the provider creation timestamp.
	CreatedAt string `json:"createdAt"`
	// ModifiedAt is the provider modification timestamp.
	ModifiedAt string `json:"modifiedAt"`
}

// ListSurveysOperation retrieves survey metadata.
type ListSurveysOperation struct{ client *Client }

// GetResponseOperation retrieves response metadata.
type GetResponseOperation struct{ client *Client }

// ListSurveys binds this query to the client.
func (client *Client) ListSurveys() ListSurveysOperation { return ListSurveysOperation{client} }

// GetResponse binds this query to the client.
func (client *Client) GetResponse() GetResponseOperation { return GetResponseOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListSurveysOperation) Definition() sdkgo.QueryDefinition { return ListSurveysDefinition }

// Definition returns the manifest-generated response contract.
func (GetResponseOperation) Definition() sdkgo.QueryDefinition { return GetResponseDefinition }

// Invoke reads one page of surveys.
func (operation ListSurveysOperation) Invoke(call sdkgo.Call, input ListSurveysInput) sdkgo.QueryAttempt[SurveysPage] {
	branches := queryBranches{found: ListSurveysBranchListed, rejected: ListSurveysBranchProviderRejected, invalid: ListSurveysBranchInvalidResponse, defect: ListSurveysBranchDefect}
	if input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return queryBranch("listSurveys", branches.defect, SurveysPage{}, sdkgo.FailureValidation, "page or perPage is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PerPage == 0 {
		input.PerPage = 50
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listSurveys", "/surveys?"+query.Encode(), branches, func(body []byte) (SurveysPage, error) { return decodeSurveysPage(body, input) })
}

// Invoke reads response metadata without answers or respondent data.
func (operation GetResponseOperation) Invoke(call sdkgo.Call, input GetResponseInput) sdkgo.QueryAttempt[ResponseMetadata] {
	branches := queryBranches{found: GetResponseBranchFound, notFound: GetResponseBranchNotFound, rejected: GetResponseBranchProviderRejected, invalid: GetResponseBranchInvalidResponse, defect: GetResponseBranchDefect}
	if !numericID.MatchString(input.SurveyID) || !numericID.MatchString(input.ResponseID) {
		return queryBranch("getResponse", branches.defect, ResponseMetadata{}, sdkgo.FailureValidation, "survey and response IDs must be numeric")
	}
	path := "/surveys/" + input.SurveyID + "/responses/" + input.ResponseID
	return invokeQuery(call, operation.client, "getResponse", path, branches, decodeResponse)
}

var numericID = regexp.MustCompile(`^[0-9]{1,24}$`)

func decodeSurveysPage(body []byte, input ListSurveysInput) (SurveysPage, error) {
	var wire struct {
		Data []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
		Page    int `json:"page"`
		PerPage int `json:"per_page"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SurveysPage{}, err
	}
	if wire.Data == nil || wire.Page != input.Page || wire.PerPage < 1 || wire.Total < 0 {
		return SurveysPage{}, fmt.Errorf("survey page missing or inconsistent")
	}
	page := SurveysPage{Surveys: make([]SurveyMetadata, 0, len(wire.Data)), Page: wire.Page, PerPage: wire.PerPage, Total: wire.Total}
	for _, survey := range wire.Data {
		if !numericID.MatchString(survey.ID) {
			return SurveysPage{}, fmt.Errorf("survey ID missing")
		}
		page.Surveys = append(page.Surveys, SurveyMetadata{ID: survey.ID, Title: survey.Title})
	}
	return page, nil
}

func decodeResponse(body []byte) (ResponseMetadata, error) {
	var wire struct {
		ID         string `json:"id"`
		SurveyID   string `json:"survey_id"`
		Status     string `json:"response_status"`
		CreatedAt  string `json:"date_created"`
		ModifiedAt string `json:"date_modified"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ResponseMetadata{}, err
	}
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.SurveyID) || wire.Status == "" {
		return ResponseMetadata{}, fmt.Errorf("response identity or status missing")
	}
	return ResponseMetadata{ID: wire.ID, SurveyID: wire.SurveyID, Status: wire.Status, CreatedAt: wire.CreatedAt, ModifiedAt: wire.ModifiedAt}, nil
}
