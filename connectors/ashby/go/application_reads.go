// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ApplicationSummary exposes only an application's IDs, status, and timestamps.
type ApplicationSummary struct {
	// ID is the Ashby application UUID.
	ID string `json:"id"`
	// CandidateID is the associated candidate UUID.
	CandidateID string `json:"candidateId"`
	// JobID is the associated job UUID.
	JobID string `json:"jobId"`
	// InterviewStageID is the current interview stage UUID.
	InterviewStageID string `json:"interviewStageId"`
	// Status is Lead, Active, Archived, or Hired.
	Status string `json:"status"`
	// CreatedAt is the application creation timestamp.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is the application update timestamp.
	UpdatedAt string `json:"updatedAt"`
}

// ListApplicationsInput selects one bounded application page.
type ListApplicationsInput struct {
	// Cursor is an opaque Ashby page cursor; blank starts the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps a page at 100 applications; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// ApplicationPage contains a bounded page and its continuation cursor.
type ApplicationPage struct {
	// Applications omits candidate contact details and form submissions.
	Applications []ApplicationSummary `json:"applications"`
	// NextCursor is passed to the next request when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListApplicationsOperation reads an application page.
type ListApplicationsOperation struct{ client *Client }

// ListApplications binds the Query to this client.
func (client *Client) ListApplications() ListApplicationsOperation {
	return ListApplicationsOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListApplicationsOperation) Definition() sdkgo.QueryDefinition {
	return ListApplicationsDefinition
}

// Invoke reads a bounded page and classifies provider outcomes.
func (operation ListApplicationsOperation) Invoke(call sdkgo.Call, input ListApplicationsInput) sdkgo.QueryAttempt[ApplicationPage] {
	branches := jobQueryBranches{ListApplicationsBranchListed, ListApplicationsBranchProviderRejected, ListApplicationsBranchInvalidResponse, ListApplicationsBranchDefect}
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return jobQueryBranch[ApplicationPage]("listApplications", branches.defect, sdkgo.FailureValidation, "application page input is invalid")
	}
	return executeJobQuery(operation.client, call, "listApplications", "/application.list", input, branches, decodeApplicationPage)
}

// GetApplicationInput identifies one application.
type GetApplicationInput struct {
	// ApplicationID is a non-secret Ashby application UUID.
	ApplicationID string `json:"applicationId"`
}

// GetApplicationOperation reads one application.
type GetApplicationOperation struct{ client *Client }

// GetApplication binds the Query to this client.
func (client *Client) GetApplication() GetApplicationOperation {
	return GetApplicationOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (GetApplicationOperation) Definition() sdkgo.QueryDefinition { return GetApplicationDefinition }

// Invoke reads one application without contact or form data.
func (operation GetApplicationOperation) Invoke(call sdkgo.Call, input GetApplicationInput) sdkgo.QueryAttempt[ApplicationSummary] {
	branches := jobQueryBranches{GetApplicationBranchFound, GetApplicationBranchProviderRejected, GetApplicationBranchInvalidResponse, GetApplicationBranchDefect}
	if !validCandidateID(input.ApplicationID) {
		return jobQueryBranch[ApplicationSummary]("getApplication", branches.defect, sdkgo.FailureValidation, "application ID is invalid")
	}
	return executeJobQuery(operation.client, call, "getApplication", "/application.info", input, branches, decodeApplicationSummary)
}

type applicationWire struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Status    string `json:"status"`
	Candidate struct {
		ID string `json:"id"`
	} `json:"candidate"`
	Job struct {
		ID string `json:"id"`
	} `json:"job"`
	CurrentInterviewStage struct {
		ID string `json:"id"`
	} `json:"currentInterviewStage"`
}

func (wire applicationWire) summary() (ApplicationSummary, error) {
	summary := ApplicationSummary{ID: wire.ID, CandidateID: wire.Candidate.ID, JobID: wire.Job.ID, InterviewStageID: wire.CurrentInterviewStage.ID, Status: wire.Status, CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt}
	if !validCandidateID(summary.ID) || !validCandidateID(summary.CandidateID) || !validCandidateID(summary.JobID) || !validCandidateID(summary.InterviewStageID) || summary.CreatedAt == "" || summary.UpdatedAt == "" {
		return ApplicationSummary{}, errors.New("incomplete application")
	}
	switch summary.Status {
	case "Lead", "Active", "Archived", "Hired":
		return summary, nil
	}
	return ApplicationSummary{}, errors.New("invalid application status")
}

func decodeApplicationSummary(body []byte) (ApplicationSummary, string, error) {
	var wire struct {
		Success   *bool           `json:"success"`
		Results   applicationWire `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return ApplicationSummary{}, "", errors.New("invalid application response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return ApplicationSummary{}, "", errors.New("missing application rejection code")
		}
		return ApplicationSummary{}, wire.ErrorInfo.Code, nil
	}
	summary, err := wire.Results.summary()
	return summary, "", err
}

func decodeApplicationPage(body []byte) (ApplicationPage, string, error) {
	var wire struct {
		Success           *bool             `json:"success"`
		Results           []applicationWire `json:"results"`
		NextCursor        string            `json:"nextCursor"`
		MoreDataAvailable bool              `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return ApplicationPage{}, "", errors.New("invalid application page")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return ApplicationPage{}, "", errors.New("missing application rejection code")
		}
		return ApplicationPage{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return ApplicationPage{}, "", errors.New("incomplete application page")
	}
	page := ApplicationPage{Applications: make([]ApplicationSummary, 0, len(wire.Results)), NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}
	for _, entry := range wire.Results {
		summary, err := entry.summary()
		if err != nil {
			return ApplicationPage{}, "", err
		}
		page.Applications = append(page.Applications, summary)
	}
	return page, "", nil
}

func executeAshbySafeWrite[OUT any](client *Client, call sdkgo.Call, operationName, path string, input any, successBranch, rejectionBranch, invalidBranch, defectBranch sdkgo.BranchID, decode func([]byte) (OUT, string, error), matches func(OUT) bool) sdkgo.MutationAttempt[OUT] {
	response := client.postOperation(call, path, input)
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return jobFailure(operationName, kind, message)
	}
	branch := func(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OUT] {
		f := failure(kind, message)
		var zero OUT
		return sdkgo.NewMutationBranch(branch, zero, &f, sdkgo.Receipt{})
	}
	if errors.Is(response.err, errCredential) {
		return branch(defectBranch, sdkgo.FailureAuthentication, "Ashby credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[OUT](failure(sdkgo.FailureTransport, "Ashby write request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OUT](failure(sdkgo.FailureRateLimit, "Ashby rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OUT](failure(sdkgo.FailureAvailability, "Ashby write endpoint is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return branch(rejectionBranch, sdkgo.FailureProviderRejection, "Ashby rejected the write with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return branch(invalidBranch, sdkgo.FailureResponseTooLarge, "Ashby write response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OUT](failure(sdkgo.FailureTransport, "Ashby write response was interrupted"), 0)
	}
	output, code, err := decode(response.body)
	if err != nil || code == "" && !matches(output) {
		return branch(invalidBranch, sdkgo.FailureProtocol, "Ashby returned an invalid write result")
	}
	if code != "" {
		return branch(rejectionBranch, sdkgo.FailureProviderRejection, "Ashby rejected the write")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}
