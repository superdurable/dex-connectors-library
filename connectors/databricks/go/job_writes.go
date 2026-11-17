// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// RunJobInput identifies a saved job to start with its stored parameters.
type RunJobInput struct {
	// JobID is the decimal job ID shown under Jobs & Pipelines.
	JobID string `json:"jobId"`
}

// UpdateJobNameInput replaces only a saved job's visible name.
type UpdateJobNameInput struct {
	// JobID is the decimal ID of the saved job.
	JobID string `json:"jobId"`
	// Name is the complete replacement job name, 1 to 255 UTF-8 bytes.
	Name string `json:"name"`
}

// DeleteJobInput selects a saved job by immutable ID.
type DeleteJobInput struct {
	// JobID is the decimal ID of the saved job.
	JobID string `json:"jobId"`
}

// DeletedJob identifies the saved job removed by Databricks.
type DeletedJob struct {
	// ID is the decimal job ID.
	ID string `json:"id"`
}

// StartedJobRun identifies the run accepted by Databricks.
type StartedJobRun struct {
	// RunID is the decimal job run ID.
	RunID string `json:"runId"`
}

// CancelJobRunInput selects a run to cancel.
type CancelJobRunInput struct {
	// RunID is the decimal run ID shown on the job's Runs page.
	RunID string `json:"runId"`
}

// CancelledJobRun identifies the run whose cancellation request was accepted.
type CancelledJobRun struct {
	// RunID is the requested decimal run ID.
	RunID string `json:"runId"`
}

// RunJobOperation starts a saved job with provider idempotency.
type RunJobOperation struct{ client *Client }

// CancelJobRunOperation requests cancellation of a job run.
type CancelJobRunOperation struct{ client *Client }

// UpdateJobNameOperation replaces a saved job's name.
type UpdateJobNameOperation struct{ client *Client }

// DeleteJobOperation removes a saved job.
type DeleteJobOperation struct{ client *Client }

// RunJob binds the mutation to this client.
func (client *Client) RunJob() RunJobOperation { return RunJobOperation{client} }

// CancelJobRun binds the mutation to this client.
func (client *Client) CancelJobRun() CancelJobRunOperation { return CancelJobRunOperation{client} }

// UpdateJobName binds the mutation to this client.
func (client *Client) UpdateJobName() UpdateJobNameOperation { return UpdateJobNameOperation{client} }

// DeleteJob binds the mutation to this client.
func (client *Client) DeleteJob() DeleteJobOperation { return DeleteJobOperation{client} }

// Definition returns the generated RunJob mutation definition.
func (RunJobOperation) Definition() sdkgo.MutationDefinition { return RunJobDefinition }

// Definition returns the generated CancelJobRun mutation definition.
func (CancelJobRunOperation) Definition() sdkgo.MutationDefinition { return CancelJobRunDefinition }

// Definition returns the generated UpdateJobName mutation definition.
func (UpdateJobNameOperation) Definition() sdkgo.MutationDefinition { return UpdateJobNameDefinition }

// Definition returns the generated DeleteJob mutation definition.
func (DeleteJobOperation) Definition() sdkgo.MutationDefinition { return DeleteJobDefinition }

// IdempotencyKey derives the provider token from the stable Dex call ID.
func (RunJobOperation) IdempotencyKey(callID sdkgo.CallID, _ RunJobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the repeated cancellation attempt in its receipt.
func (CancelJobRunOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelJobRunInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates the absolute replacement across retries.
func (UpdateJobNameOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateJobNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates repeated deletion by immutable ID.
func (DeleteJobOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteJobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces only the job name, preserving the task and schedule settings.
func (operation UpdateJobNameOperation) Invoke(call sdkgo.Call, input UpdateJobNameInput) sdkgo.MutationAttempt[JobSummary] {
	if !validRunID(input.JobID) || strings.TrimSpace(input.Name) == "" || !validListText(input.Name, 255) {
		return mutationBranch[JobSummary](UpdateJobNameBranchDefect, "updateJobName", sdkgo.FailureValidation, "valid job ID and replacement name are required")
	}
	body, err := json.Marshal(struct {
		JobID       json.Number `json:"job_id"`
		NewSettings struct {
			Name string `json:"name"`
		} `json:"new_settings"`
	}{JobID: json.Number(input.JobID), NewSettings: struct {
		Name string `json:"name"`
	}{Name: input.Name}})
	if err != nil {
		return mutationBranch[JobSummary](UpdateJobNameBranchDefect, "updateJobName", sdkgo.FailureLocalDefect, "job update could not be encoded")
	}
	response := operation.client.post(call, "/api/2.2/jobs/update", body)
	if attempt, handled := classifyMutationResponse[JobSummary](response, "updateJobName", UpdateJobNameBranchProviderRejected, UpdateJobNameBranchDefect, UpdateJobNameBranchNotFound); handled {
		return attempt
	}
	if response.err != nil {
		return mutationRetry[JobSummary]("updateJobName", sdkgo.FailureTransport, "Databricks update response was interrupted", response.retryAfter)
	}
	return sdkgo.NewMutationBranch(UpdateJobNameBranchUpdated, JobSummary{ID: input.JobID, Name: input.Name}, nil, jobReceipt(call, input.JobID))
}

// Invoke deletes one saved job; repeating deletion targets the same ID.
func (operation DeleteJobOperation) Invoke(call sdkgo.Call, input DeleteJobInput) sdkgo.MutationAttempt[DeletedJob] {
	if !validRunID(input.JobID) {
		return mutationBranch[DeletedJob](DeleteJobBranchDefect, "deleteJob", sdkgo.FailureValidation, "job ID must be a positive decimal integer")
	}
	body, err := json.Marshal(struct {
		JobID json.Number `json:"job_id"`
	}{JobID: json.Number(input.JobID)})
	if err != nil {
		return mutationBranch[DeletedJob](DeleteJobBranchDefect, "deleteJob", sdkgo.FailureLocalDefect, "job deletion could not be encoded")
	}
	response := operation.client.post(call, "/api/2.2/jobs/delete", body)
	if attempt, handled := classifyMutationResponse[DeletedJob](response, "deleteJob", DeleteJobBranchProviderRejected, DeleteJobBranchDefect, DeleteJobBranchNotFound); handled {
		return attempt
	}
	if response.err != nil {
		return mutationRetry[DeletedJob]("deleteJob", sdkgo.FailureTransport, "Databricks deletion response was interrupted", response.retryAfter)
	}
	return sdkgo.NewMutationBranch(DeleteJobBranchDeleted, DeletedJob{ID: input.JobID}, nil, jobReceipt(call, input.JobID))
}

// Invoke starts a job once; Databricks deduplicates repeat requests by token.
func (operation RunJobOperation) Invoke(call sdkgo.Call, input RunJobInput) sdkgo.MutationAttempt[StartedJobRun] {
	if !validRunID(input.JobID) || len(call.IdempotencyKey) == 0 || len(call.IdempotencyKey) > 64 {
		return mutationBranch[StartedJobRun](RunJobBranchDefect, "runJob", sdkgo.FailureValidation, "valid job ID and stable call identity are required")
	}
	body, err := json.Marshal(struct {
		JobID json.Number `json:"job_id"`
		Token string      `json:"idempotency_token"`
	}{
		JobID: json.Number(input.JobID), Token: string(call.IdempotencyKey),
	})
	if err != nil {
		return mutationBranch[StartedJobRun](RunJobBranchDefect, "runJob", sdkgo.FailureLocalDefect, "Databricks run request could not be encoded")
	}
	response := operation.client.post(call, "/api/2.2/jobs/run-now", body)
	if attempt, handled := classifyMutationResponse[StartedJobRun](response, "runJob", RunJobBranchProviderRejected, RunJobBranchDefect, RunJobBranchNotFound); handled {
		return attempt
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return mutationBranch[StartedJobRun](RunJobBranchInvalidResponse, "runJob", sdkgo.FailureResponseTooLarge, "Databricks run response exceeded the configured limit")
	}
	if response.err != nil {
		return mutationRetry[StartedJobRun]("runJob", sdkgo.FailureTransport, "Databricks run response was interrupted", response.retryAfter)
	}
	var wire struct {
		RunID int64 `json:"run_id"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.RunID <= 0 {
		return mutationBranch[StartedJobRun](RunJobBranchInvalidResponse, "runJob", sdkgo.FailureProtocol, "Databricks returned an invalid run ID")
	}
	runID := strconv.FormatInt(wire.RunID, 10)
	return sdkgo.NewMutationBranch(RunJobBranchStarted, StartedJobRun{RunID: runID}, nil, jobReceipt(call, runID))
}

// Invoke asks Databricks to cancel one run; repeating the request cannot start another run.
func (operation CancelJobRunOperation) Invoke(call sdkgo.Call, input CancelJobRunInput) sdkgo.MutationAttempt[CancelledJobRun] {
	if !validRunID(input.RunID) {
		return mutationBranch[CancelledJobRun](CancelJobRunBranchDefect, "cancelJobRun", sdkgo.FailureValidation, "run ID must be a positive decimal integer")
	}
	body, err := json.Marshal(struct {
		RunID json.Number `json:"run_id"`
	}{RunID: json.Number(input.RunID)})
	if err != nil {
		return mutationBranch[CancelledJobRun](CancelJobRunBranchDefect, "cancelJobRun", sdkgo.FailureLocalDefect, "Databricks cancellation request could not be encoded")
	}
	response := operation.client.post(call, "/api/2.2/jobs/runs/cancel", body)
	if attempt, handled := classifyMutationResponse[CancelledJobRun](response, "cancelJobRun", CancelJobRunBranchProviderRejected, CancelJobRunBranchDefect, CancelJobRunBranchNotFound); handled {
		return attempt
	}
	if response.err != nil {
		return mutationRetry[CancelledJobRun]("cancelJobRun", sdkgo.FailureTransport, "Databricks cancellation response was interrupted", response.retryAfter)
	}
	return sdkgo.NewMutationBranch(CancelJobRunBranchCancelled, CancelledJobRun{RunID: input.RunID}, nil, jobReceipt(call, input.RunID))
}

func jobReceipt(call sdkgo.Call, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "databricks", ProviderObjectID: objectID, ObservedAt: time.Now().UTC()}
}

func mutationBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "databricks", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func mutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "databricks", Operation: operation, Message: message}, retryAfter)
}

func classifyMutationResponse[T any](response providerResponse, operation string, rejected, defect, notFound sdkgo.BranchID) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return mutationBranch[T](defect, operation, sdkgo.FailureAuthentication, "Databricks credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return mutationRetry[T](operation, sdkgo.FailureTransport, "Databricks request failed", 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return mutationRetry[T](operation, sdkgo.FailureRateLimit, "Databricks rate limited the request", response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return mutationRetry[T](operation, sdkgo.FailureAvailability, "Databricks is temporarily unavailable", response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		return mutationBranch[T](notFound, operation, sdkgo.FailureNotFound, "Databricks did not expose the resource"), true
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return mutationBranch[T](rejected, operation, sdkgo.FailureProviderRejection, "Databricks rejected HTTP "+strconv.Itoa(response.status)), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}
