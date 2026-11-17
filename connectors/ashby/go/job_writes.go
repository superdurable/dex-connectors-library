// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateJobInput supplies Ashby's required job title, team, and location.
type CreateJobInput struct {
	// Title is the complete job title, up to 512 bytes.
	Title string `json:"title"`
	// TeamID is the Ashby team or department UUID.
	TeamID string `json:"teamId"`
	// LocationID is the Ashby location UUID.
	LocationID string `json:"locationId"`
}

// CreateJobOperation creates a job with a bounded number of possible sends.
type CreateJobOperation struct{ client *Client }

// CreateJob binds the Mutation to this client.
func (client *Client) CreateJob() CreateJobOperation { return CreateJobOperation{client} }

// Definition returns the manifest-generated Mutation definition.
func (CreateJobOperation) Definition() sdkgo.MutationDefinition { return CreateJobDefinition }

// IdempotencyKey records the Dex call identity; Ashby does not deduplicate by this key.
func (CreateJobOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateJobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a job, allowing at most the definition's number of possible sends.
func (operation CreateJobOperation) Invoke(call sdkgo.Call, input CreateJobInput) sdkgo.MutationAttempt[JobSummary] {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 512 || !validCandidateID(input.TeamID) || !validCandidateID(input.LocationID) {
		return createJobBranch(CreateJobBranchDefect, sdkgo.FailureValidation, "job title, team, or location is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateJobDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createJobBranch(CreateJobBranchDefect, sdkgo.FailureLocalDefect, "Ashby job dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(JobSummary{}, jobFailure("createJob", sdkgo.FailureTransport, "Ashby job checkpoint could not be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(JobSummary{}, jobFailure("createJob", sdkgo.FailureTransport, "Ashby job may have been created without a stable lookup key"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postOperationWithContext(call, requestContext, "/job.create", input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure("createJob", sdkgo.FailureAvailability, "Ashby job create was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(JobSummary{}, jobFailure("createJob", sdkgo.FailureAuthentication, "Ashby credential failed after a possible earlier job create"), sdkgo.Receipt{})
		}
		return createJobBranch(CreateJobBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[JobSummary](jobFailure("createJob", sdkgo.FailureAvailability, "Ashby job resend checkpoint was unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[JobSummary](jobFailure("createJob", kind, "Ashby temporarily rejected job creation"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure("createJob", sdkgo.FailureTransport, "Ashby job create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(JobSummary{}, jobFailure("createJob", sdkgo.FailureProviderRejection, "an earlier job create may have succeeded"), sdkgo.Receipt{})
		}
		return createJobBranch(CreateJobBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected job creation with HTTP "+strconv.Itoa(response.status))
	}
	job, rejectionCode, err := decodeJobSummary(response.body)
	if err != nil || rejectionCode == "" && job.Title != input.Title {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure("createJob", sdkgo.FailureProtocol, "Ashby accepted job creation but returned an unusable response"), 0)
	}
	if rejectionCode != "" {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(JobSummary{}, jobFailure("createJob", sdkgo.FailureProviderRejection, "an earlier job create may have succeeded"), sdkgo.Receipt{})
		}
		return createJobBranch(CreateJobBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected job creation")
	}
	return sdkgo.NewMutationBranch(CreateJobBranchCreated, job, nil, sdkgo.Receipt{})
}

// UpdateJobTitleInput replaces a job title with one absolute value.
type UpdateJobTitleInput struct {
	// JobID is the non-secret Ashby job UUID.
	JobID string `json:"jobId"`
	// Title is the complete new title, up to 512 bytes.
	Title string `json:"title"`
}

// UpdateJobTitleOperation sets a job's complete title.
type UpdateJobTitleOperation struct{ client *Client }

// UpdateJobTitle binds the Mutation to this client.
func (client *Client) UpdateJobTitle() UpdateJobTitleOperation {
	return UpdateJobTitleOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UpdateJobTitleOperation) Definition() sdkgo.MutationDefinition { return UpdateJobTitleDefinition }

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (UpdateJobTitleOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateJobTitleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the job title and safely repeats the absolute write.
func (operation UpdateJobTitleOperation) Invoke(call sdkgo.Call, input UpdateJobTitleInput) sdkgo.MutationAttempt[JobSummary] {
	branches := jobWriteBranches{UpdateJobTitleBranchUpdated, UpdateJobTitleBranchProviderRejected, UpdateJobTitleBranchInvalidResponse, UpdateJobTitleBranchDefect}
	if !validCandidateID(input.JobID) || strings.TrimSpace(input.Title) == "" || len(input.Title) > 512 {
		return jobWriteBranch("updateJobTitle", branches.defect, sdkgo.FailureValidation, "job ID or title is invalid")
	}
	return operation.client.executeSafeJobWrite(call, "updateJobTitle", "/job.update", input, branches, func(job JobSummary) bool { return strings.EqualFold(job.ID, input.JobID) && job.Title == input.Title })
}

// SetJobStatusInput selects one absolute job status.
type SetJobStatusInput struct {
	// JobID is the non-secret Ashby job UUID.
	JobID string `json:"jobId"`
	// Status is Draft, Open, Closed, or Archived.
	Status string `json:"status"`
	// CloseReasonID is an optional close-reason UUID, valid only for Closed.
	CloseReasonID string `json:"closeReasonId,omitempty"`
}

// SetJobStatusOperation sets the job's status to an absolute value.
type SetJobStatusOperation struct{ client *Client }

// SetJobStatus binds the Mutation to this client.
func (client *Client) SetJobStatus() SetJobStatusOperation { return SetJobStatusOperation{client} }

// Definition returns the manifest-generated Mutation definition.
func (SetJobStatusOperation) Definition() sdkgo.MutationDefinition { return SetJobStatusDefinition }

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (SetJobStatusOperation) IdempotencyKey(callID sdkgo.CallID, _ SetJobStatusInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets a job status and safely repeats the same absolute value.
func (operation SetJobStatusOperation) Invoke(call sdkgo.Call, input SetJobStatusInput) sdkgo.MutationAttempt[JobSummary] {
	branches := jobWriteBranches{SetJobStatusBranchSet, SetJobStatusBranchProviderRejected, SetJobStatusBranchInvalidResponse, SetJobStatusBranchDefect}
	if !validCandidateID(input.JobID) || !validJobStatus(input.Status) || input.CloseReasonID != "" && (input.Status != "Closed" || !validCandidateID(input.CloseReasonID)) {
		return jobWriteBranch("setJobStatus", branches.defect, sdkgo.FailureValidation, "job ID, status, or close reason is invalid")
	}
	return operation.client.executeSafeJobWrite(call, "setJobStatus", "/job.setStatus", input, branches, func(job JobSummary) bool { return strings.EqualFold(job.ID, input.JobID) && job.Status == input.Status })
}

type jobWriteBranches struct{ success, rejected, invalid, defect sdkgo.BranchID }

func (client *Client) executeSafeJobWrite(call sdkgo.Call, operationName string, operationPath string, input any, branches jobWriteBranches, matches func(JobSummary) bool) sdkgo.MutationAttempt[JobSummary] {
	response := client.postOperation(call, operationPath, input)
	if errors.Is(response.err, errCredential) {
		return jobWriteBranch(operationName, branches.defect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure(operationName, sdkgo.FailureTransport, "Ashby job write request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure(operationName, sdkgo.FailureRateLimit, "Ashby rate limited the job write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure(operationName, sdkgo.FailureAvailability, "Ashby job write is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return jobWriteBranch(operationName, branches.rejected, sdkgo.FailureProviderRejection, "Ashby rejected the job write with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return jobWriteBranch(operationName, branches.invalid, sdkgo.FailureResponseTooLarge, "Ashby job write response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[JobSummary](jobFailure(operationName, sdkgo.FailureTransport, "Ashby job write response was interrupted"), 0)
	}
	job, rejectionCode, err := decodeJobSummary(response.body)
	if err != nil || rejectionCode == "" && !matches(job) {
		return jobWriteBranch(operationName, branches.invalid, sdkgo.FailureProtocol, "Ashby returned an invalid updated job")
	}
	if rejectionCode != "" {
		return jobWriteBranch(operationName, branches.rejected, sdkgo.FailureProviderRejection, "Ashby rejected the job write")
	}
	return sdkgo.NewMutationBranch(branches.success, job, nil, sdkgo.Receipt{})
}

func validJobStatus(status string) bool {
	switch status {
	case "Draft", "Open", "Closed", "Archived":
		return true
	}
	return false
}

func createJobBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[JobSummary] {
	return jobWriteBranch("createJob", branch, kind, message)
}

func jobWriteBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[JobSummary] {
	failure := jobFailure(operationName, kind, message)
	return sdkgo.NewMutationBranch(branch, JobSummary{}, &failure, sdkgo.Receipt{})
}
