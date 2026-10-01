// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateEventInput describes one event to create. Every attempt of one Step execution
// uses the same idempotency key: TransactionID when set, otherwise the Step's Call ID.
// The key is sent as Graph's transactionId and stamped on the event as a connector-owned
// extended property, so a retried Step finds and returns its own event.
type CreateEventInput struct {
	// CalendarID is a writable calendar ID from the calendar picker; blank uses the mailbox's default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// TransactionID optionally fixes the idempotency key for business-level deduplication across Step
	// executions: 1 to 64 letters, digits, dots, hyphens, or underscores. Blank uses the Call ID.
	TransactionID string `json:"transactionId,omitempty"`
	// Subject is the required event title.
	Subject string `json:"subject"`
	// Body is the optional plain-text event body.
	Body string `json:"body,omitempty"`
	// Location is the optional location display name.
	Location string `json:"location,omitempty"`
	// Start is the inclusive start; see EventDateTime and ValidateEventTimes.
	Start EventDateTime `json:"start"`
	// End is the exclusive end; see EventDateTime and ValidateEventTimes.
	End EventDateTime `json:"end"`
	// IsAllDay creates an all-day event; Start and End must then be midnights in one zone.
	IsAllDay bool `json:"isAllDay,omitempty"`
	// Attendees lists unique bare attendee addresses. Graph sends every attendee an invitation
	// when the event is created, and that cannot be turned off; empty creates an event without attendees.
	Attendees []EventAttendeeInput `json:"attendees,omitempty"`
	// RequestOnlineMeeting asks Graph to attach a Microsoft Teams meeting to the event.
	RequestOnlineMeeting bool `json:"requestOnlineMeeting,omitempty"`
}

// CreateEventOutput is the event that carries the Step's idempotency key.
type CreateEventOutput struct {
	// Event is the created or previously created event.
	Event Event `json:"event"`
	// WasAlreadyCreated reports that an earlier attempt had already created this event, so no POST was sent.
	WasAlreadyCreated bool `json:"wasAlreadyCreated"`
	// DuplicateEventIDs lists further events with the same idempotency key. Graph's transactionId is meant
	// to prevent them, so a non-empty list means two concurrent attempts both created an event.
	DuplicateEventIDs []string `json:"duplicateEventIds,omitempty"`
}

// CreateEventOperation implements the createEvent connector Mutation.
type CreateEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// IdempotencyKey returns input.TransactionID when set, otherwise the Call ID, a UUID.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, input CreateEventInput) sdkgo.IdempotencyKey {
	if input.TransactionID != "" {
		return sdkgo.IdempotencyKey(input.TransactionID)
	}
	return sdkgo.IdempotencyKey(callID)
}

// Invoke looks the idempotency key up first and returns an event that already carries it.
// Otherwise it POSTs the event with the key as transactionId and as the extended-property
// marker. A lost response, a 5xx, or a 429 is retried, and the retry finds the event through
// the marker; a 400 or 409 is checked the same way before it is reported. Graph does not
// document what a repeated transactionId returns, so the connector never relies on it alone.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[CreateEventOutput] {
	const operationID = "createEvent"
	idempotencyKey := string(call.IdempotencyKey)
	display, err := validateCreateEventInput(input, idempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failure, sdkgo.Receipt{})
	}
	existing := operation.client.findEventByIdempotencyKey(call, &credentials, operationID, mailboxRoot, idempotencyKey, input.CalendarID, display)
	if existing.outcome != eventReadMissing {
		return operation.existingEventAttempt(call, existing)
	}
	response, err := operation.client.send(call, &credentials, graphRequest{
		method: http.MethodPost, path: calendarPath(mailboxRoot, input.CalendarID) + "/events",
		payload: buildCreateEventPayload(input, idempotencyKey),
	})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(sdkgo.FailureTransport, operationID, "create outcome is unknown; the retry looks the idempotency key up first"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, "")
	switch {
	case isSuccessStatus(response.statusCode):
		event, err := decodeEvent(response.body, input.CalendarID, display)
		if err != nil || (event.TransactionID != "" && event.TransactionID != idempotencyKey) {
			message := "provider returned an event with another transactionId"
			if err != nil {
				message = "provider returned an invalid event: " + err.Error()
			}
			return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, message), receipt)
		}
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{Event: event}, nil, operation.client.receipt(call, response, event.ID))
	case response.statusCode == http.StatusBadRequest || response.statusCode == http.StatusConflict:
		return operation.rejectedCreateAttempt(call, &credentials, mailboxRoot, input, idempotencyKey, display, response)
	}
	classification := classifyFailureStatus(response)
	if classification.isRetryable {
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
	}
	return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
}

// rejectedCreateAttempt reads the key back after a 400 or 409; a 409 without the event may still be committing.
func (operation CreateEventOperation) rejectedCreateAttempt(call sdkgo.Call, credentials *Credentials, mailboxRoot string, input CreateEventInput, idempotencyKey string, display eventDisplay, rejected graphResponse) sdkgo.MutationAttempt[CreateEventOutput] {
	const operationID = "createEvent"
	readBack := operation.client.findEventByIdempotencyKey(call, credentials, operationID, mailboxRoot, idempotencyKey, input.CalendarID, display)
	if readBack.outcome != eventReadMissing {
		return operation.existingEventAttempt(call, readBack)
	}
	classification := classifyFailureStatus(rejected)
	if rejected.statusCode == http.StatusConflict {
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
	}
	return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, failurePointer(classification.kind, operationID, classification.message), operation.client.receipt(call, rejected, ""))
}

// existingEventAttempt maps a lookup of the idempotency key, before or after the POST, to a createEvent result.
func (operation CreateEventOperation) existingEventAttempt(call sdkgo.Call, read eventReadResult) sdkgo.MutationAttempt[CreateEventOutput] {
	const operationID = "createEvent"
	receipt := operation.client.receipt(call, read.response, read.event.ID)
	switch read.outcome {
	case eventReadFound:
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{
			Event: read.event, WasAlreadyCreated: true, DuplicateEventIDs: read.duplicateEventIDs,
		}, nil, receipt)
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

func validateCreateEventInput(input CreateEventInput, idempotencyKey string) (eventDisplay, error) {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return eventDisplay{}, err
	}
	if err := validateTransactionID(idempotencyKey); err != nil {
		return eventDisplay{}, err
	}
	if strings.TrimSpace(input.Subject) == "" {
		return eventDisplay{}, errors.New("subject is required")
	}
	if err := ValidateEventTimes(input.Start, input.End, input.IsAllDay); err != nil {
		return eventDisplay{}, err
	}
	if err := validateAttendeeInputs(input.Attendees); err != nil {
		return eventDisplay{}, err
	}
	location, zone, err := loadDisplayLocation(input.Start.TimeZone)
	if err != nil {
		return eventDisplay{}, err
	}
	return eventDisplay{location: location, zone: zone}, nil
}

func buildCreateEventPayload(input CreateEventInput, idempotencyKey string) map[string]any {
	payload := map[string]any{
		"subject": input.Subject, "start": encodeGraphBoundary(input.Start), "end": encodeGraphBoundary(input.End),
		"transactionId":                 idempotencyKey,
		"singleValueExtendedProperties": []map[string]string{{"id": idempotencyMarkerPropertyID, "value": idempotencyKey}},
	}
	if input.IsAllDay {
		payload["isAllDay"] = true
	}
	if input.Body != "" {
		payload["body"] = graphItemBody{ContentType: "text", Content: input.Body}
	}
	if input.Location != "" {
		payload["location"] = graphLocation{DisplayName: input.Location}
	}
	if len(input.Attendees) > 0 {
		payload["attendees"] = encodeAttendees(input.Attendees)
	}
	if input.RequestOnlineMeeting {
		payload["isOnlineMeeting"] = true
		payload["onlineMeetingProvider"] = teamsOnlineMeetingProvider
	}
	return payload
}
