// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// JobSummary exposes the job identity, title, status, and timestamps only.
type JobSummary struct {
	// ID is Ashby's job UUID.
	ID string `json:"id"`
	// Title is the job's display title.
	Title string `json:"title"`
	// Status is Draft, Open, Closed, or Archived.
	Status string `json:"status"`
	// CreatedAt is Ashby's UTC creation timestamp.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is Ashby's UTC update timestamp.
	UpdatedAt string `json:"updatedAt"`
}

// ListJobsInput selects one bounded page of jobs.
type ListJobsInput struct {
	// Cursor is an opaque Ashby page cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps a page at 100 jobs; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
}

// JobPage carries job identities and an optional next cursor.
type JobPage struct {
	// Jobs contains only job identity fields.
	Jobs []JobSummary `json:"jobs"`
	// NextCursor is passed to the next request when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListJobsOperation reads a bounded page of jobs.
type ListJobsOperation struct{ client *Client }

// ListJobs binds the Query to this client.
func (client *Client) ListJobs() ListJobsOperation { return ListJobsOperation{client} }

// Definition returns the manifest-generated Query definition.
func (ListJobsOperation) Definition() sdkgo.QueryDefinition { return ListJobsDefinition }

// Invoke reads a job page and classifies provider outcomes.
func (operation ListJobsOperation) Invoke(call sdkgo.Call, input ListJobsInput) sdkgo.QueryAttempt[JobPage] {
	branches := jobQueryBranches{ListJobsBranchListed, ListJobsBranchProviderRejected, ListJobsBranchInvalidResponse, ListJobsBranchDefect}
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return jobQueryBranch[JobPage]("listJobs", branches.defect, sdkgo.FailureValidation, "job page input is invalid")
	}
	return executeJobQuery(operation.client, call, "listJobs", "/job.list", input, branches, decodeJobPage)
}

// GetJobInput identifies the job to inspect.
type GetJobInput struct {
	// JobID is the non-secret Ashby job UUID.
	JobID string `json:"jobId"`
}

// GetJobOperation reads one job.
type GetJobOperation struct{ client *Client }

// GetJob binds the Query to this client.
func (client *Client) GetJob() GetJobOperation { return GetJobOperation{client} }

// Definition returns the manifest-generated Query definition.
func (GetJobOperation) Definition() sdkgo.QueryDefinition { return GetJobDefinition }

// Invoke reads a job without exposing hiring-team or compensation fields.
func (operation GetJobOperation) Invoke(call sdkgo.Call, input GetJobInput) sdkgo.QueryAttempt[JobSummary] {
	branches := jobQueryBranches{GetJobBranchFound, GetJobBranchProviderRejected, GetJobBranchInvalidResponse, GetJobBranchDefect}
	if !validCandidateID(input.JobID) {
		return jobQueryBranch[JobSummary]("getJob", branches.defect, sdkgo.FailureValidation, "job ID is invalid")
	}
	return executeJobQuery(operation.client, call, "getJob", "/job.info", map[string]string{"id": input.JobID}, branches, decodeJobSummary)
}

// SearchJobsInput selects matching jobs by title, requisition ID, or both.
type SearchJobsInput struct {
	// Title is a title or substring, up to 512 bytes.
	Title string `json:"title,omitempty"`
	// RequisitionID is an optional custom requisition ID or substring, up to 256 bytes.
	RequisitionID string `json:"requisitionId,omitempty"`
	// Limit caps the search at 100 jobs; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
}

// JobSearchResults contains matching job identities.
type JobSearchResults struct {
	// Jobs omits hiring-team and compensation details.
	Jobs []JobSummary `json:"jobs"`
}

// SearchJobsOperation reads matching job identities.
type SearchJobsOperation struct{ client *Client }

// SearchJobs binds the Query to this client.
func (client *Client) SearchJobs() SearchJobsOperation { return SearchJobsOperation{client} }

// Definition returns the manifest-generated Query definition.
func (SearchJobsOperation) Definition() sdkgo.QueryDefinition { return SearchJobsDefinition }

// Invoke searches jobs without exposing hiring-team details.
func (operation SearchJobsOperation) Invoke(call sdkgo.Call, input SearchJobsInput) sdkgo.QueryAttempt[JobSearchResults] {
	branches := jobQueryBranches{SearchJobsBranchFound, SearchJobsBranchProviderRejected, SearchJobsBranchInvalidResponse, SearchJobsBranchDefect}
	if strings.TrimSpace(input.Title) == "" && strings.TrimSpace(input.RequisitionID) == "" || len(input.Title) > 512 || len(input.RequisitionID) > 256 || input.Limit < 0 || input.Limit > 100 {
		return jobQueryBranch[JobSearchResults]("searchJobs", branches.defect, sdkgo.FailureValidation, "job search input is invalid")
	}
	return executeJobQuery(operation.client, call, "searchJobs", "/job.search", input, branches, decodeJobSearch)
}

type jobQueryBranches struct {
	success  sdkgo.BranchID
	rejected sdkgo.BranchID
	invalid  sdkgo.BranchID
	defect   sdkgo.BranchID
}

func executeJobQuery[OUT any](client *Client, call sdkgo.Call, operationName string, operationPath string, input any, branches jobQueryBranches, decode func([]byte) (OUT, string, error)) sdkgo.QueryAttempt[OUT] {
	response := client.postOperation(call, operationPath, input)
	if errors.Is(response.err, errCredential) {
		return jobQueryBranch[OUT](operationName, branches.defect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OUT](jobFailure(operationName, sdkgo.FailureTransport, "Ashby query request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OUT](jobFailure(operationName, sdkgo.FailureRateLimit, "Ashby rate limited the query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OUT](jobFailure(operationName, sdkgo.FailureAvailability, "Ashby query endpoint is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return jobQueryBranch[OUT](operationName, branches.rejected, sdkgo.FailureProviderRejection, "Ashby rejected the query with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return jobQueryBranch[OUT](operationName, branches.invalid, sdkgo.FailureResponseTooLarge, "Ashby query response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OUT](jobFailure(operationName, sdkgo.FailureTransport, "Ashby query response was interrupted"), 0)
	}
	output, rejectionCode, err := decode(response.body)
	if err != nil {
		return jobQueryBranch[OUT](operationName, branches.invalid, sdkgo.FailureProtocol, "Ashby returned invalid query JSON")
	}
	if rejectionCode != "" {
		return jobQueryBranch[OUT](operationName, branches.rejected, sdkgo.FailureProviderRejection, "Ashby rejected the query")
	}
	return sdkgo.NewQueryBranch(branches.success, output, nil, sdkgo.Receipt{})
}

func decodeJobSummary(body []byte) (JobSummary, string, error) {
	var wire struct {
		Success   *bool      `json:"success"`
		Results   JobSummary `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return JobSummary{}, "", errors.New("invalid job response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return JobSummary{}, "", errors.New("job rejection code missing")
		}
		return JobSummary{}, wire.ErrorInfo.Code, nil
	}
	if !validJobSummary(wire.Results) {
		return JobSummary{}, "", errors.New("incomplete job")
	}
	return wire.Results, "", nil
}

func decodeJobPage(body []byte) (JobPage, string, error) {
	var wire struct {
		Success           *bool        `json:"success"`
		Results           []JobSummary `json:"results"`
		NextCursor        string       `json:"nextCursor"`
		MoreDataAvailable bool         `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return JobPage{}, "", errors.New("invalid job page")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return JobPage{}, "", errors.New("job rejection code missing")
		}
		return JobPage{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return JobPage{}, "", errors.New("incomplete job page")
	}
	for _, job := range wire.Results {
		if !validJobSummary(job) {
			return JobPage{}, "", errors.New("invalid job in page")
		}
	}
	return JobPage{Jobs: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, "", nil
}

func decodeJobSearch(body []byte) (JobSearchResults, string, error) {
	page, rejectionCode, err := decodeJobPage(body)
	return JobSearchResults{Jobs: page.Jobs}, rejectionCode, err
}

func validJobSummary(job JobSummary) bool {
	if !validCandidateID(job.ID) || strings.TrimSpace(job.Title) == "" || job.CreatedAt == "" || job.UpdatedAt == "" {
		return false
	}
	switch job.Status {
	case "Draft", "Open", "Closed", "Archived":
		return true
	}
	return false
}

func jobFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: operationName, Message: message}
}

func jobQueryBranch[OUT any](operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OUT] {
	failure := jobFailure(operationName, kind, message)
	var zero OUT
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
