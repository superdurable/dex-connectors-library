// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listedEventsPageJSON = `{
  "timeZone": "America/Los_Angeles",
  "nextPageToken": "page-2",
  "items": [
    {"id": "evtallday01", "status": "confirmed", "summary": "Company holiday", "transparency": "transparent",
     "start": {"date": "2026-02-20"}, "end": {"date": "2026-02-21"}},
    {"id": "evttztrap01", "status": "confirmed", "summary": "Vendor call", "description": "Starts 2:00 PM PST",
     "start": {"dateTime": "2026-02-20T11:00:00-08:00", "timeZone": "America/Los_Angeles"},
     "end": {"dateTime": "2026-02-20T12:00:00-08:00", "timeZone": "America/Los_Angeles"},
     "attendees": [{"email": "ben@example.com", "displayName": "Ben", "responseStatus": "accepted", "optional": true},
                   {"email": "owner@example.com", "responseStatus": "accepted", "organizer": true, "self": true}],
     "organizer": {"email": "owner@example.com"},
     "conferenceData": {"conferenceId": "abc-defg-hij", "conferenceSolution": {"key": {"type": "hangoutsMeet"}},
                        "entryPoints": [{"entryPointType": "phone", "uri": "tel:+1-555-0100"}, {"entryPointType": "video", "uri": "https://meet.google.com/abc-defg-hij"}]}},
    {"id": "evtoldboard", "status": "cancelled"}
  ]
}`

func TestListEventsSendsABoundedExpandedOrderedWindow(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, listedEventsPageJSON)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("list-window"), client.ListEvents(), calendarConnection, calendar.ListEventsInput{
		CalendarID: "team#ops@group.calendar.google.com", TimeMin: "2026-02-20T00:00:00-08:00", TimeMax: "2026-02-21T00:00:00-08:00",
		Query: "vendor", PageToken: "page-1", TimeZone: "America/Los_Angeles",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.ListEventsBranchListed, result.Branch)

	request := provider.request(t, 0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/calendars/team%23ops@group.calendar.google.com/events", request.path)
	require.Equal(t, "true", request.query.Get("singleEvents"))
	require.Equal(t, "startTime", request.query.Get("orderBy"))
	require.Equal(t, "false", request.query.Get("showDeleted"))
	require.Equal(t, "2026-02-20T00:00:00-08:00", request.query.Get("timeMin"))
	require.Equal(t, "2026-02-21T00:00:00-08:00", request.query.Get("timeMax"))
	require.Equal(t, "50", request.query.Get("maxResults"))
	require.Equal(t, "vendor", request.query.Get("q"))
	require.Equal(t, "page-1", request.query.Get("pageToken"))
	require.Equal(t, "America/Los_Angeles", request.query.Get("timeZone"))

	output := result.Value
	require.Equal(t, "2026-02-20T00:00:00-08:00", output.TimeMin)
	require.Equal(t, "2026-02-21T00:00:00-08:00", output.TimeMax)
	require.Equal(t, "America/Los_Angeles", output.TimeZone)
	require.Equal(t, "page-2", output.NextPageToken)
	require.Len(t, output.Events, 2, "the cancelled event is omitted")
}

func TestListEventsReturnsUnambiguousAllDayAndTimedEvents(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, listedEventsPageJSON)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("list-shapes"), client.ListEvents(), calendarConnection, calendar.ListEventsInput{
		CalendarID: "primary", TimeMin: "2026-02-20T08:00:00Z", TimeMax: "2026-02-21T08:00:00Z",
	})
	require.NoError(t, err)
	allDay, timed := result.Value.Events[0], result.Value.Events[1]

	require.True(t, allDay.IsAllDay)
	require.Equal(t, calendar.EventDateTime{Date: "2026-02-20"}, allDay.Start)
	require.Equal(t, calendar.EventDateTime{Date: "2026-02-21"}, allDay.End)
	require.Equal(t, "transparent", allDay.Transparency)
	require.Equal(t, "primary", allDay.CalendarID)

	// The description says 2:00 PM PST, but the instant is 11:00 in Los Angeles: the connector never reinterprets it.
	require.False(t, timed.IsAllDay)
	require.Equal(t, "Starts 2:00 PM PST", timed.Description)
	start, err := timed.Start.Instant(nil)
	require.NoError(t, err)
	require.Equal(t, "2026-02-20T19:00:00Z", start.UTC().Format(time.RFC3339))
	require.Equal(t, "America/Los_Angeles", timed.Start.TimeZone)
	require.Equal(t, "opaque", timed.Transparency, "Google omits transparency for busy events")
	require.Equal(t, "owner@example.com", timed.OrganizerEmail)
	require.Equal(t, []calendar.EventAttendee{
		{Email: "ben@example.com", DisplayName: "Ben", ResponseStatus: "accepted", IsOptional: true},
		{Email: "owner@example.com", ResponseStatus: "accepted", IsOrganizer: true, IsSelf: true},
	}, timed.Attendees)
	require.Equal(t, &calendar.EventConference{SolutionType: "hangoutsMeet", ConferenceID: "abc-defg-hij", JoinURL: "https://meet.google.com/abc-defg-hij"}, timed.Conference)

	// The calendar zone reported beside the page gives an all-day date its instant.
	location, err := time.LoadLocation(result.Value.TimeZone)
	require.NoError(t, err)
	allDayStart, err := allDay.Start.Instant(location)
	require.NoError(t, err)
	require.Equal(t, "2026-02-20T08:00:00Z", allDayStart.UTC().Format(time.RFC3339))
}

func TestListEventsRejectsUnboundedOrAmbiguousWindowsBeforeCallingGoogle(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	valid := calendar.ListEventsInput{CalendarID: "primary", TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z"}
	for _, test := range []struct {
		name    string
		change  func(*calendar.ListEventsInput)
		message string
	}{
		{name: "missing calendar", change: func(input *calendar.ListEventsInput) { input.CalendarID = "" }, message: "calendarId is required"},
		{name: "missing timeMax", change: func(input *calendar.ListEventsInput) { input.TimeMax = "" }, message: "timeMin and timeMax are both required"},
		{name: "naive timeMin", change: func(input *calendar.ListEventsInput) { input.TimeMin = "2026-02-20T00:00:00" }, message: "explicit offset"},
		{name: "reversed window", change: func(input *calendar.ListEventsInput) { input.TimeMax = "2026-02-19T00:00:00Z" }, message: "must be after timeMin"},
		{name: "window over 366 days", change: func(input *calendar.ListEventsInput) { input.TimeMax = "2027-02-22T00:00:00Z" }, message: "cannot exceed 366 days"},
		{name: "page too large", change: func(input *calendar.ListEventsInput) { input.PageSize = 251 }, message: "pageSize must be between 1 and 250"},
		{name: "negative page", change: func(input *calendar.ListEventsInput) { input.PageSize = -1 }, message: "pageSize must be between 1 and 250"},
		{name: "invalid response zone", change: func(input *calendar.ListEventsInput) { input.TimeZone = "PST" }, message: "IANA"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.change(&input)
			result, err := sdkgo.RunQuery(newCalendarDexContext("list-invalid"), client.ListEvents(), calendarConnection, input)
			require.NoError(t, err)
			require.Equal(t, calendar.ListEventsBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestListEventsHonorsAnExplicitPageSize(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"timeZone":"UTC","items":[]}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("list-page-size"), client.ListEvents(), calendarConnection, calendar.ListEventsInput{
		CalendarID: "primary", TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2027-02-21T00:00:00Z", PageSize: 250,
	})
	require.NoError(t, err)
	require.Equal(t, calendar.ListEventsBranchListed, result.Branch)
	require.Equal(t, "250", provider.request(t, 0).query.Get("maxResults"))
	require.Empty(t, result.Value.Events)
	require.NotNil(t, result.Value.Events)
	require.Empty(t, result.Value.NextPageToken)
}

func TestListEventsClassifiesMissingCalendarsAndInvalidPages(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing calendar", status: http.StatusNotFound, body: `{"error":{"errors":[{"reason":"notFound"}]}}`, branch: calendar.ListEventsBranchNotFound},
		{name: "read-only rejection", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"forbidden"}]}}`, branch: calendar.ListEventsBranchProviderRejected},
		{name: "not JSON", status: http.StatusOK, body: `<html>`, branch: calendar.ListEventsBranchInvalidResponse},
		{name: "event instant without an offset", status: http.StatusOK, body: `{"items":[{"id":"evt123456","status":"confirmed","start":{"dateTime":"2026-02-20T11:00:00"},"end":{"dateTime":"2026-02-20T12:00:00-08:00"}}]}`, branch: calendar.ListEventsBranchInvalidResponse},
		{name: "event mixing date and dateTime", status: http.StatusOK, body: `{"items":[{"id":"evt123456","status":"confirmed","start":{"date":"2026-02-20"},"end":{"dateTime":"2026-02-20T12:00:00-08:00"}}]}`, branch: calendar.ListEventsBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newCalendarDexContext("list-"+test.name), client.ListEvents(), calendarConnection, calendar.ListEventsInput{
				CalendarID: "primary", TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z",
			})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotNil(t, result.Failure)
		})
	}
}

func TestListEventsRetriesUnavailableGoogle(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	client := newCalendarClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newCalendarDexContext("list-unavailable"), client.ListEvents(), calendarConnection, calendar.ListEventsInput{
		CalendarID: "primary", TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z",
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}
