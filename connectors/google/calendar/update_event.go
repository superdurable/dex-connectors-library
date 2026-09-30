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

// UpdateEventInput patches named fields of one existing event. A nil field is left unchanged.
// Start and End are set together, and follow the same rules as createEvent, so an event can
// move between timed and all-day without keeping a stale boundary.
type UpdateEventInput struct {
	// CalendarID is the calendar that holds the event, or primary for the authorized user's calendar.
	CalendarID string `json:"calendarId"`
	// EventID is the Google event ID, including an expanded recurring instance ID.
	EventID string `json:"eventId"`
	// Summary replaces the title when set; it cannot be blank.
	Summary *string `json:"summary,omitempty"`
	// Description replaces the description when set; an empty string clears it.
	Description *string `json:"description,omitempty"`
	// Start replaces the inclusive start when set, together with End.
	Start *EventDateTime `json:"start,omitempty"`
	// End replaces the exclusive end when set, together with Start.
	End *EventDateTime `json:"end,omitempty"`
	// Attendees replaces the complete guest list when set; an empty list removes every guest.
	Attendees *[]EventAttendeeInput `json:"attendees,omitempty"`
	// SendUpdates is none, all, or externalOnly; blank sends no guest notifications.
	SendUpdates string `json:"sendUpdates,omitempty"`
}

// UpdateEventOutput is the event after the patch.
type UpdateEventOutput struct {
	// Event is the event that holds the requested values.
	Event Event `json:"event"`
	// WasAlreadyApplied reports that the event already held every requested value, so no patch was sent.
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
// and otherwise patches it with If-Match set to the read ETag. A retried attempt after a lost
// response therefore finds its own change and sends no second patch or guest notification.
// A 412 from a concurrent edit, a transport failure, or a 5xx is retried from the read.
func (operation UpdateEventOperation) Invoke(call sdkgo.Call, input UpdateEventInput) sdkgo.MutationAttempt[UpdateEventOutput] {
	const operationID = "updateEvent"
	if err := validateUpdateEventInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchDefect, UpdateEventOutput{}, failure, sdkgo.Receipt{})
	}
	current := operation.client.readEvent(call, &credentials, operationID, input.CalendarID, input.EventID, "")
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
	if current.event.Status == cancelledEventStatus {
		return sdkgo.NewMutationBranch(UpdateEventBranchNotFound, UpdateEventOutput{}, failurePointer(sdkgo.FailureNotFound, operationID, "event is cancelled"), readReceipt)
	}
	if isUpdateAlreadyApplied(current.event, input) {
		return sdkgo.NewMutationBranch(UpdateEventBranchUpdated, UpdateEventOutput{Event: current.event, WasAlreadyApplied: true}, nil, readReceipt)
	}
	query := url.Values{}
	if input.SendUpdates != "" {
		query.Set("sendUpdates", input.SendUpdates)
	}
	response, err := operation.client.send(call, &credentials, providerRequest{
		method: http.MethodPatch, pathSuffix: eventPath(input.CalendarID, input.EventID), query: query,
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
			return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(sdkgo.FailureTransport, operationID, "patch outcome is unknown; the retry rereads the event first"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, input.EventID)
	switch {
	case response.statusCode == http.StatusPreconditionFailed:
		return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(sdkgo.FailureConflict, operationID, "event changed after it was read"), 0)
	case isMissingResourceStatus(response.statusCode):
		return sdkgo.NewMutationBranch(UpdateEventBranchNotFound, UpdateEventOutput{}, failurePointer(sdkgo.FailureNotFound, operationID, "calendar or event was not found"), receipt)
	case !isSuccessStatus(response.statusCode):
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewMutationRetry[UpdateEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewMutationBranch(UpdateEventBranchProviderRejected, UpdateEventOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	event, err := decodeEvent(response.body, input.CalendarID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateEventBranchInvalidResponse, UpdateEventOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid event: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateEventBranchUpdated, UpdateEventOutput{Event: event}, nil, receipt)
}

func validateUpdateEventInput(input UpdateEventInput) error {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return err
	}
	if err := validateExistingEventID(input.EventID); err != nil {
		return err
	}
	if input.Summary == nil && input.Description == nil && input.Start == nil && input.End == nil && input.Attendees == nil {
		return errors.New("set at least one of summary, description, start and end, or attendees")
	}
	if input.Summary != nil && strings.TrimSpace(*input.Summary) == "" {
		return errors.New("summary cannot be blank")
	}
	if (input.Start == nil) != (input.End == nil) {
		return errors.New("set start and end together so the event keeps one unambiguous time range")
	}
	if input.Start != nil {
		if err := ValidateEventTimes(*input.Start, *input.End); err != nil {
			return err
		}
	}
	if input.Attendees != nil {
		if err := validateAttendeeInputs(*input.Attendees); err != nil {
			return err
		}
	}
	return validateSendUpdates(input.SendUpdates)
}

// isUpdateAlreadyApplied reports whether every requested field already holds its requested value.
func isUpdateAlreadyApplied(current Event, input UpdateEventInput) bool {
	if input.Summary != nil && current.Summary != *input.Summary {
		return false
	}
	if input.Description != nil && current.Description != *input.Description {
		return false
	}
	if input.Start != nil && (!isSameEventBoundary(current.Start, *input.Start) || !isSameEventBoundary(current.End, *input.End)) {
		return false
	}
	return input.Attendees == nil || hasSameAttendees(current.Attendees, *input.Attendees)
}

func buildUpdateEventPayload(input UpdateEventInput) map[string]any {
	payload := map[string]any{}
	if input.Summary != nil {
		payload["summary"] = *input.Summary
	}
	if input.Description != nil {
		payload["description"] = *input.Description
	}
	if input.Start != nil {
		payload["start"] = encodeEventBoundary(*input.Start)
		payload["end"] = encodeEventBoundary(*input.End)
	}
	if input.Attendees != nil {
		payload["attendees"] = encodeAttendees(*input.Attendees)
	}
	return payload
}
