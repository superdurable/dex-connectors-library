// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultListEventsPageSize = 50
	// maximumListEventsPageSize keeps one page small; Graph's calendarView accepts $top up to 1000.
	maximumListEventsPageSize = 250
)

// ListEventsInput selects one page of a calendar view: the occurrences, exceptions, and
// single instances of events that overlap a bounded window, with recurring series expanded.
type ListEventsInput struct {
	// CalendarID is a calendar ID from the calendar picker; blank lists the mailbox's default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// TimeMin is the required window start as RFC 3339 with an explicit offset.
	TimeMin string `json:"timeMin"`
	// TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
	TimeMax string `json:"timeMax"`
	// PageSize is the maximum events in the page, from 1 to 250; zero requests 50.
	// Graph may return fewer events, even none, before the last page.
	PageSize int64 `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page. Keep every other field unchanged when paging.
	PageToken string `json:"pageToken,omitempty"`
	// TimeZone is an optional IANA zone for the offsets of returned times; blank uses UTC.
	TimeZone string `json:"timeZone,omitempty"`
}

// ListEventsOutput is one page of the calendar view.
type ListEventsOutput struct {
	// CalendarID echoes the listed calendar; blank means the default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// TimeMin echoes the checked window start.
	TimeMin string `json:"timeMin"`
	// TimeMax echoes the checked window end.
	TimeMax string `json:"timeMax"`
	// TimeZone is the IANA zone of every returned offset.
	TimeZone string `json:"timeZone"`
	// Events lists the page's events sorted by start instant within the page. Cancelled meetings
	// that still show on the calendar are included with IsCancelled set.
	Events []Event `json:"events"`
	// NextPageToken requests the next page, or is empty on the last page. It is Graph's opaque
	// @odata.nextLink URL; pass it back unchanged.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListEventsOperation implements the listEvents connector Query.
type ListEventsOperation struct{ client *Client }

type graphEventPage struct {
	Value    []graphEvent `json:"value"`
	NextLink string       `json:"@odata.nextLink"`
}

// Definition returns the immutable connector operation definition.
func (ListEventsOperation) Definition() sdkgo.QueryDefinition { return ListEventsDefinition }

// Invoke executes one calendarView request, or follows the validated nextLink in PageToken,
// and classifies its attempt.
func (operation ListEventsOperation) Invoke(call sdkgo.Call, input ListEventsInput) sdkgo.QueryAttempt[ListEventsOutput] {
	const operationID = "listEvents"
	pageSize, display, err := validateListEventsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchDefect, ListEventsOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchDefect, ListEventsOutput{}, failure, sdkgo.Receipt{})
	}
	request := graphRequest{method: http.MethodGet, nextLink: input.PageToken}
	if input.PageToken == "" {
		request.path = calendarPath(mailboxRoot, input.CalendarID) + "/calendarView"
		request.query = url.Values{
			"startDateTime": {input.TimeMin}, "endDateTime": {input.TimeMax},
			"$top": {strconv.FormatInt(pageSize, 10)}, "$select": {listEventSelectFields},
		}
	}
	response, err := operation.client.send(call, &credentials, request)
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
	output, err := decodeEventPage(response.body, input, display)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEventsBranchInvalidResponse, ListEventsOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid event page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListEventsBranchListed, output, nil, receipt)
}

func validateListEventsInput(input ListEventsInput) (int64, eventDisplay, error) {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return 0, eventDisplay{}, err
	}
	start, end, err := parseQueryWindow(input.TimeMin, input.TimeMax)
	if err != nil {
		return 0, eventDisplay{}, err
	}
	if end.Sub(start) > maximumListWindow {
		return 0, eventDisplay{}, errors.New("the timeMin to timeMax window cannot exceed 366 days")
	}
	location, zone, err := loadDisplayLocation(input.TimeZone)
	if err != nil {
		return 0, eventDisplay{}, err
	}
	if input.PageToken != "" {
		if err := validateNextLink(input.PageToken); err != nil {
			return 0, eventDisplay{}, err
		}
	}
	switch {
	case input.PageSize == 0:
		return defaultListEventsPageSize, eventDisplay{location: location, zone: zone}, nil
	case input.PageSize < 0 || input.PageSize > maximumListEventsPageSize:
		return 0, eventDisplay{}, errors.New("pageSize must be between 1 and 250, or zero for the default")
	default:
		return input.PageSize, eventDisplay{location: location, zone: zone}, nil
	}
}

func decodeEventPage(body []byte, input ListEventsInput, display eventDisplay) (ListEventsOutput, error) {
	var page graphEventPage
	if err := json.Unmarshal(body, &page); err != nil || page.Value == nil {
		return ListEventsOutput{}, errors.New("event page is not a JSON object with a value array")
	}
	if page.NextLink != "" {
		if err := validateNextLink(page.NextLink); err != nil {
			return ListEventsOutput{}, errors.New("event page has a nextLink outside https://graph.microsoft.com/v1.0")
		}
	}
	output := ListEventsOutput{
		CalendarID: input.CalendarID, TimeMin: input.TimeMin, TimeMax: input.TimeMax, TimeZone: display.zone,
		Events: make([]Event, 0, len(page.Value)), NextPageToken: page.NextLink,
	}
	startInstants := make(map[string]time.Time, len(page.Value))
	for _, item := range page.Value {
		event, err := convertEvent(item, input.CalendarID, display)
		if err != nil {
			return ListEventsOutput{}, err
		}
		startInstants[event.ID], _ = event.Start.Instant()
		output.Events = append(output.Events, event)
	}
	sort.SliceStable(output.Events, func(left int, right int) bool {
		return startInstants[output.Events[left].ID].Before(startInstants[output.Events[right].ID])
	})
	return output, nil
}
