// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Calendar is the non-secret identity and editability of one Outlook calendar.
type Calendar struct {
	// ID is the stable Graph calendar ID.
	ID string `json:"id"`
	// Name is the calendar's display name.
	Name string `json:"name"`
	// CanEdit reports whether Graph permits changes to this calendar.
	CanEdit bool `json:"canEdit"`
}

// ListCalendarsInput requests one page of calendars.
type ListCalendarsInput struct {
	// PageSize limits one response to 1 through 100 calendars; zero selects 50.
	PageSize int64 `json:"pageSize,omitempty"`
	// PageToken is the opaque nextPageToken from the preceding page.
	PageToken string `json:"pageToken,omitempty"`
}

// ListCalendarsOutput contains one calendar page.
type ListCalendarsOutput struct {
	// Calendars lists the returned calendars.
	Calendars []Calendar `json:"calendars"`
	// NextPageToken is Graph's opaque link for another page, or blank.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetCalendarInput identifies one calendar.
type GetCalendarInput struct {
	// CalendarID is the Graph calendar ID.
	CalendarID string `json:"calendarId"`
}

// ListCalendarsOperation implements the listCalendars query.
type ListCalendarsOperation struct{ client *Client }

// GetCalendarOperation implements the getCalendar query.
type GetCalendarOperation struct{ client *Client }

// Definition returns the list query definition.
func (ListCalendarsOperation) Definition() sdkgo.QueryDefinition { return ListCalendarsDefinition }

// Definition returns the get query definition.
func (GetCalendarOperation) Definition() sdkgo.QueryDefinition { return GetCalendarDefinition }

// Invoke lists a bounded calendar page.
func (operation ListCalendarsOperation) Invoke(call sdkgo.Call, input ListCalendarsInput) sdkgo.QueryAttempt[ListCalendarsOutput] {
	const operationID = "listCalendars"
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return sdkgo.NewQueryBranch(ListCalendarsBranchDefect, ListCalendarsOutput{}, failurePointer(sdkgo.FailureValidation, operationID, "pageSize must be 1 through 100"), sdkgo.Receipt{})
	}
	if input.PageToken != "" && validateCalendarsNextLink(input.PageToken) != nil {
		return sdkgo.NewQueryBranch(ListCalendarsBranchDefect, ListCalendarsOutput{}, failurePointer(sdkgo.FailureValidation, operationID, "pageToken is invalid"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListCalendarsBranchDefect, ListCalendarsOutput{}, failure, sdkgo.Receipt{})
	}
	request := graphRequest{method: http.MethodGet, nextLink: input.PageToken}
	if input.PageToken == "" {
		request.path = mailboxRoot + "/calendars"
		request.query = url.Values{"$top": {strconv.FormatInt(pageSize, 10)}, "$select": {"id,name,canEdit"}}
	}
	response, err := operation.client.send(call, &credentials, request)
	if err != nil {
		requestFailure := asProviderRequestError(err)
		if requestFailure.kind == sdkgo.FailureResponseTooLarge {
			return sdkgo.NewQueryBranch(ListCalendarsBranchInvalidResponse, ListCalendarsOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		}
		if requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewQueryBranch(ListCalendarsBranchDefect, ListCalendarsOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[ListCalendarsOutput](calendarFailure(sdkgo.FailureTransport, operationID, "provider request failed"), 0)
	}
	receipt := operation.client.receipt(call, response, "")
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewQueryRetry[ListCalendarsOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewQueryBranch(ListCalendarsBranchProviderRejected, ListCalendarsOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	var page struct {
		Value    []Calendar `json:"value"`
		NextLink string     `json:"@odata.nextLink"`
	}
	if err := json.Unmarshal(response.body, &page); err != nil || page.Value == nil || len(page.Value) > int(pageSize) {
		return sdkgo.NewQueryBranch(ListCalendarsBranchInvalidResponse, ListCalendarsOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned invalid calendar page"), receipt)
	}
	for _, item := range page.Value {
		if item.ID == "" || strings.TrimSpace(item.Name) == "" {
			return sdkgo.NewQueryBranch(ListCalendarsBranchInvalidResponse, ListCalendarsOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned invalid calendar metadata"), receipt)
		}
	}
	if page.NextLink != "" && validateCalendarsNextLink(page.NextLink) != nil {
		return sdkgo.NewQueryBranch(ListCalendarsBranchInvalidResponse, ListCalendarsOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid nextLink"), receipt)
	}
	return sdkgo.NewQueryBranch(ListCalendarsBranchListed, ListCalendarsOutput{Calendars: page.Value, NextPageToken: page.NextLink}, nil, receipt)
}

// Invoke reads one calendar by ID.
func (operation GetCalendarOperation) Invoke(call sdkgo.Call, input GetCalendarInput) sdkgo.QueryAttempt[Calendar] {
	const operationID = "getCalendar"
	if validateExistingEventID(input.CalendarID) != nil {
		return sdkgo.NewQueryBranch(GetCalendarBranchDefect, Calendar{}, failurePointer(sdkgo.FailureValidation, operationID, "calendarId is invalid"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetCalendarBranchDefect, Calendar{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.send(call, &credentials, graphRequest{method: http.MethodGet, path: calendarPath(mailboxRoot, input.CalendarID), query: url.Values{"$select": {"id,name,canEdit"}}})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		if requestFailure.kind == sdkgo.FailureResponseTooLarge {
			return sdkgo.NewQueryBranch(GetCalendarBranchInvalidResponse, Calendar{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		}
		if requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewQueryBranch(GetCalendarBranchDefect, Calendar{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[Calendar](calendarFailure(sdkgo.FailureTransport, operationID, "provider request failed"), 0)
	}
	receipt := operation.client.receipt(call, response, input.CalendarID)
	if isMissingResourceStatus(response.statusCode) {
		return sdkgo.NewQueryBranch(GetCalendarBranchNotFound, Calendar{}, failurePointer(sdkgo.FailureNotFound, operationID, "calendar was not found"), receipt)
	}
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewQueryRetry[Calendar](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewQueryBranch(GetCalendarBranchProviderRejected, Calendar{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	var item Calendar
	if err := json.Unmarshal(response.body, &item); err != nil || item.ID != input.CalendarID || strings.TrimSpace(item.Name) == "" {
		return sdkgo.NewQueryBranch(GetCalendarBranchInvalidResponse, Calendar{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned invalid calendar metadata"), receipt)
	}
	return sdkgo.NewQueryBranch(GetCalendarBranchFound, item, nil, receipt)
}

func validateCalendarsNextLink(nextLink string) error {
	if err := validateNextLink(nextLink); err != nil {
		return err
	}
	parsed, _ := url.Parse(nextLink)
	if !strings.HasSuffix(strings.ToLower(parsed.Path), "/calendars") {
		return &providerRequestError{kind: sdkgo.FailureValidation, message: "pageToken must continue a calendars listing"}
	}
	return nil
}
