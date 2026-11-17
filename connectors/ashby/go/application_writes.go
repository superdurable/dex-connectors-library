// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateApplicationInput considers an existing candidate for an existing job.
type CreateApplicationInput struct {
	// CandidateID is the Ashby candidate UUID.
	CandidateID string `json:"candidateId"`
	// JobID is the Ashby job UUID.
	JobID string `json:"jobId"`
}

// CreateApplicationOperation creates an application with a bounded send count.
type CreateApplicationOperation struct{ client *Client }

// CreateApplication binds the Mutation to this client.
func (client *Client) CreateApplication() CreateApplicationOperation {
	return CreateApplicationOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreateApplicationOperation) Definition() sdkgo.MutationDefinition {
	return CreateApplicationDefinition
}

// IdempotencyKey records Dex call identity; Ashby does not deduplicate by this key.
func (CreateApplicationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateApplicationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an application, allowing at most three possible sends.
func (operation CreateApplicationOperation) Invoke(call sdkgo.Call, input CreateApplicationInput) sdkgo.MutationAttempt[ApplicationSummary] {
	if !validCandidateID(input.CandidateID) || !validCandidateID(input.JobID) {
		return applicationCreateBranch(CreateApplicationBranchDefect, sdkgo.FailureValidation, "candidate or job ID is invalid")
	}
	return operation.client.executeLimitedApplicationWrite(call, "createApplication", "/application.create", input, CreateApplicationDefinition.StepDefaults.ExecuteMethodTimeout, CreateApplicationBranchCreated, CreateApplicationBranchProviderRejected, CreateApplicationBranchDefect, func(output ApplicationSummary) bool {
		return output.CandidateID == input.CandidateID && output.JobID == input.JobID
	}, nil)
}

// ChangeApplicationStageInput selects one interview stage without an archive email.
type ChangeApplicationStageInput struct {
	// ApplicationID is the Ashby application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewStageID is the target stage UUID.
	InterviewStageID string `json:"interviewStageId"`
	// ArchiveReasonID is required by Ashby when the target is an Archived stage.
	ArchiveReasonID string `json:"archiveReasonId,omitempty"`
}

// ChangeApplicationStageOperation moves an application to an interview stage.
type ChangeApplicationStageOperation struct{ client *Client }

// ChangeApplicationStage binds the Mutation to this client.
func (client *Client) ChangeApplicationStage() ChangeApplicationStageOperation {
	return ChangeApplicationStageOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (ChangeApplicationStageOperation) Definition() sdkgo.MutationDefinition {
	return ChangeApplicationStageDefinition
}

// IdempotencyKey records Dex call identity; Ashby does not deduplicate by this key.
func (ChangeApplicationStageOperation) IdempotencyKey(callID sdkgo.CallID, _ ChangeApplicationStageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke moves a stage with a bounded send count because stage automation may run.
func (operation ChangeApplicationStageOperation) Invoke(call sdkgo.Call, input ChangeApplicationStageInput) sdkgo.MutationAttempt[ApplicationSummary] {
	if !validCandidateID(input.ApplicationID) || !validCandidateID(input.InterviewStageID) || input.ArchiveReasonID != "" && !validCandidateID(input.ArchiveReasonID) {
		return applicationSafeBranch[ApplicationSummary]("changeApplicationStage", ChangeApplicationStageBranchDefect, sdkgo.FailureValidation, "application, stage, or archive reason ID is invalid")
	}
	return operation.client.executeLimitedApplicationWrite(call, "changeApplicationStage", "/application.changeStage", input, ChangeApplicationStageDefinition.StepDefaults.ExecuteMethodTimeout, ChangeApplicationStageBranchChanged, ChangeApplicationStageBranchProviderRejected, ChangeApplicationStageBranchDefect, func(output ApplicationSummary) bool {
		return output.ID == input.ApplicationID && output.InterviewStageID == input.InterviewStageID
	}, &input.ApplicationID)
}

// TransferApplicationInput selects an absolute target job, plan, and stage.
type TransferApplicationInput struct {
	// ApplicationID is the existing application UUID.
	ApplicationID string `json:"applicationId"`
	// JobID is the target job UUID.
	JobID string `json:"jobId"`
	// InterviewPlanID is the target job's interview plan UUID.
	InterviewPlanID string `json:"interviewPlanId"`
	// InterviewStageID is a stage UUID in the target plan.
	InterviewStageID string `json:"interviewStageId"`
}

// TransferApplicationOperation moves an application to another job.
type TransferApplicationOperation struct{ client *Client }

// TransferApplication binds the Mutation to this client.
func (client *Client) TransferApplication() TransferApplicationOperation {
	return TransferApplicationOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (TransferApplicationOperation) Definition() sdkgo.MutationDefinition {
	return TransferApplicationDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (TransferApplicationOperation) IdempotencyKey(callID sdkgo.CallID, _ TransferApplicationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke transfers an application without starting automatic activities.
func (operation TransferApplicationOperation) Invoke(call sdkgo.Call, input TransferApplicationInput) sdkgo.MutationAttempt[ApplicationSummary] {
	if !validCandidateID(input.ApplicationID) || !validCandidateID(input.JobID) || !validCandidateID(input.InterviewPlanID) || !validCandidateID(input.InterviewStageID) {
		return applicationSafeBranch[ApplicationSummary]("transferApplication", TransferApplicationBranchDefect, sdkgo.FailureValidation, "transfer IDs or automatic activity setting is invalid")
	}
	request := struct {
		TransferApplicationInput
		StartAutomaticActivities bool `json:"startAutomaticActivities"`
	}{TransferApplicationInput: input, StartAutomaticActivities: false}
	return executeAshbySafeWrite(operation.client, call, "transferApplication", "/application.transfer", request, TransferApplicationBranchTransferred, TransferApplicationBranchProviderRejected, TransferApplicationBranchInvalidResponse, TransferApplicationBranchDefect, decodeApplicationSummary, func(output ApplicationSummary) bool {
		return output.ID == input.ApplicationID && output.JobID == input.JobID && output.InterviewStageID == input.InterviewStageID
	})
}

// SetApplicationSourceInput selects a source UUID for an application.
type SetApplicationSourceInput struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// SourceID is the selected Ashby source UUID.
	SourceID string `json:"sourceId"`
}

// SetApplicationSourceOperation replaces the application's source.
type SetApplicationSourceOperation struct{ client *Client }

// SetApplicationSource binds the Mutation to this client.
func (client *Client) SetApplicationSource() SetApplicationSourceOperation {
	return SetApplicationSourceOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (SetApplicationSourceOperation) Definition() sdkgo.MutationDefinition {
	return SetApplicationSourceDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (SetApplicationSourceOperation) IdempotencyKey(callID sdkgo.CallID, _ SetApplicationSourceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces one source with an absolute UUID.
func (operation SetApplicationSourceOperation) Invoke(call sdkgo.Call, input SetApplicationSourceInput) sdkgo.MutationAttempt[ApplicationSummary] {
	if !validCandidateID(input.ApplicationID) || !validCandidateID(input.SourceID) {
		return applicationSafeBranch[ApplicationSummary]("setApplicationSource", SetApplicationSourceBranchDefect, sdkgo.FailureValidation, "application or source ID is invalid")
	}
	return executeAshbySafeWrite(operation.client, call, "setApplicationSource", "/application.changeSource", input, SetApplicationSourceBranchSet, SetApplicationSourceBranchProviderRejected, SetApplicationSourceBranchInvalidResponse, SetApplicationSourceBranchDefect, decodeApplicationSummary, func(output ApplicationSummary) bool { return output.ID == input.ApplicationID })
}

// DeleteApplicationInput identifies an application to remove.
type DeleteApplicationInput struct {
	// ApplicationID is the Ashby application UUID.
	ApplicationID string `json:"applicationId"`
}

// DeletedApplication identifies the removed application.
type DeletedApplication struct {
	// ApplicationID is the UUID Ashby reports as deleted.
	ApplicationID string `json:"applicationId"`
}

// DeleteApplicationOperation removes one application.
type DeleteApplicationOperation struct{ client *Client }

// DeleteApplication binds the Mutation to this client.
func (client *Client) DeleteApplication() DeleteApplicationOperation {
	return DeleteApplicationOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (DeleteApplicationOperation) Definition() sdkgo.MutationDefinition {
	return DeleteApplicationDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (DeleteApplicationOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteApplicationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the specified application.
func (operation DeleteApplicationOperation) Invoke(call sdkgo.Call, input DeleteApplicationInput) sdkgo.MutationAttempt[DeletedApplication] {
	if !validCandidateID(input.ApplicationID) {
		return applicationSafeBranch[DeletedApplication]("deleteApplication", DeleteApplicationBranchDefect, sdkgo.FailureValidation, "application ID is invalid")
	}
	return executeAshbySafeWrite(operation.client, call, "deleteApplication", "/application.delete", input, DeleteApplicationBranchDeleted, DeleteApplicationBranchProviderRejected, DeleteApplicationBranchInvalidResponse, DeleteApplicationBranchDefect, decodeDeletedApplication, func(output DeletedApplication) bool { return output.ApplicationID == input.ApplicationID })
}

func decodeDeletedApplication(body []byte) (DeletedApplication, string, error) {
	var wire struct {
		Success   *bool              `json:"success"`
		Results   DeletedApplication `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return DeletedApplication{}, "", errors.New("invalid delete response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return DeletedApplication{}, "", errors.New("missing delete rejection code")
		}
		return DeletedApplication{}, wire.ErrorInfo.Code, nil
	}
	if !validCandidateID(wire.Results.ApplicationID) {
		return DeletedApplication{}, "", errors.New("missing deleted application ID")
	}
	return wire.Results, "", nil
}

func applicationSafeBranch[OUT any](operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OUT] {
	failure := jobFailure(operationName, kind, message)
	var zero OUT
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func applicationCreateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ApplicationSummary] {
	return applicationSafeBranch[ApplicationSummary]("createApplication", branch, kind, message)
}

func (client *Client) executeLimitedApplicationWrite(call sdkgo.Call, operationName, path string, input any, timeoutDuration time.Duration, successBranch, rejectionBranch, defectBranch sdkgo.BranchID, matches func(ApplicationSummary) bool, readBackID *string) sdkgo.MutationAttempt[ApplicationSummary] {
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return jobFailure(operationName, kind, message)
	}
	uncertain := func(message string) sdkgo.MutationAttempt[ApplicationSummary] {
		return sdkgo.NewMutationUncertain(ApplicationSummary{}, failure(sdkgo.FailureTransport, message), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, timeoutDuration, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return applicationSafeBranch[ApplicationSummary](operationName, defectBranch, sdkgo.FailureLocalDefect, "application dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return uncertain("application checkpoint could not be trusted")
	}
	if decision.MaximumPriorSends > 0 && readBackID != nil {
		response := client.postOperation(call, "/application.info", map[string]string{"applicationId": *readBackID})
		if response.err == nil && response.status == http.StatusOK {
			output, code, decodeErr := decodeApplicationSummary(response.body)
			if decodeErr == nil && code == "" && matches(output) {
				return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
			}
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertain("application outcome could not be confirmed within the send cap")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.postOperationWithContext(call, requestContext, path, input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[ApplicationSummary](failure(sdkgo.FailureAvailability, "application request was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return uncertain("credential failed after an earlier possible send")
		}
		return applicationSafeBranch[ApplicationSummary](operationName, defectBranch, sdkgo.FailureAuthentication, "Ashby credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ApplicationSummary](failure(sdkgo.FailureAvailability, "application resend checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[ApplicationSummary](failure(kind, "Ashby temporarily rejected the application request"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[ApplicationSummary](failure(sdkgo.FailureTransport, "application write outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return uncertain("an earlier application write may have succeeded")
		}
		return applicationSafeBranch[ApplicationSummary](operationName, rejectionBranch, sdkgo.FailureProviderRejection, "Ashby rejected application request with HTTP "+strconv.Itoa(response.status))
	}
	output, code, err := decodeApplicationSummary(response.body)
	if err != nil || code == "" && !matches(output) {
		return sdkgo.NewMutationRetry[ApplicationSummary](failure(sdkgo.FailureProtocol, "Ashby accepted an application write but returned an unusable response"), 0)
	}
	if code != "" {
		if decision.MaximumPriorSends > 0 {
			return uncertain("an earlier application write may have succeeded")
		}
		return applicationSafeBranch[ApplicationSummary](operationName, rejectionBranch, sdkgo.FailureProviderRejection, "Ashby rejected the application request")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}
