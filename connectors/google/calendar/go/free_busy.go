// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// maximumFreeBusyCalendars is Google's documented calendarExpansionMax for one query.
const maximumFreeBusyCalendars = 50

// QueryFreeBusyInput asks for the busy intervals of calendars in a bounded window.
// Google counts only events that block time: transparent and cancelled events are free.
type QueryFreeBusyInput struct {
	// CalendarIDs lists 1 to 50 unique calendar IDs, such as primary or colleague@example.com.
	// Group IDs are not supported.
	CalendarIDs []string `json:"calendarIds"`
	// TimeMin is the required window start as RFC 3339 with an explicit offset.
	TimeMin string `json:"timeMin"`
	// TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
	TimeMax string `json:"timeMax"`
	// TimeZone is an optional IANA zone for the offsets of returned intervals; blank uses UTC.
	TimeZone string `json:"timeZone,omitempty"`
}

// QueryFreeBusyOutput holds the busy intervals of every requested calendar in request order.
type QueryFreeBusyOutput struct {
	// TimeMin is the checked window start.
	TimeMin string `json:"timeMin"`
	// TimeMax is the checked window end.
	TimeMax string `json:"timeMax"`
	// TimeZone is the zone of the returned offsets.
	TimeZone string `json:"timeZone"`
	// Calendars lists one entry per requested calendar ID.
	Calendars []CalendarFreeBusy `json:"calendars"`
}

// CalendarFreeBusy is the availability of one calendar.
type CalendarFreeBusy struct {
	// CalendarID is the requested calendar ID.
	CalendarID string `json:"calendarId"`
	// Busy lists busy intervals ordered as Google returned them; empty means free only when Errors is empty.
	Busy []BusyInterval `json:"busy"`
	// Errors lists Google's errors for this calendar. Any error selects the incomplete branch.
	Errors []FreeBusyError `json:"errors,omitempty"`
}

// BusyInterval is one busy time range.
type BusyInterval struct {
	// Start is the inclusive start as RFC 3339 with an explicit offset.
	Start string `json:"start"`
	// End is the exclusive end as RFC 3339 with an explicit offset.
	End string `json:"end"`
}

// FreeBusyError is one error Google reported for a calendar.
type FreeBusyError struct {
	// Domain is Google's broad error category, such as global.
	Domain string `json:"domain"`
	// Reason is Google's reason, such as notFound or internalError; Google may add reasons.
	Reason string `json:"reason"`
}

// QueryFreeBusyOperation implements the queryFreeBusy connector Query.
type QueryFreeBusyOperation struct{ client *Client }

type googleFreeBusyResponse struct {
	TimeMin   string                            `json:"timeMin"`
	TimeMax   string                            `json:"timeMax"`
	Calendars map[string]googleCalendarFreeBusy `json:"calendars"`
}

type googleCalendarFreeBusy struct {
	Busy   []BusyInterval  `json:"busy"`
	Errors []FreeBusyError `json:"errors"`
}

// Definition returns the immutable connector operation definition.
func (QueryFreeBusyOperation) Definition() sdkgo.QueryDefinition { return QueryFreeBusyDefinition }

// Invoke executes one freeBusy.query request and classifies its attempt.
func (operation QueryFreeBusyOperation) Invoke(call sdkgo.Call, input QueryFreeBusyInput) sdkgo.QueryAttempt[QueryFreeBusyOutput] {
	const operationID = "queryFreeBusy"
	if err := validateQueryFreeBusyInput(input); err != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failure, sdkgo.Receipt{})
	}
	items := make([]map[string]string, len(input.CalendarIDs))
	for index, calendarID := range input.CalendarIDs {
		items[index] = map[string]string{"id": calendarID}
	}
	payload := map[string]any{"timeMin": input.TimeMin, "timeMax": input.TimeMax, "items": items}
	if input.TimeZone != "" {
		payload["timeZone"] = input.TimeZone
	}
	response, err := operation.client.send(call, &credentials, providerRequest{method: http.MethodPost, pathSuffix: "/freeBusy", payload: payload})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchInvalidResponse, QueryFreeBusyOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewQueryRetry[QueryFreeBusyOutput](calendarFailure(sdkgo.FailureAvailability, operationID, "provider is unavailable"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, "")
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewQueryRetry[QueryFreeBusyOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchProviderRejected, QueryFreeBusyOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	output, err := decodeFreeBusy(response.body, input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchInvalidResponse, QueryFreeBusyOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid free/busy response: "+err.Error()), receipt)
	}
	for _, calendar := range output.Calendars {
		if len(calendar.Errors) != 0 {
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchIncomplete, output, failurePointer(sdkgo.FailureProviderRejection, operationID, "provider could not report availability for every calendar"), receipt)
		}
	}
	return sdkgo.NewQueryBranch(QueryFreeBusyBranchQueried, output, nil, receipt)
}

func validateQueryFreeBusyInput(input QueryFreeBusyInput) error {
	if len(input.CalendarIDs) == 0 || len(input.CalendarIDs) > maximumFreeBusyCalendars {
		return fmt.Errorf("calendarIds must list 1 to %d calendars", maximumFreeBusyCalendars)
	}
	seen := make(map[string]bool, len(input.CalendarIDs))
	for _, calendarID := range input.CalendarIDs {
		if err := validateCalendarID(calendarID); err != nil {
			return err
		}
		if seen[calendarID] {
			return fmt.Errorf("calendar ID %q is duplicated", calendarID)
		}
		seen[calendarID] = true
	}
	if _, _, err := parseQueryWindow(input.TimeMin, input.TimeMax); err != nil {
		return err
	}
	if input.TimeZone != "" {
		return validateTimeZoneName("timeZone", input.TimeZone)
	}
	return nil
}

func decodeFreeBusy(body []byte, input QueryFreeBusyInput) (QueryFreeBusyOutput, error) {
	var response googleFreeBusyResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return QueryFreeBusyOutput{}, errors.New("free/busy response is not valid JSON")
	}
	output := QueryFreeBusyOutput{
		TimeMin: firstNonEmpty(response.TimeMin, input.TimeMin), TimeMax: firstNonEmpty(response.TimeMax, input.TimeMax),
		TimeZone: firstNonEmpty(input.TimeZone, "UTC"), Calendars: make([]CalendarFreeBusy, 0, len(input.CalendarIDs)),
	}
	for _, calendarID := range input.CalendarIDs {
		availability, ok := lookupCalendarFreeBusy(response.Calendars, calendarID)
		if !ok {
			return QueryFreeBusyOutput{}, fmt.Errorf("calendar %q is missing from the response", calendarID)
		}
		for _, interval := range availability.Busy {
			start, startErr := parseInstant("busy.start", interval.Start)
			end, endErr := parseInstant("busy.end", interval.End)
			if startErr != nil || endErr != nil || !end.After(start) {
				return QueryFreeBusyOutput{}, fmt.Errorf("calendar %q has a busy interval without two ordered RFC 3339 offsets", calendarID)
			}
		}
		busy := availability.Busy
		if busy == nil {
			busy = []BusyInterval{}
		}
		output.Calendars = append(output.Calendars, CalendarFreeBusy{CalendarID: calendarID, Busy: busy, Errors: availability.Errors})
	}
	return output, nil
}

// lookupCalendarFreeBusy matches the requested ID exactly, then case-insensitively for email calendar IDs.
func lookupCalendarFreeBusy(calendars map[string]googleCalendarFreeBusy, calendarID string) (googleCalendarFreeBusy, bool) {
	if availability, ok := calendars[calendarID]; ok {
		return availability, true
	}
	for key, availability := range calendars {
		if strings.EqualFold(key, calendarID) {
			return availability, true
		}
	}
	return googleCalendarFreeBusy{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
