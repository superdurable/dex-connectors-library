// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const googleMeetConferenceSolutionType = "hangoutsMeet"

// CreateEventInput describes one event to create. The event is written under a stable
// client-supplied ID: EventID when set, otherwise one derived from the Step's Call ID.
// A retried Step therefore finds and returns its own event instead of creating another.
type CreateEventInput struct {
	// CalendarID is a writable calendar ID from the calendar picker, or primary for the authorized user's calendar.
	CalendarID string `json:"calendarId"`
	// EventID optionally fixes the event ID for business-level deduplication across Step executions.
	// It must be 5 to 1024 lowercase base32hex characters (a-v and 0-9); blank derives the ID from the Call ID.
	EventID string `json:"eventId,omitempty"`
	// Summary is the required event title.
	Summary string `json:"summary"`
	// Description is the optional event description.
	Description string `json:"description,omitempty"`
	// Start is the inclusive start; see EventDateTime and ValidateEventTimes.
	Start EventDateTime `json:"start"`
	// End is the exclusive end; see EventDateTime and ValidateEventTimes.
	End EventDateTime `json:"end"`
	// Attendees lists unique bare guest addresses; empty creates an event without guests.
	Attendees []EventAttendeeInput `json:"attendees,omitempty"`
	// RequestConference asks Google to attach a new Google Meet conference to the event.
	RequestConference bool `json:"requestConference,omitempty"`
	// SendUpdates is none, all, or externalOnly; blank sends no guest notifications.
	SendUpdates string `json:"sendUpdates,omitempty"`
}

// CreateEventOutput is the event that exists under the stable event ID.
type CreateEventOutput struct {
	// Event is the created or previously created event.
	Event Event `json:"event"`
	// WasAlreadyCreated reports that the ID already held this event, such as after a retried attempt.
	WasAlreadyCreated bool `json:"wasAlreadyCreated"`
}

// CreateEventOperation implements the createEvent connector Mutation.
type CreateEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// IdempotencyKey returns the event ID the operation writes: input.EventID when set, otherwise
// the Call ID without dashes. A UUID's hexadecimal digits are valid base32hex event ID characters.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, input CreateEventInput) sdkgo.IdempotencyKey {
	if input.EventID != "" {
		return sdkgo.IdempotencyKey(input.EventID)
	}
	return sdkgo.IdempotencyKey(strings.ReplaceAll(string(callID), "-", ""))
}

// Invoke reads the stable event ID, inserts the event only when the ID is unused, and reads it
// back after Google reports the ID as a duplicate. Because a repeated attempt finds its own event,
// transport failures and 5xx responses are retried rather than reported as uncertain.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[CreateEventOutput] {
	const operationID = "createEvent"
	eventID := string(call.IdempotencyKey)
	if err := validateCreateEventInput(input, eventID); err != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failure, sdkgo.Receipt{})
	}
	existing := operation.client.readEvent(call, &credentials, operationID, input.CalendarID, eventID, "")
	if existing.outcome != eventReadMissing {
		return operation.existingEventAttempt(call, existing, eventID)
	}
	query := url.Values{}
	if input.RequestConference {
		query.Set("conferenceDataVersion", "1")
	}
	if input.SendUpdates != "" {
		query.Set("sendUpdates", input.SendUpdates)
	}
	response, err := operation.client.send(call, &credentials, providerRequest{
		method: http.MethodPost, pathSuffix: calendarPath(input.CalendarID) + "/events", query: query,
		payload: buildCreateEventPayload(input, eventID),
	})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, eventID))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(sdkgo.FailureTransport, operationID, "insert outcome is unknown; the stable event ID makes the retry safe"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, eventID)
	if response.statusCode == http.StatusConflict {
		return operation.existingEventAttempt(call, operation.client.readEvent(call, &credentials, operationID, input.CalendarID, eventID, ""), eventID)
	}
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	event, err := decodeEvent(response.body, input.CalendarID)
	if err != nil || event.ID != eventID {
		message := "provider returned an event with another ID"
		if err != nil {
			message = "provider returned an invalid event: " + err.Error()
		}
		return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, message), receipt)
	}
	return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{Event: event}, nil, receipt)
}

// existingEventAttempt maps a read of the stable ID, before or after insert, to a createEvent result.
func (operation CreateEventOperation) existingEventAttempt(call sdkgo.Call, read eventReadResult, eventID string) sdkgo.MutationAttempt[CreateEventOutput] {
	const operationID = "createEvent"
	receipt := operation.client.receipt(call, read.response, eventID)
	switch read.outcome {
	case eventReadFound:
		if read.event.Status == cancelledEventStatus {
			return sdkgo.NewMutationBranch(CreateEventBranchConflict, CreateEventOutput{Event: read.event}, failurePointer(sdkgo.FailureConflict, operationID, "event ID belongs to a deleted event"), receipt)
		}
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{Event: read.event, WasAlreadyCreated: true}, nil, receipt)
	case eventReadMissing:
		return sdkgo.NewMutationBranch(CreateEventBranchConflict, CreateEventOutput{}, failurePointer(sdkgo.FailureConflict, operationID, "event ID is used by an event the connection cannot read"), receipt)
	case eventReadRetry:
		return sdkgo.NewMutationRetry[CreateEventOutput](read.failure, read.retryAfter)
	case eventReadRejected:
		return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, &read.failure, receipt)
	case eventReadInvalid:
		return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, &read.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, &read.failure, sdkgo.Receipt{})
	}
}

func validateCreateEventInput(input CreateEventInput, eventID string) error {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return err
	}
	if err := validateClientEventID(eventID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Summary) == "" {
		return errors.New("summary is required")
	}
	if err := ValidateEventTimes(input.Start, input.End); err != nil {
		return err
	}
	if err := validateAttendeeInputs(input.Attendees); err != nil {
		return err
	}
	return validateSendUpdates(input.SendUpdates)
}

func buildCreateEventPayload(input CreateEventInput, eventID string) map[string]any {
	payload := map[string]any{"id": eventID, "summary": input.Summary, "start": input.Start, "end": input.End}
	if input.Description != "" {
		payload["description"] = input.Description
	}
	if len(input.Attendees) > 0 {
		payload["attendees"] = encodeAttendees(input.Attendees)
	}
	if input.RequestConference {
		// The event ID doubles as the conference request ID, so a retried insert cannot request a second conference.
		payload["conferenceData"] = map[string]any{"createRequest": map[string]any{
			"requestId": eventID, "conferenceSolutionKey": map[string]string{"type": googleMeetConferenceSolutionType},
		}}
	}
	return payload
}
