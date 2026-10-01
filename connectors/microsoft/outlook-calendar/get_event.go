// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetEventInput identifies one event to read. Graph event IDs are unique within a
// mailbox, so no calendar ID is needed.
type GetEventInput struct {
	// EventID is the Graph event ID, including an expanded occurrence ID.
	EventID string `json:"eventId"`
	// TimeZone is an optional IANA zone for the offsets of returned times; blank uses UTC.
	TimeZone string `json:"timeZone,omitempty"`
}

// GetEventOperation implements the getEvent connector Query.
type GetEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetEventOperation) Definition() sdkgo.QueryDefinition { return GetEventDefinition }

// Invoke executes one event GET and classifies its attempt. A cancelled meeting that
// Graph still returns selects found with IsCancelled set.
func (operation GetEventOperation) Invoke(call sdkgo.Call, input GetEventInput) sdkgo.QueryAttempt[Event] {
	const operationID = "getEvent"
	display, err := validateGetEventInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetEventBranchDefect, Event{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetEventBranchDefect, Event{}, failure, sdkgo.Receipt{})
	}
	read := operation.client.readEvent(call, &credentials, operationID, mailboxRoot, input.EventID, display)
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

func validateGetEventInput(input GetEventInput) (eventDisplay, error) {
	if err := validateExistingEventID(input.EventID); err != nil {
		return eventDisplay{}, err
	}
	location, zone, err := loadDisplayLocation(input.TimeZone)
	if err != nil {
		return eventDisplay{}, err
	}
	return eventDisplay{location: location, zone: zone}, nil
}
