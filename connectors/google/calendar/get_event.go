// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetEventInput identifies one event to read.
type GetEventInput struct {
	// CalendarID is the calendar that holds the event, or primary for the authorized user's calendar.
	CalendarID string `json:"calendarId"`
	// EventID is the Google event ID, including an expanded recurring instance ID.
	EventID string `json:"eventId"`
	// TimeZone is an optional IANA zone for the offsets of returned dateTime values; blank uses the calendar's zone.
	TimeZone string `json:"timeZone,omitempty"`
}

// GetEventOperation implements the getEvent connector Query.
type GetEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetEventOperation) Definition() sdkgo.QueryDefinition { return GetEventDefinition }

// Invoke executes one events.get request and classifies its attempt. A deleted event that
// Google still returns selects found with Status cancelled.
func (operation GetEventOperation) Invoke(call sdkgo.Call, input GetEventInput) sdkgo.QueryAttempt[Event] {
	const operationID = "getEvent"
	if err := validateGetEventInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetEventBranchDefect, Event{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetEventBranchDefect, Event{}, failure, sdkgo.Receipt{})
	}
	read := operation.client.readEvent(call, &credentials, operationID, input.CalendarID, input.EventID, input.TimeZone)
	receipt := operation.client.receipt(call, read.response, read.event.ID)
	switch read.outcome {
	case eventReadFound:
		return sdkgo.NewQueryBranch(GetEventBranchFound, read.event, nil, receipt)
	case eventReadMissing:
		return sdkgo.NewQueryBranch(GetEventBranchNotFound, Event{}, &read.failure, receipt)
	case eventReadRetry:
		return sdkgo.NewQueryRetry[Event](read.failure, read.retryAfter)
	case eventReadRejected:
		return sdkgo.NewQueryBranch(GetEventBranchProviderRejected, Event{}, &read.failure, receipt)
	case eventReadInvalid:
		return sdkgo.NewQueryBranch(GetEventBranchInvalidResponse, Event{}, &read.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetEventBranchDefect, Event{}, &read.failure, sdkgo.Receipt{})
	}
}

func validateGetEventInput(input GetEventInput) error {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return err
	}
	if err := validateExistingEventID(input.EventID); err != nil {
		return err
	}
	if input.TimeZone != "" {
		return validateTimeZoneName("timeZone", input.TimeZone)
	}
	return nil
}
