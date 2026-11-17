// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

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

const (
	createMeetingOperationID = "createMeeting"
	// scheduledMeetingType is Zoom's type for a one-time meeting with a fixed start.
	scheduledMeetingType = 2
)

// CreateMeetingInput describes one scheduled meeting for the authorized user.
type CreateMeetingInput struct {
	// Topic is the meeting topic, 1 to 200 characters.
	Topic string `json:"topic"`
	// StartTime is the start instant in RFC 3339 with an explicit Z or ±hh:mm
	// offset, such as 2026-02-24T09:00:00-08:00. It must be in the future and a
	// whole second; a value without an offset is rejected rather than guessed.
	StartTime string `json:"startTime"`
	// TimeZone is the IANA time zone Zoom uses to display the meeting, such as
	// America/Los_Angeles. It never moves the instant StartTime names.
	TimeZone string `json:"timeZone"`
	// DurationMinutes is the scheduled duration, from 1 to 1440 minutes.
	DurationMinutes int `json:"durationMinutes"`
	// Agenda is the optional meeting description, at most 2000 characters.
	Agenda string `json:"agenda,omitempty"`
	// Settings optionally overrides the user's default meeting settings.
	Settings *MeetingSettingsInput `json:"settings,omitempty"`
}

// CreateMeetingOperation implements the createMeeting Mutation.
type CreateMeetingOperation struct{ client *Client }

type zoomCreateMeetingRequest struct {
	Topic     string                       `json:"topic"`
	Type      int                          `json:"type"`
	StartTime string                       `json:"start_time"`
	Duration  int                          `json:"duration"`
	TimeZone  string                       `json:"timezone"`
	Agenda    string                       `json:"agenda,omitempty"`
	Settings  *zoomMeetingSettingsResource `json:"settings,omitempty"`
}

type createMeetingSendAttempt struct {
	client       *Client
	call         sdkgo.Call
	credentials  Credentials
	request      zoomCreateMeetingRequest
	response     providerResponse
	isDispatched bool
}

func (attempt *createMeetingSendAttempt) send(requestContext context.Context) error {
	var err error
	attempt.response, attempt.isDispatched, err = attempt.client.exchangeWithContext(
		attempt.call, requestContext, &attempt.credentials,
		providerRequest{method: http.MethodPost, path: currentUserPath + "/meetings", payload: attempt.request}, true,
	)
	return err
}

// Definition returns the immutable connector operation definition.
func (CreateMeetingOperation) Definition() sdkgo.MutationDefinition { return CreateMeetingDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Zoom accepts no idempotency key, so it is never sent and cannot deduplicate a repeated create.
func (CreateMeetingOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMeetingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one meeting behind a durable dispatch fence. Zoom accepts no idempotency key
// and has no Call ID lookup, so an unknown outcome resends while possible sends stay below
// call.MaxSends() and selects uncertain at the cap. A saved meeting ID is read back instead of
// resent, and a definite refusal is never resent.
func (operation CreateMeetingOperation) Invoke(call sdkgo.Call, input CreateMeetingInput) sdkgo.MutationAttempt[Meeting] {
	client := operation.client
	request, requested, err := client.buildCreateMeetingRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateMeetingDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureLocalDefect, "Zoom create dispatch fence could not be initialized"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "the Zoom create checkpoint is unreadable; no meeting was created in this attempt"), 0)
	case singledispatch.ReadBackRequired:
		if decision.Marker.CallID != call.ID || decision.Marker.ProviderID == "" {
			return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "an earlier Zoom create may have succeeded without a confirmed meeting ID, and the send cap is reached"), client.receipt(call, "", ""))
		}
		return client.readBackCreatedMeeting(call, requested, decision.Marker.ProviderID)
	case singledispatch.ReadBackThenResendAllowed:
		// A saved meeting ID proves Zoom created the meeting, so it is read back and never resent.
		if decision.Marker.ProviderID != "" {
			return client.readBackCreatedMeeting(call, requested, decision.Marker.ProviderID)
		}
	}
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have created the meeting, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	if !requested.StartTime.After(client.now()) {
		if hasPossibleEarlierSend {
			// A passed start time forbids the resend, so only the earlier possible create remains.
			return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureValidation, "an earlier attempt may have created the Zoom meeting, and its start time has passed, so it cannot be resent"), client.receipt(call, "", ""))
		}
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom create not-sent checkpoint was unavailable"), 0)
		}
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureValidation, "startTime is not in the future, and Zoom would replace it with the current time"), sdkgo.Receipt{})
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, createMeetingOperationID)
	if failure != nil {
		if hasPossibleEarlierSend {
			// notSent would hide the earlier possible send, so nothing is recorded.
			return sdkgo.NewMutationRetry[Meeting](*failure, 0)
		}
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom credentials and the not-sent checkpoint were unavailable"), 0)
		}
		if isRetryable {
			return sdkgo.NewMutationRetry[Meeting](*failure, 0)
		}
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failure, sdkgo.Receipt{})
	}
	attempt := &createMeetingSendAttempt{client: client, call: call, credentials: credentials, request: request}
	err = fence.Dispatch(attempt.send)
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom create was not sent before the attempt deadline or checkpoint"), 0)
		}
		if errors.Is(err, errRequestNotBuilt) {
			// The request was never sent; a lost canResend checkpoint leaves the claim, which still counts.
			_ = fence.RecordCanResend()
			if hasPossibleEarlierSend {
				return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureLocalDefect, err.Error()+"; no meeting was created in this attempt"), 0)
			}
			return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		if !attempt.isDispatched {
			if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom was not reached but the resend checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom could not be reached, so no meeting was created"), 0)
		}
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom meeting outcome is unknown"), 0, "")
	}
	response := attempt.response
	if isSuccessStatus(response.statusCode) {
		return client.classifyCreatedMeeting(call, response, requested, fence, canResendUnknownOutcome)
	}
	classification := client.classifyErrorStatus(createMeetingOperationID, response)
	switch {
	case response.statusCode == http.StatusUnauthorized:
		if _, isRejectionRefreshing := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !isRejectionRefreshing {
			failure := newFailure(createMeetingOperationID, sdkgo.FailureAuthentication, "Zoom rejected the connection credential; reconnect the connection")
			return client.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, failure, classification.errorCode)
		}
		var refreshContext context.Context = call.Context
		if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
			boundedContext, cancel := context.WithDeadline(call.Context, deadline.Add(-time.Second))
			defer cancel()
			refreshContext = boundedContext
		}
		refreshed, refreshErr := sdkgo.ResolveCredentialAfterRejection(refreshContext, client.credentials, call, client.refreshDriver)
		if refreshErr == nil && validateResolvedCredentials(refreshed) == nil && refreshed.AccessToken.Reveal() != credentials.AccessToken.Reveal() {
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom rejected the credential but the resend checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[Meeting](classification.failure, 0)
		}
		failure := newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom credential refresh is temporarily unavailable")
		if sdkgo.IsReauthorizationRequired(refreshErr) || refreshErr == nil {
			failure = newFailure(createMeetingOperationID, sdkgo.FailureAuthentication, "Zoom connection requires reauthorization; reconnect the connection")
		}
		return client.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, failure, classification.errorCode)
	case response.statusCode == http.StatusTooManyRequests && classification.outcome == statusOutcomeRejected:
		// The documented daily limit refuses the create, so nothing was created by this send.
		return client.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, classification.failure, classification.errorCode)
	case response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom rejected the create but the resend checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[Meeting](classification.failure, classification.retryAfter)
	case isDefiniteRefusalStatus(response.statusCode) && readCreatedMeetingID(response.body) == 0:
		return client.rejectOrSelectUncertain(call, fence, hasPossibleEarlierSend, requested, classification.failure, classification.errorCode)
	default:
		// 408, 409, 5xx, redirects, and other 4xx responses do not prove whether Zoom applied the create.
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, classification.failure, classification.retryAfter, classification.errorCode)
	}
}

func (client *Client) classifyCreatedMeeting(call sdkgo.Call, response providerResponse, requested Meeting, fence *singledispatch.Fence, canResendUnknownOutcome bool) sdkgo.MutationAttempt[Meeting] {
	switch {
	case response.isBodyTooLarge:
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, newFailure(createMeetingOperationID, sdkgo.FailureResponseTooLarge, "Zoom meeting response exceeds the configured size limit"), 0, "")
	case response.hasBodyReadFailed:
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom meeting response was interrupted"), 0, "")
	case response.isCredentialReflected:
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom meeting response contained the connection credential"), 0, "")
	}
	meeting, err := decodeMeeting(response.body)
	if err != nil {
		if readCreatedMeetingID(response.body) > 0 {
			// Zoom returned a created meeting ID, so a resend could duplicate the meeting.
			return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom accepted the request but returned an unusable meeting"), client.receipt(call, "", ""))
		}
		return client.resendOrSelectUncertain(call, canResendUnknownOutcome, requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom meeting response is malformed"), 0, "")
	}
	// A lost accepted-ID checkpoint leaves the claim, which still counts this send.
	_ = fence.RecordProviderID(strconv.FormatInt(meeting.ID, 10))
	return sdkgo.NewMutationBranch(CreateMeetingBranchCreated, meeting, nil, client.receipt(call, strconv.FormatInt(meeting.ID, 10), ""))
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove Zoom refused the request.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// readCreatedMeetingID returns a positive top-level meeting id from a response body, or zero.
func readCreatedMeetingID(body []byte) int64 {
	var resource struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(body, &resource) != nil || resource.ID < 1 {
		return 0
	}
	return resource.ID
}

// resendOrSelectUncertain retries a possibly sent create while the send cap allows another send.
func (client *Client) resendOrSelectUncertain(call sdkgo.Call, canResendUnknownOutcome bool, requested Meeting, failure sdkgo.Failure, retryAfter time.Duration, errorCode string) sdkgo.MutationAttempt[Meeting] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[Meeting](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(requested, failure, client.receipt(call, "", errorCode))
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have created the meeting.
func (client *Client) rejectOrSelectUncertain(call sdkgo.Call, fence *singledispatch.Fence, hasPossibleEarlierSend bool, requested Meeting, failure sdkgo.Failure, errorCode string) sdkgo.MutationAttempt[Meeting] {
	if hasPossibleEarlierSend {
		return sdkgo.NewMutationUncertain(requested, failure, client.receipt(call, "", errorCode))
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return sdkgo.NewMutationBranch(CreateMeetingBranchProviderRejected, requested, &failure, client.receipt(call, "", errorCode))
}

func (client *Client) readBackCreatedMeeting(call sdkgo.Call, requested Meeting, providerID string) sdkgo.MutationAttempt[Meeting] {
	meetingID, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || meetingID < 1 {
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "the accepted Zoom meeting ID could not be read back"), client.receipt(call, providerID, ""))
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, createMeetingOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewMutationRetry[Meeting](*failure, 0)
		}
		return sdkgo.NewMutationUncertain(requested, *failure, client.receipt(call, providerID, ""))
	}
	var readBackContext context.Context = call.Context
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		if time.Until(deadline) <= time.Second {
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "Zoom meeting read-back has insufficient time"), 0)
		}
		boundedContext, cancel := context.WithDeadline(call.Context, deadline.Add(-time.Second))
		defer cancel()
		readBackContext = boundedContext
	}
	response, _, err := client.exchangeWithContext(call, readBackContext, &credentials, providerRequest{method: http.MethodGet, path: meetingPath(meetingID)}, false)
	if err != nil {
		return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom meeting read-back could not be reached"), 0)
	}
	classification := client.classifyReadResponse(createMeetingOperationID, response)
	if classification.outcome == statusOutcomeRetry {
		return sdkgo.NewMutationRetry[Meeting](classification.failure, classification.retryAfter)
	}
	if classification.outcome == statusOutcomeUsable {
		meeting, decodeErr := decodeMeeting(response.body)
		if decodeErr == nil && meeting.ID == meetingID {
			return sdkgo.NewMutationBranch(CreateMeetingBranchCreated, meeting, nil, client.receipt(call, providerID, ""))
		}
	}
	return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom could not confirm the accepted meeting"), client.receipt(call, providerID, ""))
}

// buildCreateMeetingRequest validates input and returns the wire request and the requested meeting echo.
func (client *Client) buildCreateMeetingRequest(input CreateMeetingInput) (zoomCreateMeetingRequest, Meeting, error) {
	if err := validateTopic(input.Topic); err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	startTime, err := parseMeetingStartInstant(input.StartTime, input.TimeZone)
	if err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	if err := validateDurationMinutes(input.DurationMinutes); err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	if err := validateAgenda(input.Agenda); err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	if err := input.Settings.validate(); err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	utcStart := startTime.UTC()
	request := zoomCreateMeetingRequest{
		Topic: input.Topic, Type: scheduledMeetingType, StartTime: utcStart.Format(zoomUTCLayout),
		Duration: input.DurationMinutes, TimeZone: input.TimeZone, Agenda: input.Agenda,
		Settings: input.Settings.toZoomSettings(),
	}
	requested := Meeting{
		Topic: input.Topic, Type: scheduledMeetingType, StartTime: &utcStart, DurationMinutes: input.DurationMinutes,
		TimeZone: input.TimeZone, Agenda: input.Agenda,
	}
	return request, requested, nil
}
