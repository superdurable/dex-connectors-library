// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// createDispatchCheckpoint is the heartbeat value recorded before the create request leaves the Worker.
type createDispatchCheckpoint struct {
	IsDispatched bool `json:"isDispatched"`
}

// Definition returns the immutable connector operation definition.
func (CreateMeetingOperation) Definition() sdkgo.MutationDefinition { return CreateMeetingDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Zoom accepts no idempotency key, so it is never sent and cannot deduplicate a repeated create.
func (CreateMeetingOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMeetingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one meeting and never sends a create request that may already have reached Zoom.
// Only a 429 or a request that provably never left the Worker is retried.
func (operation CreateMeetingOperation) Invoke(call sdkgo.Call, input CreateMeetingInput) sdkgo.MutationAttempt[Meeting] {
	client := operation.client
	request, requested, err := client.buildCreateMeetingRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if hasEarlierCreateDispatch(call) {
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "an earlier attempt sent the Zoom create request and its outcome is unknown"), client.receipt(call, "", ""))
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, createMeetingOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewMutationRetry[Meeting](*failure, 0)
		}
		return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failure, sdkgo.Receipt{})
	}
	if err := call.Context.RecordHeartbeat(createDispatchCheckpoint{IsDispatched: true}); err != nil {
		return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureAvailability, "the create checkpoint could not be recorded, so nothing was sent to Zoom"), 0)
	}
	response, isDispatched, err := client.exchange(call, &credentials, providerRequest{method: http.MethodPost, path: currentUserPath + "/meetings", payload: request})
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			clearCreateDispatch(call)
			return sdkgo.NewMutationBranch(CreateMeetingBranchDefect, Meeting{}, failurePointer(createMeetingOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		if !isDispatched {
			clearCreateDispatch(call)
			return sdkgo.NewMutationRetry[Meeting](newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom could not be reached, so no meeting was created"), 0)
		}
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom meeting outcome is unknown"), client.receipt(call, "", ""))
	}
	if isSuccessStatus(response.statusCode) {
		return client.classifyCreatedMeeting(call, response, requested)
	}
	classification := client.classifyErrorStatus(createMeetingOperationID, response)
	switch {
	case response.statusCode == http.StatusTooManyRequests && classification.outcome == statusOutcomeRetry:
		clearCreateDispatch(call)
		return sdkgo.NewMutationRetry[Meeting](classification.failure, classification.retryAfter)
	case response.statusCode >= 400 && response.statusCode < 500 && response.statusCode != http.StatusRequestTimeout:
		return sdkgo.NewMutationBranch(CreateMeetingBranchProviderRejected, requested, &classification.failure, client.receipt(call, "", classification.errorCode))
	default:
		// A 3xx, 408, or 5xx can follow a create Zoom already applied.
		return sdkgo.NewMutationUncertain(requested, classification.failure, client.receipt(call, "", classification.errorCode))
	}
}

func (client *Client) classifyCreatedMeeting(call sdkgo.Call, response providerResponse, requested Meeting) sdkgo.MutationAttempt[Meeting] {
	switch {
	case response.isBodyTooLarge:
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureResponseTooLarge, "Zoom accepted the request but its response exceeds the configured size limit"), client.receipt(call, "", ""))
	case response.hasBodyReadFailed:
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureTransport, "Zoom accepted the request but its response was interrupted"), client.receipt(call, "", ""))
	case response.isCredentialReflected:
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom accepted the request but its response contained the connection credential"), client.receipt(call, "", ""))
	}
	meeting, err := decodeMeeting(response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, newFailure(createMeetingOperationID, sdkgo.FailureProtocol, "Zoom accepted the request but returned an unusable meeting"), client.receipt(call, "", ""))
	}
	return sdkgo.NewMutationBranch(CreateMeetingBranchCreated, meeting, nil, client.receipt(call, strconv.FormatInt(meeting.ID, 10), ""))
}

// buildCreateMeetingRequest validates input and returns the wire request and the requested meeting echo.
func (client *Client) buildCreateMeetingRequest(input CreateMeetingInput) (zoomCreateMeetingRequest, Meeting, error) {
	if err := validateTopic(input.Topic); err != nil {
		return zoomCreateMeetingRequest{}, Meeting{}, err
	}
	startTime, err := parseMeetingStart(input.StartTime, input.TimeZone, client.now())
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

// hasEarlierCreateDispatch reports an earlier attempt's checkpoint; an unreadable one counts, avoiding a duplicate.
func hasEarlierCreateDispatch(call sdkgo.Call) bool {
	var checkpoint createDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	return err != nil || (isFound && checkpoint.IsDispatched)
}

// clearCreateDispatch removes the checkpoint after Zoom provably created nothing.
func clearCreateDispatch(call sdkgo.Call) {
	// A lost clear leaves the checkpoint set, so the next attempt reports uncertain instead of creating twice.
	_ = call.Context.RecordHeartbeat(nil)
}
