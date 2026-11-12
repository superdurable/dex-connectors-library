// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListFormsInput selects a zero-based page of at most 100 forms.
type ListFormsInput struct {
	// Offset is the zero-based form offset; zero starts the first page.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum form count; zero defaults to 20.
	Limit int `json:"limit,omitempty"`
}

// FormMetadata contains public form identity and status without questions or answers.
type FormMetadata struct {
	// ID is the provider form ID.
	ID string `json:"id"`
	// Title is the form's untrusted display title.
	Title string `json:"title"`
	// Status is the provider form status.
	Status string `json:"status"`
}

// FormsPage is one offset-based Jotform form page.
type FormsPage struct {
	// Forms contains the selected page.
	Forms []FormMetadata `json:"forms"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
}

// GetSubmissionInput selects one numeric Jotform submission ID.
type GetSubmissionInput struct {
	// SubmissionID is the numeric ID from Jotform's submission URL or API.
	SubmissionID string `json:"submissionId"`
}

// SubmissionMetadata omits answers, which may contain secrets or personal data.
type SubmissionMetadata struct {
	// ID is the stable submission ID.
	ID string `json:"id"`
	// FormID identifies the owning form.
	FormID string `json:"formId"`
	// Status is the provider submission status.
	Status string `json:"status"`
	// CreatedAt is Jotform's raw creation timestamp.
	CreatedAt string `json:"createdAt"`
}

// ListFormsOperation retrieves form metadata.
type ListFormsOperation struct{ client *Client }

// GetSubmissionOperation retrieves submission metadata.
type GetSubmissionOperation struct{ client *Client }

// ListForms binds this query to the client.
func (client *Client) ListForms() ListFormsOperation { return ListFormsOperation{client} }

// GetSubmission binds this query to the client.
func (client *Client) GetSubmission() GetSubmissionOperation { return GetSubmissionOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListFormsOperation) Definition() sdkgo.QueryDefinition { return ListFormsDefinition }

// Definition returns the manifest-generated submission contract.
func (GetSubmissionOperation) Definition() sdkgo.QueryDefinition { return GetSubmissionDefinition }

// Invoke reads one page of forms.
func (operation ListFormsOperation) Invoke(call sdkgo.Call, input ListFormsInput) sdkgo.QueryAttempt[FormsPage] {
	branches := queryBranches{found: ListFormsBranchListed, rejected: ListFormsBranchProviderRejected, invalid: ListFormsBranchInvalidResponse, defect: ListFormsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listForms", branches.defect, FormsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"offset": {strconv.Itoa(input.Offset)}, "limit": {strconv.Itoa(input.Limit)}}
	return invokeQuery(call, operation.client, "listForms", "/user/forms?"+query.Encode(), branches, func(body []byte) (FormsPage, int, error) { return decodeFormsPage(body, input) })
}

// Invoke reads a submission's identity and status without its answers.
func (operation GetSubmissionOperation) Invoke(call sdkgo.Call, input GetSubmissionInput) sdkgo.QueryAttempt[SubmissionMetadata] {
	branches := queryBranches{found: GetSubmissionBranchFound, notFound: GetSubmissionBranchNotFound, rejected: GetSubmissionBranchProviderRejected, invalid: GetSubmissionBranchInvalidResponse, defect: GetSubmissionBranchDefect}
	if !numericID.MatchString(input.SubmissionID) {
		return queryBranch("getSubmission", branches.defect, SubmissionMetadata{}, sdkgo.FailureValidation, "submission ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getSubmission", "/submission/"+input.SubmissionID, branches, decodeSubmission)
}

var numericID = regexp.MustCompile(`^[0-9]{1,24}$`)

func decodeFormsPage(body []byte, input ListFormsInput) (FormsPage, int, error) {
	var wire struct {
		ResponseCode int `json:"responseCode"`
		Content      []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return FormsPage{}, 0, err
	}
	if wire.ResponseCode != 200 {
		return FormsPage{}, wire.ResponseCode, nil
	}
	if wire.Content == nil {
		return FormsPage{}, 0, fmt.Errorf("form list missing")
	}
	page := FormsPage{Forms: make([]FormMetadata, 0, len(wire.Content)), Offset: input.Offset, Limit: input.Limit}
	for _, form := range wire.Content {
		if !numericID.MatchString(form.ID) || form.Title == "" {
			return FormsPage{}, 0, fmt.Errorf("form identity missing")
		}
		page.Forms = append(page.Forms, FormMetadata{ID: form.ID, Title: form.Title, Status: form.Status})
	}
	return page, 200, nil
}

func decodeSubmission(body []byte) (SubmissionMetadata, int, error) {
	var wire struct {
		ResponseCode int `json:"responseCode"`
		Content      struct {
			ID        string `json:"id"`
			FormID    string `json:"form_id"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SubmissionMetadata{}, 0, err
	}
	if wire.ResponseCode != 200 {
		return SubmissionMetadata{}, wire.ResponseCode, nil
	}
	if !numericID.MatchString(wire.Content.ID) || !numericID.MatchString(wire.Content.FormID) || wire.Content.CreatedAt == "" {
		return SubmissionMetadata{}, 0, fmt.Errorf("submission identity missing")
	}
	return SubmissionMetadata{ID: wire.Content.ID, FormID: wire.Content.FormID, Status: wire.Content.Status, CreatedAt: wire.Content.CreatedAt}, 200, nil
}
