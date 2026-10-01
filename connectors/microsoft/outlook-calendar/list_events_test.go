// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const nextPageLink = "https://graph.microsoft.com/v1.0/me/calendars/team-calendar/calendarView?startDateTime=2026-02-24T00%3A00%3A00-08%3A00&endDateTime=2026-02-25T00%3A00%3A00-08%3A00&%24top=2&%24skip=2"

func TestListEventsRequestsOneBoundedCalendarViewPageInUTC(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[`+
			`{"id":"late","subject":"Review","showAs":"tentative","start":{"dateTime":"2026-02-24T20:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T21:00:00.0000000","timeZone":"UTC"}},`+
			`{"id":"early","subject":"Standup","start":{"dateTime":"2026-02-24T17:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T17:15:00.0000000","timeZone":"UTC"},"isCancelled":true}`+
			`],"@odata.nextLink":"`+nextPageLink+`"}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("list"), client.ListEvents(), calendarConnection, outlookcalendar.ListEventsInput{
		CalendarID: "team-calendar", TimeMin: "2026-02-24T00:00:00-08:00", TimeMax: "2026-02-25T00:00:00-08:00",
		PageSize: 2, TimeZone: "America/Los_Angeles",
	})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.ListEventsBranchListed, result.Branch)
	request := provider.request(t, 0)
	require.Equal(t, "/v1.0/me/calendars/team-calendar/calendarView", request.path)
	require.Equal(t, "2026-02-24T00:00:00-08:00", request.query.Get("startDateTime"), "Graph honors the offset in the value")
	require.Equal(t, "2026-02-25T00:00:00-08:00", request.query.Get("endDateTime"))
	require.Equal(t, "2", request.query.Get("$top"))
	require.NotContains(t, request.query.Get("$select"), "body,", "a page carries bodyPreview, not the full body")
	require.Contains(t, request.query.Get("$select"), "bodyPreview")
	require.Equal(t, []string{`outlook.timezone="UTC"`, `outlook.body-content-type="text"`}, request.header.Values("Prefer"))

	output := result.Value
	require.Equal(t, "America/Los_Angeles", output.TimeZone)
	require.Equal(t, []string{"early", "late"}, []string{output.Events[0].ID, output.Events[1].ID}, "events are ordered by start within the page")
	require.Equal(t, losAngeles("2026-02-24T09:00:00-08:00"), output.Events[0].Start)
	require.True(t, output.Events[0].IsCancelled)
	require.Equal(t, "unknown", output.Events[0].ShowAs, "an omitted showAs is reported as unknown, never free")
	require.Equal(t, "tentative", output.Events[1].ShowAs)
	require.Equal(t, "team-calendar", output.Events[1].CalendarID)
	require.Equal(t, nextPageLink, output.NextPageToken)
}

func TestListEventsFollowsTheNextLinkUnchanged(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[]}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("next-page"), client.ListEvents(), calendarConnection, outlookcalendar.ListEventsInput{
		CalendarID: "team-calendar", TimeMin: "2026-02-24T00:00:00-08:00", TimeMax: "2026-02-25T00:00:00-08:00", PageToken: nextPageLink,
	})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.ListEventsBranchListed, result.Branch)
	require.Empty(t, result.Value.Events)
	require.Empty(t, result.Value.NextPageToken)
	request := provider.request(t, 0)
	require.Equal(t, "/v1.0/me/calendars/team-calendar/calendarView", request.path)
	require.Equal(t, "2", request.query.Get("$skip"), "the nextLink is used whole, as Graph requires")
}

func TestListEventsUsesTheDefaultCalendarWhenNoneIsPicked(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[]}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("default-calendar"), client.ListEvents(), calendarConnection, outlookcalendar.ListEventsInput{
		TimeMin: "2026-02-24T08:00:00Z", TimeMax: "2026-02-25T08:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.ListEventsBranchListed, result.Branch)
	require.Equal(t, "UTC", result.Value.TimeZone)
	require.Equal(t, "/v1.0/me/calendar/calendarView", provider.request(t, 0).path)
	require.Equal(t, "50", provider.request(t, 0).query.Get("$top"))
}

func TestListEventsRejectsUnboundedOrAmbiguousInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	for _, test := range []struct {
		name    string
		input   outlookcalendar.ListEventsInput
		message string
	}{
		{name: "naive window", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00", TimeMax: "2026-02-25T00:00:00Z"}, message: "explicit offset"},
		{name: "missing window", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00Z"}, message: "both required"},
		{name: "window too long", input: outlookcalendar.ListEventsInput{TimeMin: "2026-01-01T00:00:00Z", TimeMax: "2027-01-03T00:00:00Z"}, message: "366 days"},
		{name: "page too large", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z", PageSize: 251}, message: "pageSize"},
		{name: "foreign page token", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z", PageToken: "https://graph.example.com/v1.0/me/calendarView"}, message: "https://graph.microsoft.com"},
		{name: "page token for another resource", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z", PageToken: "https://graph.microsoft.com/v1.0/me/messages"}, message: "calendarView"},
		{name: "unknown display zone", input: outlookcalendar.ListEventsInput{TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z", TimeZone: "Pacific Standard Time"}, message: "IANA"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newCalendarDexContext("invalid-"+test.name), client.ListEvents(), calendarConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.ListEventsBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestListEventsMapsAMissingCalendarAndAnUnusablePage(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing calendar", status: http.StatusNotFound, body: `{"error":{"code":"ErrorItemNotFound"}}`, branch: outlookcalendar.ListEventsBranchNotFound},
		{name: "rejected", status: http.StatusBadRequest, body: `{"error":{"code":"ErrorInvalidIdMalformed"}}`, branch: outlookcalendar.ListEventsBranchProviderRejected},
		{name: "array instead of object", status: http.StatusOK, body: `[]`, branch: outlookcalendar.ListEventsBranchInvalidResponse},
		{name: "event in a Windows zone", status: http.StatusOK, body: `{"value":[{"id":"e","start":{"dateTime":"2026-02-24T09:00:00","timeZone":"Pacific Standard Time"},"end":{"dateTime":"2026-02-24T10:00:00","timeZone":"Pacific Standard Time"}}]}`, branch: outlookcalendar.ListEventsBranchInvalidResponse},
		{name: "event without an end", status: http.StatusOK, body: `{"value":[{"id":"e","start":{"dateTime":"2026-02-24T09:00:00","timeZone":"UTC"}}]}`, branch: outlookcalendar.ListEventsBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newCalendarDexContext("map-"+strings.ReplaceAll(test.name, " ", "-")), client.ListEvents(), calendarConnection, outlookcalendar.ListEventsInput{
				TimeMin: "2026-02-24T00:00:00Z", TimeMax: "2026-02-25T00:00:00Z",
			})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}
