// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// InterviewScheduleInterviewer identifies an Ashby user by email.
type InterviewScheduleInterviewer struct {
	// Email is the interviewer's Ashby account email address.
	Email string `json:"email"`
	// FeedbackRequired indicates whether this interviewer must submit feedback.
	FeedbackRequired *bool `json:"feedbackRequired,omitempty"`
}

// InterviewScheduleEvent supplies a time range and interviewers.
type InterviewScheduleEvent struct {
	// StartTime is an RFC3339 timestamp.
	StartTime string `json:"startTime"`
	// EndTime is an RFC3339 timestamp after StartTime.
	EndTime string `json:"endTime"`
	// Interviewers are Ashby users who receive this interview.
	Interviewers []InterviewScheduleInterviewer `json:"interviewers"`
	// InterviewID selects an interview definition; blank uses Ashby's default.
	InterviewID string `json:"interviewId,omitempty"`
}

// CreateInterviewScheduleInput creates a schedule for one application.
type CreateInterviewScheduleInput struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewEvents contains one to ten scheduled events.
	InterviewEvents []InterviewScheduleEvent `json:"interviewEvents"`
}

// CreateInterviewScheduleOperation schedules interviews with a bounded send count.
type CreateInterviewScheduleOperation struct{ client *Client }

// CreateInterviewSchedule binds the Mutation to this client.
func (client *Client) CreateInterviewSchedule() CreateInterviewScheduleOperation {
	return CreateInterviewScheduleOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreateInterviewScheduleOperation) Definition() sdkgo.MutationDefinition {
	return CreateInterviewScheduleDefinition
}

// IdempotencyKey records Dex call identity; Ashby does not deduplicate by this key.
func (CreateInterviewScheduleOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInterviewScheduleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a schedule, allowing at most three possible sends.
func (operation CreateInterviewScheduleOperation) Invoke(call sdkgo.Call, input CreateInterviewScheduleInput) sdkgo.MutationAttempt[InterviewScheduleSummary] {
	if !validCandidateID(input.ApplicationID) || len(input.InterviewEvents) == 0 || len(input.InterviewEvents) > 10 {
		return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchDefect, sdkgo.FailureValidation, "application ID or interview event count is invalid")
	}
	for _, event := range input.InterviewEvents {
		start, startErr := time.Parse(time.RFC3339Nano, event.StartTime)
		end, endErr := time.Parse(time.RFC3339Nano, event.EndTime)
		if startErr != nil || endErr != nil || !end.After(start) || len(event.Interviewers) == 0 || len(event.Interviewers) > 20 || event.InterviewID != "" && !validCandidateID(event.InterviewID) {
			return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchDefect, sdkgo.FailureValidation, "interview event time, ID, or interviewer count is invalid")
		}
		for _, interviewer := range event.Interviewers {
			address, err := mail.ParseAddress(interviewer.Email)
			if err != nil || address.Address != interviewer.Email || len(interviewer.Email) > 254 {
				return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchDefect, sdkgo.FailureValidation, "interviewer email is invalid")
			}
		}
	}
	fence, err := singledispatch.NewFence(call, CreateInterviewScheduleDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchDefect, sdkgo.FailureLocalDefect, "schedule dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return jobFailure("createInterviewSchedule", kind, message)
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(InterviewScheduleSummary{}, failure(sdkgo.FailureTransport, "schedule outcome could not be confirmed within the send cap"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postOperationWithContext(call, requestContext, "/interviewSchedule.create", input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[InterviewScheduleSummary](failure(sdkgo.FailureAvailability, "schedule request was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(InterviewScheduleSummary{}, failure(sdkgo.FailureAuthentication, "credential failed after an earlier possible send"), sdkgo.Receipt{})
		}
		return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[InterviewScheduleSummary](failure(sdkgo.FailureAvailability, "schedule resend checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[InterviewScheduleSummary](failure(kind, "Ashby temporarily rejected interview scheduling"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[InterviewScheduleSummary](failure(sdkgo.FailureTransport, "interview scheduling outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(InterviewScheduleSummary{}, failure(sdkgo.FailureProviderRejection, "an earlier schedule may have succeeded"), sdkgo.Receipt{})
		}
		return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected scheduling with HTTP "+strconv.Itoa(response.status))
	}
	schedule, code, err := decodeScheduleSummary(response.body)
	if err != nil || code == "" && schedule.ApplicationID != input.ApplicationID {
		return sdkgo.NewMutationRetry[InterviewScheduleSummary](failure(sdkgo.FailureProtocol, "Ashby accepted scheduling but returned an unusable response"), 0)
	}
	if code != "" {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(InterviewScheduleSummary{}, failure(sdkgo.FailureProviderRejection, "an earlier schedule may have succeeded"), sdkgo.Receipt{})
		}
		return scheduleBranch("createInterviewSchedule", CreateInterviewScheduleBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected scheduling")
	}
	return sdkgo.NewMutationBranch(CreateInterviewScheduleBranchCreated, schedule, nil, sdkgo.Receipt{})
}

// CancelInterviewScheduleInput identifies a schedule to cancel.
type CancelInterviewScheduleInput struct {
	// InterviewScheduleID is the schedule UUID.
	InterviewScheduleID string `json:"interviewScheduleId"`
}

// CancelInterviewScheduleOperation cancels a complete schedule.
type CancelInterviewScheduleOperation struct{ client *Client }

// CancelInterviewSchedule binds the Mutation to this client.
func (client *Client) CancelInterviewSchedule() CancelInterviewScheduleOperation {
	return CancelInterviewScheduleOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CancelInterviewScheduleOperation) Definition() sdkgo.MutationDefinition {
	return CancelInterviewScheduleDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (CancelInterviewScheduleOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelInterviewScheduleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels the schedule without allowing rescheduling.
func (operation CancelInterviewScheduleOperation) Invoke(call sdkgo.Call, input CancelInterviewScheduleInput) sdkgo.MutationAttempt[InterviewScheduleSummary] {
	if !validCandidateID(input.InterviewScheduleID) {
		return scheduleBranch("cancelInterviewSchedule", CancelInterviewScheduleBranchDefect, sdkgo.FailureValidation, "schedule ID is invalid")
	}
	request := struct {
		ID              string `json:"id"`
		AllowReschedule bool   `json:"allowReschedule"`
	}{ID: input.InterviewScheduleID, AllowReschedule: false}
	return executeAshbySafeWrite(operation.client, call, "cancelInterviewSchedule", "/interviewSchedule.cancel", request, CancelInterviewScheduleBranchCancelled, CancelInterviewScheduleBranchProviderRejected, CancelInterviewScheduleBranchInvalidResponse, CancelInterviewScheduleBranchDefect, decodeScheduleSummary, func(output InterviewScheduleSummary) bool {
		return output.ID == input.InterviewScheduleID && output.Status == "Cancelled"
	})
}

// CancelInterviewEventInput identifies one event on an API-created schedule.
type CancelInterviewEventInput struct {
	// InterviewScheduleID is the schedule UUID.
	InterviewScheduleID string `json:"interviewScheduleId"`
	// InterviewEventID is the event UUID to cancel.
	InterviewEventID string `json:"interviewEventId"`
}

// CancelInterviewEventOperation cancels one event while preserving submitted feedback.
type CancelInterviewEventOperation struct{ client *Client }

// CancelInterviewEvent binds the Mutation to this client.
func (client *Client) CancelInterviewEvent() CancelInterviewEventOperation {
	return CancelInterviewEventOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CancelInterviewEventOperation) Definition() sdkgo.MutationDefinition {
	return CancelInterviewEventDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (CancelInterviewEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelInterviewEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels an event without deleting submitted feedback.
func (operation CancelInterviewEventOperation) Invoke(call sdkgo.Call, input CancelInterviewEventInput) sdkgo.MutationAttempt[InterviewScheduleSummary] {
	if !validCandidateID(input.InterviewScheduleID) || !validCandidateID(input.InterviewEventID) {
		return scheduleBranch("cancelInterviewEvent", CancelInterviewEventBranchDefect, sdkgo.FailureValidation, "schedule or event ID is invalid")
	}
	request := struct {
		InterviewScheduleID      string `json:"interviewScheduleId"`
		InterviewEventIDToCancel string `json:"interviewEventIdToCancel"`
		AllowFeedbackDeletion    bool   `json:"allowFeedbackDeletion"`
	}{InterviewScheduleID: input.InterviewScheduleID, InterviewEventIDToCancel: input.InterviewEventID, AllowFeedbackDeletion: false}
	decode := func(body []byte) (InterviewScheduleSummary, string, error) {
		return decodeScheduleWithoutEvent(body, input.InterviewEventID)
	}
	return executeAshbySafeWrite(operation.client, call, "cancelInterviewEvent", "/interviewSchedule.update", request, CancelInterviewEventBranchCancelled, CancelInterviewEventBranchProviderRejected, CancelInterviewEventBranchInvalidResponse, CancelInterviewEventBranchDefect, decode, func(output InterviewScheduleSummary) bool {
		return output.ID == input.InterviewScheduleID
	})
}

func decodeScheduleWithoutEvent(body []byte, cancelledEventID string) (InterviewScheduleSummary, string, error) {
	schedule, code, err := decodeScheduleSummary(body)
	if err != nil || code != "" {
		return schedule, code, err
	}
	var wire struct {
		Results struct {
			InterviewEvents []struct {
				ID string `json:"id"`
			} `json:"interviewEvents"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Results.InterviewEvents == nil {
		return InterviewScheduleSummary{}, "", errors.New("interview events missing from updated schedule")
	}
	for _, event := range wire.Results.InterviewEvents {
		if event.ID == cancelledEventID {
			return InterviewScheduleSummary{}, "", errors.New("cancelled interview event is still present")
		}
	}
	return schedule, "", nil
}

func scheduleBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[InterviewScheduleSummary] {
	return applicationSafeBranch[InterviewScheduleSummary](operationName, branch, kind, message)
}
