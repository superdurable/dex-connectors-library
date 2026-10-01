// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateMeetingOperationID = "updateMeeting"

// UpdateMeetingInput patches one meeting. Every field other than MeetingID is
// optional, at least one must be set, and each set field replaces Zoom's value,
// so repeating the same patch leaves the same meeting.
type UpdateMeetingInput struct {
	// MeetingID is Zoom's numeric meeting ID.
	MeetingID int64 `json:"meetingId"`
	// Topic replaces the topic, 1 to 200 characters; blank leaves it.
	Topic string `json:"topic,omitempty"`
	// Agenda replaces the agenda, at most 2000 characters; nil leaves it and an empty string clears it.
	Agenda *string `json:"agenda,omitempty"`
	// StartTime moves the meeting to a future RFC 3339 instant with an explicit
	// offset; blank leaves it. Zoom silently ignores a past start, so the connector rejects one.
	StartTime string `json:"startTime,omitempty"`
	// TimeZone is the IANA time zone Zoom uses to display the meeting. It is
	// required with StartTime and must be blank without it.
	TimeZone string `json:"timeZone,omitempty"`
	// DurationMinutes replaces the duration, from 1 to 1440 minutes; zero leaves it.
	DurationMinutes int `json:"durationMinutes,omitempty"`
	// Settings changes the listed settings and leaves the others.
	Settings *MeetingSettingsInput `json:"settings,omitempty"`
}

// UpdatedMeeting identifies the meeting Zoom patched. Zoom returns no meeting
// body, so read it with getMeeting to observe the stored values.
type UpdatedMeeting struct {
	// MeetingID is Zoom's numeric meeting ID.
	MeetingID int64 `json:"meetingId"`
}

// UpdateMeetingOperation implements the updateMeeting Mutation.
type UpdateMeetingOperation struct{ client *Client }

type zoomUpdateMeetingRequest struct {
	Topic     string                       `json:"topic,omitempty"`
	Agenda    *string                      `json:"agenda,omitempty"`
	StartTime string                       `json:"start_time,omitempty"`
	TimeZone  string                       `json:"timezone,omitempty"`
	Duration  int                          `json:"duration,omitempty"`
	Settings  *zoomMeetingSettingsResource `json:"settings,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateMeetingOperation) Definition() sdkgo.MutationDefinition { return UpdateMeetingDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// The patch sets absolute values, so a repeated request needs no provider key.
func (UpdateMeetingOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateMeetingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one patch. Transport failures, 408, 429, and 5xx responses are
// retried, because sending the same absolute values again is safe.
func (operation UpdateMeetingOperation) Invoke(call sdkgo.Call, input UpdateMeetingInput) sdkgo.MutationAttempt[UpdatedMeeting] {
	client := operation.client
	request, err := client.buildUpdateMeetingRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateMeetingBranchDefect, UpdatedMeeting{}, failurePointer(updateMeetingOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, updateMeetingOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewMutationRetry[UpdatedMeeting](*failure, 0)
		}
		return sdkgo.NewMutationBranch(UpdateMeetingBranchDefect, UpdatedMeeting{}, failure, sdkgo.Receipt{})
	}
	updated := UpdatedMeeting{MeetingID: input.MeetingID}
	meetingID := strconv.FormatInt(input.MeetingID, 10)
	response, _, err := client.exchange(call, &credentials, providerRequest{method: http.MethodPatch, path: meetingPath(input.MeetingID), payload: request})
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			return sdkgo.NewMutationBranch(UpdateMeetingBranchDefect, UpdatedMeeting{}, failurePointer(updateMeetingOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[UpdatedMeeting](newFailure(updateMeetingOperationID, sdkgo.FailureTransport, "Zoom could not be reached"), 0)
	}
	if isSuccessStatus(response.statusCode) {
		return sdkgo.NewMutationBranch(UpdateMeetingBranchUpdated, updated, nil, client.receipt(call, meetingID, ""))
	}
	classification := client.classifyErrorStatus(updateMeetingOperationID, response)
	switch classification.outcome {
	case statusOutcomeRetry:
		return sdkgo.NewMutationRetry[UpdatedMeeting](classification.failure, classification.retryAfter)
	case statusOutcomeNotFound:
		return sdkgo.NewMutationBranch(UpdateMeetingBranchNotFound, updated, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	default:
		return sdkgo.NewMutationBranch(UpdateMeetingBranchProviderRejected, updated, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	}
}

func (client *Client) buildUpdateMeetingRequest(input UpdateMeetingInput) (zoomUpdateMeetingRequest, error) {
	if err := validateMeetingID(input.MeetingID); err != nil {
		return zoomUpdateMeetingRequest{}, err
	}
	request := zoomUpdateMeetingRequest{Agenda: input.Agenda, Duration: input.DurationMinutes, Settings: input.Settings.toZoomSettings()}
	if input.Topic != "" {
		if err := validateTopic(input.Topic); err != nil {
			return zoomUpdateMeetingRequest{}, err
		}
		request.Topic = input.Topic
	}
	if input.Agenda != nil {
		if err := validateAgenda(*input.Agenda); err != nil {
			return zoomUpdateMeetingRequest{}, err
		}
	}
	switch {
	case input.StartTime != "":
		startTime, err := parseMeetingStart(input.StartTime, input.TimeZone, client.now())
		if err != nil {
			return zoomUpdateMeetingRequest{}, err
		}
		request.StartTime = startTime.UTC().Format(zoomUTCLayout)
		request.TimeZone = input.TimeZone
	case input.TimeZone != "":
		return zoomUpdateMeetingRequest{}, errors.New("timeZone requires startTime, so the instant and its display zone change together")
	}
	if input.DurationMinutes != 0 {
		if err := validateDurationMinutes(input.DurationMinutes); err != nil {
			return zoomUpdateMeetingRequest{}, err
		}
	}
	if err := input.Settings.validate(); err != nil {
		return zoomUpdateMeetingRequest{}, err
	}
	if request == (zoomUpdateMeetingRequest{}) {
		return zoomUpdateMeetingRequest{}, errors.New("updateMeeting needs at least one change")
	}
	return request, nil
}
