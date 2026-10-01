// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getMeetingOperationID = "getMeeting"

// GetMeetingInput identifies one meeting.
type GetMeetingInput struct {
	// MeetingID is Zoom's numeric meeting ID, such as the ID createMeeting returned.
	MeetingID int64 `json:"meetingId"`
}

// GetMeetingOperation implements the getMeeting Query.
type GetMeetingOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetMeetingOperation) Definition() sdkgo.QueryDefinition { return GetMeetingDefinition }

// Invoke reads one meeting. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetMeetingOperation) Invoke(call sdkgo.Call, input GetMeetingInput) sdkgo.QueryAttempt[Meeting] {
	client := operation.client
	if err := validateMeetingID(input.MeetingID); err != nil {
		return sdkgo.NewQueryBranch(GetMeetingBranchDefect, Meeting{}, failurePointer(getMeetingOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, getMeetingOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewQueryRetry[Meeting](*failure, 0)
		}
		return sdkgo.NewQueryBranch(GetMeetingBranchDefect, Meeting{}, failure, sdkgo.Receipt{})
	}
	meetingID := strconv.FormatInt(input.MeetingID, 10)
	response, _, err := client.exchange(call, &credentials, providerRequest{method: http.MethodGet, path: meetingPath(input.MeetingID)})
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			return sdkgo.NewQueryBranch(GetMeetingBranchDefect, Meeting{}, failurePointer(getMeetingOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[Meeting](newFailure(getMeetingOperationID, sdkgo.FailureTransport, "Zoom could not be reached"), 0)
	}
	classification := client.classifyReadResponse(getMeetingOperationID, response)
	switch classification.outcome {
	case statusOutcomeUsable:
	case statusOutcomeRetry:
		return sdkgo.NewQueryRetry[Meeting](classification.failure, classification.retryAfter)
	case statusOutcomeNotFound:
		return sdkgo.NewQueryBranch(GetMeetingBranchNotFound, Meeting{ID: input.MeetingID}, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	case statusOutcomeInvalid:
		return sdkgo.NewQueryBranch(GetMeetingBranchInvalidResponse, Meeting{}, &classification.failure, client.receipt(call, meetingID, ""))
	default:
		return sdkgo.NewQueryBranch(GetMeetingBranchProviderRejected, Meeting{}, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	}
	meeting, err := decodeMeeting(response.body)
	if err != nil || meeting.ID != input.MeetingID {
		return sdkgo.NewQueryBranch(GetMeetingBranchInvalidResponse, Meeting{}, failurePointer(getMeetingOperationID, sdkgo.FailureProtocol, "Zoom returned an unusable meeting"), client.receipt(call, meetingID, ""))
	}
	return sdkgo.NewQueryBranch(GetMeetingBranchFound, meeting, nil, client.receipt(call, meetingID, ""))
}
