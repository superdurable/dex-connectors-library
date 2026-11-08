// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetJobRunInput selects a job run by its non-secret Databricks ID.
type GetJobRunInput struct {
	// RunID is the ID shown in Databricks JobRuns or returned by the API.
	RunID string `json:"runId"`
}

// JobRunStatus excludes task details and other workspace metadata.
type JobRunStatus struct {
	// ID is the Databricks job run ID.
	ID string `json:"id"`
	// ResultState is the terminal result state, empty for nonterminal runs.
	ResultState string `json:"resultState"`
	// Status is the current run state, such as RUNNING.
	Status string `json:"status"`
}

// GetJobRunOperation reads one job run's identity and state.
type GetJobRunOperation struct{ client *Client }

// GetJobRun binds the Query to this client.
func (client *Client) GetJobRun() GetJobRunOperation {
	return GetJobRunOperation{client}
}

// Definition returns the manifest-generated Query job_run.
func (GetJobRunOperation) Definition() sdkgo.QueryDefinition { return GetJobRunDefinition }

// Invoke reads one job_run and classifies retryable and definitive outcomes.
func (operation GetJobRunOperation) Invoke(call sdkgo.Call, input GetJobRunInput) sdkgo.QueryAttempt[JobRunStatus] {
	if !validRunID(input.RunID) {
		return jobRunBranch(GetJobRunBranchDefect, sdkgo.FailureValidation, "job_run ID must be a positive decimal integer")
	}
	response := operation.client.get(call, "/api/2.2/jobs/runs/get?run_id="+url.PathEscape(input.RunID))
	if errors.Is(response.err, errCredential) {
		return jobRunBranch(GetJobRunBranchDefect, sdkgo.FailureAuthentication, "Databricks credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[JobRunStatus](jobRunFailure(sdkgo.FailureTransport, "Databricks request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[JobRunStatus](jobRunFailure(sdkgo.FailureRateLimit, "Databricks rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[JobRunStatus](jobRunFailure(sdkgo.FailureAvailability, "Databricks is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return jobRunBranch(GetJobRunBranchNotFound, sdkgo.FailureNotFound, "Databricks did not expose the job_run")
	}
	if response.status != http.StatusOK {
		return jobRunBranch(GetJobRunBranchProviderRejected, sdkgo.FailureProviderRejection, "Databricks rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return jobRunBranch(GetJobRunBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Databricks response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[JobRunStatus](jobRunFailure(sdkgo.FailureTransport, "Databricks response was interrupted"), 0)
	}
	jobRunStatus, err := decodeJobRun(response.body, input.RunID)
	if err != nil {
		return jobRunBranch(GetJobRunBranchInvalidResponse, sdkgo.FailureProtocol, "Databricks returned invalid job_run JSON")
	}
	return sdkgo.NewQueryBranch(GetJobRunBranchFound, jobRunStatus, nil, sdkgo.Receipt{})
}

func validRunID(runID string) bool {
	if len(runID) == 0 || len(runID) > 19 || runID[0] == '0' {
		return false
	}
	for _, character := range runID {
		if character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	parsedID, err := strconv.ParseInt(runID, 10, 64)
	return err == nil && parsedID <= 9007199254740991
}

func decodeJobRun(body []byte, requestedRunID string) (JobRunStatus, error) {
	var wire struct {
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
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return JobRunStatus{}, err
	}
	if strconv.FormatInt(wire.ID, 10) != requestedRunID || strings.TrimSpace(wire.Status.State) == "" {
		return JobRunStatus{}, fmt.Errorf("job run identity or status missing")
	}
	return JobRunStatus{ID: requestedRunID, ResultState: wire.LegacyState.ResultState, Status: wire.Status.State}, nil
}

func jobRunFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "databricks", Operation: "getJobRun", Message: message}
}

func jobRunBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[JobRunStatus] {
	failure := jobRunFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, JobRunStatus{}, &failure, sdkgo.Receipt{})
}
