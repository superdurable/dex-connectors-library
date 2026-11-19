// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListJobsInput selects one page of jobs. Empty fields use the provider's first page and default size.
type ListJobsInput struct {
	// Name filters by exact job name, ignoring case. Blank includes all visible jobs.
	Name string `json:"name,omitempty"`
	// PageToken is the opaque continuation returned by a prior page. Blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
	// Limit is a page size from 1 through 100. Zero uses the Databricks default of 20.
	Limit int `json:"limit,omitempty"`
}

// JobSummary exposes a job's ID and name without its task settings or workspace metadata.
type JobSummary struct {
	// ID is the decimal Databricks job ID.
	ID string `json:"id"`
	// Name is the job name.
	Name string `json:"name"`
}

// JobPage is one bounded page of jobs.
type JobPage struct {
	// Jobs is empty when no jobs match.
	Jobs []JobSummary `json:"jobs"`
	// NextPageToken is blank after the last page.
	NextPageToken string `json:"nextPageToken"`
}

// GetJobInput identifies one job by its decimal ID.
type GetJobInput struct {
	// JobID is the numeric ID shown under Jobs & Pipelines.
	JobID string `json:"jobId"`
}

// ListJobRunsInput selects one page of runs for one job.
type ListJobRunsInput struct {
	// JobID identifies the job whose runs are listed.
	JobID string `json:"jobId"`
	// PageToken is a continuation returned by the previous page; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
	// Limit is a page size from 1 through 25; zero uses the provider default of 20.
	Limit int `json:"limit,omitempty"`
}

// JobRunPage contains bounded run summaries and the next-page token.
type JobRunPage struct {
	// Runs contains this page of runs, possibly empty.
	Runs []JobRunStatus `json:"runs"`
	// NextPageToken is blank after the last page.
	NextPageToken string `json:"nextPageToken"`
}

// ListJobsOperation reads one page of visible jobs.
type ListJobsOperation struct{ client *Client }

// GetJobOperation reads one job summary.
type GetJobOperation struct{ client *Client }

// ListJobRunsOperation reads one page of runs for a job.
type ListJobRunsOperation struct{ client *Client }

// ListJobs binds the query to this client.
func (client *Client) ListJobs() ListJobsOperation { return ListJobsOperation{client} }

// GetJob binds the query to this client.
func (client *Client) GetJob() GetJobOperation { return GetJobOperation{client} }

// ListJobRuns binds the query to this client.
func (client *Client) ListJobRuns() ListJobRunsOperation { return ListJobRunsOperation{client} }

// Definition returns the generated ListJobs query definition.
func (ListJobsOperation) Definition() sdkgo.QueryDefinition { return ListJobsDefinition }

// Definition returns the generated GetJob query definition.
func (GetJobOperation) Definition() sdkgo.QueryDefinition { return GetJobDefinition }

// Definition returns the generated ListJobRuns query definition.
func (ListJobRunsOperation) Definition() sdkgo.QueryDefinition { return ListJobRunsDefinition }

// Invoke requests a job page and rejects malformed IDs or continuation tokens.
func (operation ListJobsOperation) Invoke(call sdkgo.Call, input ListJobsInput) sdkgo.QueryAttempt[JobPage] {
	if input.Limit < 0 || input.Limit > 100 || !validListText(input.Name, 4096) || !validListText(input.PageToken, 4096) {
		return queryBranch[JobPage](ListJobsBranchDefect, "listJobs", sdkgo.FailureValidation, "invalid name, page token, or limit")
	}
	values := url.Values{}
	if input.Name != "" {
		values.Set("name", input.Name)
	}
	if input.PageToken != "" {
		values.Set("page_token", input.PageToken)
	}
	if input.Limit != 0 {
		values.Set("limit", strconv.Itoa(input.Limit))
	}
	path := "/api/2.2/jobs/list"
	if len(values) != 0 {
		path += "?" + values.Encode()
	}
	response := operation.client.get(call, path)
	if attempt, handled := classifyQueryResponse[JobPage](response, "listJobs", ListJobsBranchProviderRejected, ListJobsBranchInvalidResponse, ListJobsBranchDefect, ""); handled {
		return attempt
	}
	var wire struct {
		Jobs []struct {
			ID       int64 `json:"job_id"`
			Settings struct {
				Name string `json:"name"`
			} `json:"settings"`
		} `json:"jobs"`
		NextPageToken string `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !validListText(wire.NextPageToken, 4096) || len(wire.Jobs) > 100 {
		return queryBranch[JobPage](ListJobsBranchInvalidResponse, "listJobs", sdkgo.FailureProtocol, "Databricks returned an invalid job page")
	}
	page := JobPage{Jobs: make([]JobSummary, 0, len(wire.Jobs)), NextPageToken: wire.NextPageToken}
	for _, job := range wire.Jobs {
		if job.ID <= 0 || strings.TrimSpace(job.Settings.Name) == "" {
			return queryBranch[JobPage](ListJobsBranchInvalidResponse, "listJobs", sdkgo.FailureProtocol, "Databricks returned an invalid job")
		}
		page.Jobs = append(page.Jobs, JobSummary{ID: strconv.FormatInt(job.ID, 10), Name: job.Settings.Name})
	}
	return sdkgo.NewQueryBranch(ListJobsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one job's ID and name without returning its settings.
func (operation GetJobOperation) Invoke(call sdkgo.Call, input GetJobInput) sdkgo.QueryAttempt[JobSummary] {
	if !validRunID(input.JobID) {
		return queryBranch[JobSummary](GetJobBranchDefect, "getJob", sdkgo.FailureValidation, "job ID must be a positive decimal integer")
	}
	response := operation.client.get(call, "/api/2.2/jobs/get?job_id="+url.QueryEscape(input.JobID))
	if attempt, handled := classifyQueryResponse[JobSummary](response, "getJob", GetJobBranchProviderRejected, GetJobBranchInvalidResponse, GetJobBranchDefect, GetJobBranchNotFound); handled {
		return attempt
	}
	var wire struct {
		ID       int64 `json:"job_id"`
		Settings struct {
			Name string `json:"name"`
		} `json:"settings"`
	}
	if json.Unmarshal(response.body, &wire) != nil || strconv.FormatInt(wire.ID, 10) != input.JobID || strings.TrimSpace(wire.Settings.Name) == "" {
		return queryBranch[JobSummary](GetJobBranchInvalidResponse, "getJob", sdkgo.FailureProtocol, "Databricks returned an invalid job")
	}
	return sdkgo.NewQueryBranch(GetJobBranchFound, JobSummary{ID: input.JobID, Name: wire.Settings.Name}, nil, sdkgo.Receipt{})
}

// Invoke reads one page of run identities and states without task or user metadata.
func (operation ListJobRunsOperation) Invoke(call sdkgo.Call, input ListJobRunsInput) sdkgo.QueryAttempt[JobRunPage] {
	if !validRunID(input.JobID) || input.Limit < 0 || input.Limit > 25 || !validListText(input.PageToken, 4096) {
		return queryBranch[JobRunPage](ListJobRunsBranchDefect, "listJobRuns", sdkgo.FailureValidation, "invalid job ID, page token, or limit")
	}
	values := url.Values{"job_id": {input.JobID}}
	if input.PageToken != "" {
		values.Set("page_token", input.PageToken)
	}
	if input.Limit != 0 {
		values.Set("limit", strconv.Itoa(input.Limit))
	}
	response := operation.client.get(call, "/api/2.2/jobs/runs/list?"+values.Encode())
	if attempt, handled := classifyQueryResponse[JobRunPage](response, "listJobRuns", ListJobRunsBranchProviderRejected, ListJobRunsBranchInvalidResponse, ListJobRunsBranchDefect, ""); handled {
		return attempt
	}
	var wire struct {
		Runs []struct {
			ID     int64 `json:"run_id"`
			Status struct {
				State              string `json:"state"`
				TerminationDetails struct {
					Code string `json:"code"`
				} `json:"termination_details"`
			} `json:"status"`
			LegacyState struct {
				ResultState string `json:"result_state"`
			} `json:"state"`
		} `json:"runs"`
		NextPageToken string `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !validListText(wire.NextPageToken, 4096) || len(wire.Runs) > 25 {
		return queryBranch[JobRunPage](ListJobRunsBranchInvalidResponse, "listJobRuns", sdkgo.FailureProtocol, "Databricks returned an invalid run page")
	}
	page := JobRunPage{Runs: make([]JobRunStatus, 0, len(wire.Runs)), NextPageToken: wire.NextPageToken}
	for _, run := range wire.Runs {
		if run.ID <= 0 || strings.TrimSpace(run.Status.State) == "" {
			return queryBranch[JobRunPage](ListJobRunsBranchInvalidResponse, "listJobRuns", sdkgo.FailureProtocol, "Databricks returned an invalid run")
		}
		page.Runs = append(page.Runs, JobRunStatus{ID: strconv.FormatInt(run.ID, 10), Status: run.Status.State, ResultState: run.LegacyState.ResultState})
	}
	return sdkgo.NewQueryBranch(ListJobRunsBranchListed, page, nil, sdkgo.Receipt{})
}

func validListText(value string, maximumBytes int) bool {
	return len(value) <= maximumBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func queryBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "databricks", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func classifyQueryResponse[T any](response providerResponse, operation string, rejected, invalid, defect, notFound sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return queryBranch[T](defect, operation, sdkgo.FailureAuthentication, "Databricks credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "databricks", Operation: operation, Message: "Databricks request failed"}, 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Provider: "databricks", Operation: operation, Message: "Databricks rate limited the request"}, response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "databricks", Operation: operation, Message: "Databricks is temporarily unavailable"}, response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return queryBranch[T](notFound, operation, sdkgo.FailureNotFound, "Databricks did not expose the resource"), true
	}
	if response.status != http.StatusOK {
		return queryBranch[T](rejected, operation, sdkgo.FailureProviderRejection, "Databricks rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch[T](invalid, operation, sdkgo.FailureResponseTooLarge, "Databricks response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "databricks", Operation: operation, Message: "Databricks response was interrupted"}, 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}
