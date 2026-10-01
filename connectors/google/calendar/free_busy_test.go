// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestQueryFreeBusyReturnsBusyIntervalsInRequestOrder(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"kind":"calendar#freeBusy","timeMin":"2026-02-20T08:00:00.000Z","timeMax":"2026-02-21T08:00:00.000Z",
		  "calendars":{"Ben@Example.com":{"busy":[]},
		               "primary":{"busy":[{"start":"2026-02-20T14:00:00-08:00","end":"2026-02-20T15:00:00-08:00"}]}}}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy"), client.QueryFreeBusy(), calendarConnection, calendar.QueryFreeBusyInput{
		CalendarIDs: []string{"primary", "ben@example.com"}, TimeMin: "2026-02-20T00:00:00-08:00", TimeMax: "2026-02-21T00:00:00-08:00",
		TimeZone: "America/Los_Angeles",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.QueryFreeBusyBranchQueried, result.Branch)
	require.Equal(t, calendar.QueryFreeBusyOutput{
		TimeMin: "2026-02-20T08:00:00.000Z", TimeMax: "2026-02-21T08:00:00.000Z", TimeZone: "America/Los_Angeles",
		Calendars: []calendar.CalendarFreeBusy{
			{CalendarID: "primary", Busy: []calendar.BusyInterval{{Start: "2026-02-20T14:00:00-08:00", End: "2026-02-20T15:00:00-08:00"}}},
			{CalendarID: "ben@example.com", Busy: []calendar.BusyInterval{}},
		},
	}, result.Value)

	request := provider.request(t, 0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/freeBusy", request.path)
	require.Equal(t, map[string]any{
		"timeMin": "2026-02-20T00:00:00-08:00", "timeMax": "2026-02-21T00:00:00-08:00", "timeZone": "America/Los_Angeles",
		"items": []any{map[string]any{"id": "primary"}, map[string]any{"id": "ben@example.com"}},
	}, decodeRequestBody(t, request))
}

func TestQueryFreeBusyNeverReportsAnErroredCalendarAsFree(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"calendars":{"primary":{"busy":[]},
		  "stranger@example.com":{"busy":[],"errors":[{"domain":"global","reason":"notFound"}]}}}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-incomplete"), client.QueryFreeBusy(), calendarConnection, calendar.QueryFreeBusyInput{
		CalendarIDs: []string{"primary", "stranger@example.com"}, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.QueryFreeBusyBranchIncomplete, result.Branch)
	require.Equal(t, "UTC", result.Value.TimeZone)
	require.Equal(t, []calendar.FreeBusyError{{Domain: "global", Reason: "notFound"}}, result.Value.Calendars[1].Errors)
	require.NotNil(t, result.Failure)
}

func TestQueryFreeBusyRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "requested calendar missing", body: `{"calendars":{}}`},
		{name: "busy start without offset", body: `{"calendars":{"primary":{"busy":[{"start":"2026-02-20T14:00:00","end":"2026-02-20T15:00:00Z"}]}}}`},
		{name: "busy interval reversed", body: `{"calendars":{"primary":{"busy":[{"start":"2026-02-20T15:00:00Z","end":"2026-02-20T14:00:00Z"}]}}}`},
		{name: "not JSON", body: `[`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-invalid"), client.QueryFreeBusy(), calendarConnection, calendar.QueryFreeBusyInput{
				CalendarIDs: []string{"primary"}, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z",
			})
			require.NoError(t, err)
			require.Equal(t, calendar.QueryFreeBusyBranchInvalidResponse, result.Branch)
		})
	}
}

func TestQueryFreeBusyRejectsInvalidInputBeforeCallingGoogle(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	tooMany := make([]string, 51)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("person%d@example.com", index)
	}
	for _, test := range []struct {
		name    string
		input   calendar.QueryFreeBusyInput
		message string
	}{
		{name: "no calendars", input: calendar.QueryFreeBusyInput{TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z"}, message: "1 to 50"},
		{name: "too many calendars", input: calendar.QueryFreeBusyInput{CalendarIDs: tooMany, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z"}, message: "1 to 50"},
		{name: "duplicate calendar", input: calendar.QueryFreeBusyInput{CalendarIDs: []string{"primary", "primary"}, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z"}, message: "duplicated"},
		{name: "naive window", input: calendar.QueryFreeBusyInput{CalendarIDs: []string{"primary"}, TimeMin: "2026-02-20T00:00:00", TimeMax: "2026-02-21T00:00:00Z"}, message: "explicit offset"},
		{name: "empty window", input: calendar.QueryFreeBusyInput{CalendarIDs: []string{"primary"}, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-19T16:00:00-08:00"}, message: "must be after timeMin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-defect"), client.QueryFreeBusy(), calendarConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, calendar.QueryFreeBusyBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestQueryFreeBusyClassifiesProviderRejection(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"errors":[{"reason":"timeRangeEmpty"}]}}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-rejected"), client.QueryFreeBusy(), calendarConnection, calendar.QueryFreeBusyInput{
		CalendarIDs: []string{"primary"}, TimeMin: "2026-02-20T00:00:00Z", TimeMax: "2026-02-21T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.QueryFreeBusyBranchProviderRejected, result.Branch)
	require.Equal(t, "provider rejected the request with HTTP 400 (timeRangeEmpty)", result.Failure.Message)
}
