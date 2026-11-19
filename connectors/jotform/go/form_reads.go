// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetFormInput selects one numeric Jotform form ID.
type GetFormInput struct {
	// FormID is the numeric ID in the form URL or API.
	FormID string `json:"formId"`
}

// ListFormSubmissionsInput selects a bounded submission page for one form.
type ListFormSubmissionsInput struct {
	// FormID is the numeric ID in the form URL or API.
	FormID string `json:"formId"`
	// Offset is the zero-based submission offset.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum submission count; zero defaults to 20.
	Limit int `json:"limit,omitempty"`
}

// SubmissionsPage contains one page of submission metadata without answers.
type SubmissionsPage struct {
	// Submissions contains metadata for the selected page.
	Submissions []SubmissionMetadata `json:"submissions"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
}

// GetFormOperation retrieves one form's metadata.
type GetFormOperation struct{ client *Client }

// ListFormSubmissionsOperation retrieves one form's submission metadata.
type ListFormSubmissionsOperation struct{ client *Client }

// GetForm binds this query to the client.
func (client *Client) GetForm() GetFormOperation { return GetFormOperation{client} }

// ListFormSubmissions binds this query to the client.
func (client *Client) ListFormSubmissions() ListFormSubmissionsOperation {
	return ListFormSubmissionsOperation{client}
}

// Definition returns the manifest-generated form lookup contract.
func (GetFormOperation) Definition() sdkgo.QueryDefinition { return GetFormDefinition }

// Definition returns the manifest-generated submission list contract.
func (ListFormSubmissionsOperation) Definition() sdkgo.QueryDefinition {
	return ListFormSubmissionsDefinition
}

// Invoke reads one form without its questions or settings.
func (operation GetFormOperation) Invoke(call sdkgo.Call, input GetFormInput) sdkgo.QueryAttempt[FormMetadata] {
	branches := queryBranches{found: GetFormBranchFound, notFound: GetFormBranchNotFound, rejected: GetFormBranchProviderRejected, invalid: GetFormBranchInvalidResponse, defect: GetFormBranchDefect}
	if !numericID.MatchString(input.FormID) {
		return queryBranch("getForm", branches.defect, FormMetadata{}, sdkgo.FailureValidation, "form ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getForm", "/form/"+input.FormID, branches, decodeForm)
}

// Invoke reads one bounded page of submission metadata without answers.
func (operation ListFormSubmissionsOperation) Invoke(call sdkgo.Call, input ListFormSubmissionsInput) sdkgo.QueryAttempt[SubmissionsPage] {
	branches := queryBranches{found: ListFormSubmissionsBranchListed, notFound: ListFormSubmissionsBranchNotFound, rejected: ListFormSubmissionsBranchProviderRejected, invalid: ListFormSubmissionsBranchInvalidResponse, defect: ListFormSubmissionsBranchDefect}
	if !numericID.MatchString(input.FormID) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listFormSubmissions", branches.defect, SubmissionsPage{}, sdkgo.FailureValidation, "form ID, offset, or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"offset": {strconv.Itoa(input.Offset)}, "limit": {strconv.Itoa(input.Limit)}}
	return invokeQuery(call, operation.client, "listFormSubmissions", "/form/"+input.FormID+"/submissions?"+query.Encode(), branches, func(body []byte) (SubmissionsPage, int, error) { return decodeSubmissionsPage(body, input) })
}

func decodeForm(body []byte) (FormMetadata, int, error) {
	var wire struct {
		ResponseCode int          `json:"responseCode"`
		Content      FormMetadata `json:"content"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return FormMetadata{}, 0, err
	}
	if wire.ResponseCode != 200 {
		return FormMetadata{}, wire.ResponseCode, nil
	}
	if !numericID.MatchString(wire.Content.ID) || wire.Content.Title == "" {
		return FormMetadata{}, 0, fmt.Errorf("form identity missing")
	}
	return wire.Content, 200, nil
}

func decodeSubmissionsPage(body []byte, input ListFormSubmissionsInput) (SubmissionsPage, int, error) {
	var wire struct {
		ResponseCode int `json:"responseCode"`
		Content      []struct {
			ID        string `json:"id"`
			FormID    string `json:"form_id"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubmissionsPage{}, 0, err
	}
	if wire.ResponseCode != 200 {
		return SubmissionsPage{}, wire.ResponseCode, nil
	}
	if wire.Content == nil {
		return SubmissionsPage{}, 0, fmt.Errorf("submission list missing")
	}
	page := SubmissionsPage{Submissions: make([]SubmissionMetadata, 0, len(wire.Content)), Offset: input.Offset, Limit: input.Limit}
	for _, submission := range wire.Content {
		if !numericID.MatchString(submission.ID) || submission.FormID != input.FormID || submission.CreatedAt == "" {
			return SubmissionsPage{}, 0, fmt.Errorf("submission identity missing")
		}
		page.Submissions = append(page.Submissions, SubmissionMetadata{ID: submission.ID, FormID: submission.FormID, Status: submission.Status, CreatedAt: submission.CreatedAt})
	}
	return page, 200, nil
}
