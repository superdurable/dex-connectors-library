// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultListEventsPageSize = 50
	maximumListEventsPageSize = 250
)

// ListEventsInput selects one page of events on one calendar that overlap a bounded window.
// Google returns an event when its end is after TimeMin and its start is before TimeMax.
// Recurring events are expanded into their instances and the page is ordered by start.
type ListEventsInput struct {
	// CalendarID is a calendar ID from the calendar picker, or primary for the authorized user's calendar.
	CalendarID string `json:"calendarId"`
	// TimeMin is the required window start as RFC 3339 with an explicit offset.
	TimeMin string `json:"timeMin"`
	// TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
	TimeMax string `json:"timeMax"`
	// Query is optional free text Google matches against summary, description, location, and guests.
	Query string `json:"query,omitempty"`
	// PageSize is the maximum events in the page, from 1 to 250; zero requests 50.
	// Google may return fewer events, even none, before the last page.
	PageSize int64 `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page. Keep every other field unchanged when paging.
	PageToken string `json:"pageToken,omitempty"`
	// TimeZone is an optional IANA zone for the offsets of returned dateTime values; blank uses the calendar's zone.
	TimeZone string `json:"timeZone,omitempty"`
}

// ListEventsOutput is one page of events and the window Google checked.
type ListEventsOutput struct {
	// CalendarID is the listed calendar.
	CalendarID string `json:"calendarId"`
	// TimeMin echoes the checked window start.
	TimeMin string `json:"timeMin"`
	// TimeMax echoes the checked window end.
	TimeMax string `json:"timeMax"`
	// TimeZone is the calendar's IANA zone, which gives all-day dates their instants.
	TimeZone string `json:"timeZone"`
	// Events lists the page's events ordered by start. Cancelled events are omitted.
	Events []Event `json:"events"`
	// NextPageToken requests the next page, or is empty on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListEventsOperation implements the listEvents connector Query.
type ListEventsOperation struct{ client *Client }

type googleEventList struct {
	TimeZone      string        `json:"timeZone"`
	NextPageToken string        `json:"nextPageToken"`
	Items         []googleEvent `json:"items"`
}

// Definition returns the immutable connector operation definition.
func (ListEventsOperation) Definition() sdkgo.QueryDefinition { return ListEventsDefinition }

// Invoke executes one events.list request and classifies its attempt.
func (operation ListEventsOperation) Invoke(call sdkgo.Call, input ListEventsInput) sdkgo.QueryAttempt[ListEventsOutput] {
	const operationID = "listEvents"
	pageSize, err := validateListEventsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchDefect, ListEventsOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchDefect, ListEventsOutput{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{
		"singleEvents": {"true"}, "orderBy": {"startTime"}, "showDeleted": {"false"},
		"timeMin": {input.TimeMin}, "timeMax": {input.TimeMax}, "maxResults": {strconv.FormatInt(pageSize, 10)},
	}
	for name, value := range map[string]string{"q": input.Query, "pageToken": input.PageToken, "timeZone": input.TimeZone} {
		if value != "" {
			query.Set(name, value)
		}
	}
	response, err := operation.client.send(call, &credentials, providerRequest{
		method: http.MethodGet, pathSuffix: calendarPath(input.CalendarID) + "/events", query: query,
	})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewQueryBranch(ListEventsBranchInvalidResponse, ListEventsOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewQueryBranch(ListEventsBranchDefect, ListEventsOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewQueryRetry[ListEventsOutput](calendarFailure(sdkgo.FailureAvailability, operationID, "provider is unavailable"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, "")
	if isMissingResourceStatus(response.statusCode) {
		return sdkgo.NewQueryBranch(ListEventsBranchNotFound, ListEventsOutput{}, failurePointer(sdkgo.FailureNotFound, operationID, "calendar was not found"), receipt)
	}
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewQueryRetry[ListEventsOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewQueryBranch(ListEventsBranchProviderRejected, ListEventsOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	output, err := decodeEventList(response.body, input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchInvalidResponse, ListEventsOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid event page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListEventsBranchListed, output, nil, receipt)
}

func validateListEventsInput(input ListEventsInput) (int64, error) {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return 0, err
	}
	if _, _, err := parseQueryWindow(input.TimeMin, input.TimeMax); err != nil {
		return 0, err
	}
	if input.TimeZone != "" {
		if err := validateTimeZoneName("timeZone", input.TimeZone); err != nil {
			return 0, err
		}
	}
	switch {
	case input.PageSize == 0:
		return defaultListEventsPageSize, nil
	case input.PageSize < 0 || input.PageSize > maximumListEventsPageSize:
		return 0, errors.New("pageSize must be between 1 and 250, or zero for the default")
	default:
		return input.PageSize, nil
	}
}

func decodeEventList(body []byte, input ListEventsInput) (ListEventsOutput, error) {
	var page googleEventList
	if err := json.Unmarshal(body, &page); err != nil {
		return ListEventsOutput{}, errors.New("event page is not valid JSON")
	}
	if page.TimeZone != "" {
		if err := validateTimeZoneName("timeZone", page.TimeZone); err != nil {
			return ListEventsOutput{}, err
		}
	}
	output := ListEventsOutput{
		CalendarID: input.CalendarID, TimeMin: input.TimeMin, TimeMax: input.TimeMax,
		TimeZone: page.TimeZone, Events: make([]Event, 0, len(page.Items)), NextPageToken: page.NextPageToken,
	}
	for _, item := range page.Items {
		event, err := convertEvent(item, input.CalendarID)
		if err != nil {
			return ListEventsOutput{}, err
		}
		if event.Status == cancelledEventStatus {
			continue
		}
		output.Events = append(output.Events, event)
	}
	return output, nil
}
