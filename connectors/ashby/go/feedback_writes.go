// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// RequestApplicationFeedbackInput asks one Ashby user for feedback on an application.
type RequestApplicationFeedbackInput struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewID is the interview definition UUID.
	InterviewID string `json:"interviewId"`
	// InterviewerUserID is the Ashby user UUID receiving the request.
	InterviewerUserID string `json:"interviewerUserId"`
}

// FeedbackRequestReceipt identifies the feedback request's interview event.
type FeedbackRequestReceipt struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewEventID is the new event UUID.
	InterviewEventID string `json:"interviewEventId"`
	// InterviewerUserID is the requested user's UUID.
	InterviewerUserID string `json:"interviewerUserId"`
}

// RequestApplicationFeedbackOperation creates a bounded feedback request.
type RequestApplicationFeedbackOperation struct{ client *Client }

// RequestApplicationFeedback binds the Mutation to this client.
func (client *Client) RequestApplicationFeedback() RequestApplicationFeedbackOperation {
	return RequestApplicationFeedbackOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (RequestApplicationFeedbackOperation) Definition() sdkgo.MutationDefinition {
	return RequestApplicationFeedbackDefinition
}

// IdempotencyKey records Dex call identity; Ashby does not deduplicate by this key.
func (RequestApplicationFeedbackOperation) IdempotencyKey(callID sdkgo.CallID, _ RequestApplicationFeedbackInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke requests feedback with at most three possible sends.
func (operation RequestApplicationFeedbackOperation) Invoke(call sdkgo.Call, input RequestApplicationFeedbackInput) sdkgo.MutationAttempt[FeedbackRequestReceipt] {
	if !validCandidateID(input.ApplicationID) || !validCandidateID(input.InterviewID) || !validCandidateID(input.InterviewerUserID) {
		return feedbackRequestBranch(RequestApplicationFeedbackBranchDefect, sdkgo.FailureValidation, "feedback request IDs are invalid")
	}
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return jobFailure("requestApplicationFeedback", kind, message)
	}
	fence, err := singledispatch.NewFence(call, RequestApplicationFeedbackDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return feedbackRequestBranch(RequestApplicationFeedbackBranchDefect, sdkgo.FailureLocalDefect, "feedback request fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(FeedbackRequestReceipt{}, failure(sdkgo.FailureTransport, "feedback request outcome could not be confirmed within the send cap"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postOperationWithContext(call, requestContext, "/applicationFeedbackRequest.create", input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[FeedbackRequestReceipt](failure(sdkgo.FailureAvailability, "feedback request was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(FeedbackRequestReceipt{}, failure(sdkgo.FailureAuthentication, "credential failed after an earlier possible send"), sdkgo.Receipt{})
		}
		return feedbackRequestBranch(RequestApplicationFeedbackBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[FeedbackRequestReceipt](failure(sdkgo.FailureAvailability, "feedback resend checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[FeedbackRequestReceipt](failure(kind, "Ashby temporarily rejected feedback request"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[FeedbackRequestReceipt](failure(sdkgo.FailureTransport, "feedback request outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(FeedbackRequestReceipt{}, failure(sdkgo.FailureProviderRejection, "an earlier feedback request may have succeeded"), sdkgo.Receipt{})
		}
		return feedbackRequestBranch(RequestApplicationFeedbackBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected feedback request with HTTP "+strconv.Itoa(response.status))
	}
	receipt, code, err := decodeFeedbackRequestReceipt(response.body)
	if err != nil || code == "" && (receipt.ApplicationID != input.ApplicationID || receipt.InterviewerUserID != input.InterviewerUserID) {
		return sdkgo.NewMutationRetry[FeedbackRequestReceipt](failure(sdkgo.FailureProtocol, "Ashby accepted feedback request but returned an unusable response"), 0)
	}
	if code != "" {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(FeedbackRequestReceipt{}, failure(sdkgo.FailureProviderRejection, "an earlier feedback request may have succeeded"), sdkgo.Receipt{})
		}
		return feedbackRequestBranch(RequestApplicationFeedbackBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected feedback request")
	}
	return sdkgo.NewMutationBranch(RequestApplicationFeedbackBranchRequested, receipt, nil, sdkgo.Receipt{})
}

// FeedbackFieldSubmission supplies one answer using the Ashby form's field path.
type FeedbackFieldSubmission struct {
	// Path is the exact field path from feedbackFormDefinition.info.
	Path string `json:"path"`
	// Value is JSON in the field's declared type; omit secrets.
	Value json.RawMessage `json:"value"`
}

// SubmitApplicationFeedbackInput replaces feedback for one interviewer and event.
type SubmitApplicationFeedbackInput struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// FormDefinitionID is the interview's assigned feedback form UUID.
	FormDefinitionID string `json:"formDefinitionId"`
	// UserID is the interviewer UUID credited with the feedback.
	UserID string `json:"userId"`
	// InterviewEventID is the event UUID; its presence makes repeat submissions replace existing feedback.
	InterviewEventID string `json:"interviewEventId"`
	// FieldSubmissions are answers keyed by form field paths.
	FieldSubmissions []FeedbackFieldSubmission `json:"fieldSubmissions"`
}

// FeedbackSubmissionReceipt identifies the submitted form without exposing answers.
type FeedbackSubmissionReceipt struct {
	// SubmissionID is the submitted form instance UUID.
	SubmissionID string `json:"submissionId"`
}

// SubmitApplicationFeedbackOperation submits or replaces an interviewer's feedback.
type SubmitApplicationFeedbackOperation struct{ client *Client }

// SubmitApplicationFeedback binds the Mutation to this client.
func (client *Client) SubmitApplicationFeedback() SubmitApplicationFeedbackOperation {
	return SubmitApplicationFeedbackOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (SubmitApplicationFeedbackOperation) Definition() sdkgo.MutationDefinition {
	return SubmitApplicationFeedbackDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (SubmitApplicationFeedbackOperation) IdempotencyKey(callID sdkgo.CallID, _ SubmitApplicationFeedbackInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces one user's existing event feedback with the supplied form values.
func (operation SubmitApplicationFeedbackOperation) Invoke(call sdkgo.Call, input SubmitApplicationFeedbackInput) sdkgo.MutationAttempt[FeedbackSubmissionReceipt] {
	if !validCandidateID(input.ApplicationID) || !validCandidateID(input.FormDefinitionID) || !validCandidateID(input.UserID) || !validCandidateID(input.InterviewEventID) || len(input.FieldSubmissions) == 0 || len(input.FieldSubmissions) > 100 {
		return applicationSafeBranch[FeedbackSubmissionReceipt]("submitApplicationFeedback", SubmitApplicationFeedbackBranchDefect, sdkgo.FailureValidation, "feedback identifiers or answer count are invalid")
	}
	for _, field := range input.FieldSubmissions {
		if strings.TrimSpace(field.Path) == "" || len(field.Path) > 256 || len(field.Value) > 8192 || !json.Valid(field.Value) {
			return applicationSafeBranch[FeedbackSubmissionReceipt]("submitApplicationFeedback", SubmitApplicationFeedbackBranchDefect, sdkgo.FailureValidation, "feedback field path or value is invalid")
		}
	}
	request := struct {
		ApplicationID    string `json:"applicationId"`
		FormDefinitionID string `json:"formDefinitionId"`
		UserID           string `json:"userId"`
		InterviewEventID string `json:"interviewEventId"`
		FeedbackForm     struct {
			FieldSubmissions []FeedbackFieldSubmission `json:"fieldSubmissions"`
		} `json:"feedbackForm"`
	}{ApplicationID: input.ApplicationID, FormDefinitionID: input.FormDefinitionID, UserID: input.UserID, InterviewEventID: input.InterviewEventID}
	request.FeedbackForm.FieldSubmissions = input.FieldSubmissions
	return executeAshbySafeWrite(operation.client, call, "submitApplicationFeedback", "/applicationFeedback.submit", request, SubmitApplicationFeedbackBranchSubmitted, SubmitApplicationFeedbackBranchProviderRejected, SubmitApplicationFeedbackBranchInvalidResponse, SubmitApplicationFeedbackBranchDefect, decodeFeedbackSubmissionReceipt, func(receipt FeedbackSubmissionReceipt) bool {
		return validCandidateID(receipt.SubmissionID)
	})
}

func decodeFeedbackRequestReceipt(body []byte) (FeedbackRequestReceipt, string, error) {
	var wire struct {
		Success   *bool                  `json:"success"`
		Results   FeedbackRequestReceipt `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return FeedbackRequestReceipt{}, "", errors.New("invalid feedback request response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return FeedbackRequestReceipt{}, "", errors.New("missing feedback rejection code")
		}
		return FeedbackRequestReceipt{}, wire.ErrorInfo.Code, nil
	}
	if !validCandidateID(wire.Results.ApplicationID) || !validCandidateID(wire.Results.InterviewEventID) || !validCandidateID(wire.Results.InterviewerUserID) {
		return FeedbackRequestReceipt{}, "", errors.New("incomplete feedback request")
	}
	return wire.Results, "", nil
}

func decodeFeedbackSubmissionReceipt(body []byte) (FeedbackSubmissionReceipt, string, error) {
	var wire struct {
		Success *bool `json:"success"`
		Results struct {
			SubmittedFormInstance struct {
				ID string `json:"id"`
			} `json:"submittedFormInstance"`
		} `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return FeedbackSubmissionReceipt{}, "", errors.New("invalid feedback submission response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return FeedbackSubmissionReceipt{}, "", errors.New("missing feedback rejection code")
		}
		return FeedbackSubmissionReceipt{}, wire.ErrorInfo.Code, nil
	}
	if !validCandidateID(wire.Results.SubmittedFormInstance.ID) {
		return FeedbackSubmissionReceipt{}, "", errors.New("missing feedback submission ID")
	}
	return FeedbackSubmissionReceipt{SubmissionID: wire.Results.SubmittedFormInstance.ID}, "", nil
}

func feedbackRequestBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[FeedbackRequestReceipt] {
	return applicationSafeBranch[FeedbackRequestReceipt]("requestApplicationFeedback", branch, kind, message)
}
