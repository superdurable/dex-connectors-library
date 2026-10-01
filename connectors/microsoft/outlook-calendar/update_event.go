// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateEventInput patches named fields of one existing event. A nil field is left unchanged.
// Start and End are set together and follow the same rules as createEvent.
type UpdateEventInput struct {
	// EventID is the Graph event ID, including an occurrence ID.
	EventID string `json:"eventId"`
	// Subject replaces the title when set; it cannot be blank.
	Subject *string `json:"subject,omitempty"`
	// Body replaces the plain-text body when set; an empty string clears it.
	Body *string `json:"body,omitempty"`
	// Location replaces the location display name when set; an empty string clears it.
	Location *string `json:"location,omitempty"`
	// Start replaces the inclusive start when set, together with End.
	Start *EventDateTime `json:"start,omitempty"`
	// End replaces the exclusive end when set, together with Start.
	End *EventDateTime `json:"end,omitempty"`
	// IsAllDay applies only with Start and End: true makes the event all-day, and false, the
	// default, makes it timed. It is sent whenever Start and End are, so no stale form remains.
	IsAllDay bool `json:"isAllDay,omitempty"`
	// Attendees replaces the complete attendee list when set; an empty list removes every attendee.
	// Graph sends a meeting update only to the attendees that changed.
	Attendees *[]EventAttendeeInput `json:"attendees,omitempty"`
	// TimeZone is an optional IANA zone for the offsets of returned times; blank uses Start's zone or UTC.
	TimeZone string `json:"timeZone,omitempty"`
}

// UpdateEventOutput is the event after the update.
type UpdateEventOutput struct {
	// Event is the event that holds the requested values.
	Event Event `json:"event"`
	// WasAlreadyApplied reports that the event already held every requested value, so no PATCH was sent.
	WasAlreadyApplied bool `json:"wasAlreadyApplied"`
}

// UpdateEventOperation implements the updateEvent connector Mutation.
type UpdateEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (UpdateEventOperation) Definition() sdkgo.MutationDefinition { return UpdateEventDefinition }

// IdempotencyKey returns the Call ID. Updates are made idempotent by the read-compare-patch cycle, not by a provider key.
func (UpdateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the event, returns it unchanged when it already holds every requested value,
// and otherwise PATCHes only the requested fields with If-Match set to the read @odata.etag.
// A retried attempt after a lost response therefore finds its own change and sends no second
// PATCH and no second meeting update. A 412 or 409 from a concurrent edit, a transport failure,
// a 429, or a 5xx is retried from the read.
func (operation UpdateEventOperation) Invoke(call sdkgo.Call, input UpdateEventInput) sdkgo.MutationAttempt[UpdateEventOutput] {
	const operationID = "updateEvent"
	display, err := validateUpdateEventInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, failure, sdkgo.Receipt{})
	}
	current := operation.client.readEvent(call, &credentials, operationID, mailboxRoot, input.EventID, display)
	readReceipt := operation.client.receipt(call, current.response, input.EventID)
	switch current.outcome {
	case eventReadFound:
	case eventReadMissing:
		return sdkgo.NewMutationBranch(UpdateEventBranchNotFound, UpdateEventOutput{}, &current.failure, readReceipt)
	case eventReadRetry:
		return sdkgo.NewMutationRetry[UpdateEventOutput](current.failure, current.retryAfter)
	case eventReadRejected:
		return sdkgo.NewMutationBranch(UpdateEventBranchProviderRejected, UpdateEventOutput{}, &current.failure, readReceipt)
	case eventReadInvalid:
		return sdkgo.NewMutationBranch(UpdateEventBranchInvalidResponse, UpdateEventOutput{}, &current.failure, readReceipt)
	default:
		return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, &current.failure, sdkgo.Receipt{})
	}
	if current.event.IsCancelled {
		return sdkgo.NewMutationBranch(UpdateEventBranchNotFound, UpdateEventOutput{}, failurePointer(sdkgo.FailureNotFound, operationID, "event is cancelled"), readReceipt)
	}
	if isUpdateAlreadyApplied(current.event, input) {
		return sdkgo.NewMutationBranch(UpdateEventBranchUpdated, UpdateEventOutput{Event: current.event, WasAlreadyApplied: true}, nil, readReceipt)
	}
	response, err := operation.client.send(call, &credentials, graphRequest{
		method: http.MethodPatch, path: mailboxRoot + "/events/" + url.PathEscape(input.EventID),
		payload: buildUpdateEventPayload(input), ifMatch: current.event.ETag,
	})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewMutationBranch(UpdateEventBranchInvalidResponse, UpdateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, input.EventID))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(sdkgo.FailureTransport, operationID, "update outcome is unknown; the retry rereads the event first"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, input.EventID)
	switch {
	case response.statusCode == http.StatusPreconditionFailed || response.statusCode == http.StatusConflict:
		return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(sdkgo.FailureConflict, operationID, describeStatus("event changed after it was read", graphErrorCode(response.body))), 0)
	case isMissingResourceStatus(response.statusCode):
		return sdkgo.NewMutationBranch(UpdateEventBranchNotFound, UpdateEventOutput{}, failurePointer(sdkgo.FailureNotFound, operationID, "event was not found"), receipt)
	case !isSuccessStatus(response.statusCode):
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewMutationBranch(UpdateEventBranchProviderRejected, UpdateEventOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	event, err := decodeEvent(response.body, "", display)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchInvalidResponse, UpdateEventOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid event: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateEventBranchUpdated, UpdateEventOutput{Event: event}, nil, receipt)
}

func validateUpdateEventInput(input UpdateEventInput) (eventDisplay, error) {
	if err := validateExistingEventID(input.EventID); err != nil {
		return eventDisplay{}, err
	}
	if input.Subject == nil && input.Body == nil && input.Location == nil && input.Start == nil && input.End == nil && input.Attendees == nil {
		return eventDisplay{}, errors.New("set at least one of subject, body, location, start and end, or attendees")
	}
	if input.Subject != nil && strings.TrimSpace(*input.Subject) == "" {
		return eventDisplay{}, errors.New("subject cannot be blank")
	}
	if (input.Start == nil) != (input.End == nil) {
		return eventDisplay{}, errors.New("set start and end together so the event keeps one unambiguous time range")
	}
	if input.IsAllDay && input.Start == nil {
		return eventDisplay{}, errors.New("isAllDay applies only together with start and end")
	}
	if input.Start != nil {
		if err := ValidateEventTimes(*input.Start, *input.End, input.IsAllDay); err != nil {
			return eventDisplay{}, err
		}
	}
	if input.Attendees != nil {
		if err := validateAttendeeInputs(*input.Attendees); err != nil {
			return eventDisplay{}, err
		}
	}
	displayZone := input.TimeZone
	if displayZone == "" && input.Start != nil {
		displayZone = input.Start.TimeZone
	}
	location, zone, err := loadDisplayLocation(displayZone)
	if err != nil {
		return eventDisplay{}, err
	}
	return eventDisplay{location: location, zone: zone}, nil
}

// isUpdateAlreadyApplied reports whether every requested field already holds its value; times compare by instant.
func isUpdateAlreadyApplied(current Event, input UpdateEventInput) bool {
	if input.Subject != nil && current.Subject != *input.Subject {
		return false
	}
	if input.Body != nil && !isSameBodyText(current.Body, *input.Body) {
		return false
	}
	if input.Location != nil && current.Location != *input.Location {
		return false
	}
	if input.Start != nil && (current.IsAllDay != input.IsAllDay || !isSameInstant(current.Start, *input.Start) || !isSameInstant(current.End, *input.End)) {
		return false
	}
	return input.Attendees == nil || hasSameAttendees(current.Attendees, *input.Attendees)
}

func isSameInstant(current EventDateTime, requested EventDateTime) bool {
	currentInstant, currentErr := current.Instant()
	requestedInstant, requestedErr := requested.Instant()
	return currentErr == nil && requestedErr == nil && currentInstant.Equal(requestedInstant)
}

func buildUpdateEventPayload(input UpdateEventInput) map[string]any {
	payload := map[string]any{}
	if input.Subject != nil {
		payload["subject"] = *input.Subject
	}
	if input.Body != nil {
		payload["body"] = graphItemBody{ContentType: "text", Content: *input.Body}
	}
	if input.Location != nil {
		payload["location"] = graphLocation{DisplayName: *input.Location}
	}
	if input.Start != nil {
		payload["start"] = encodeGraphBoundary(*input.Start)
		payload["end"] = encodeGraphBoundary(*input.End)
		payload["isAllDay"] = input.IsAllDay
	}
	if input.Attendees != nil {
		payload["attendees"] = encodeAttendees(*input.Attendees)
	}
	return payload
}
