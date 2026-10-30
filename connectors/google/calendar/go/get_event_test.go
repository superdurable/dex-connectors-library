// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetEventReadsOneEventInTheRequestedZone(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("abc123_20260224T170000Z", "Weekly sync"))
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("get-instance"), client.GetEvent(), calendarConnection, calendar.GetEventInput{
		CalendarID: "owner@example.com", EventID: "abc123_20260224T170000Z", TimeZone: "Europe/Zurich",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchFound, result.Branch)
	require.Equal(t, "abc123_20260224T170000Z", result.Value.ID)
	require.Equal(t, "owner@example.com", result.Value.CalendarID)
	require.Equal(t, `"3181161784712000"`, result.Value.ETag)
	require.Equal(t, "abc123_20260224T170000Z", result.Receipt.ProviderObjectID)
	require.Equal(t, "google-request", result.Receipt.ProviderRequestID)
	request := provider.request(t, 0)
	require.Equal(t, "/calendars/owner@example.com/events/abc123_20260224T170000Z", request.path)
	require.Equal(t, "Europe/Zurich", request.query.Get("timeZone"))
}

func TestGetEventReportsDeletedEventsAsFoundWithTheirStatus(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"event12345","status":"cancelled"}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("get-cancelled"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchFound, result.Branch)
	require.Equal(t, "cancelled", result.Value.Status)
}

func TestGetEventSelectsNotFoundAndDefectBranches(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"error":{"errors":[{"reason":"notFound"}]}}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("get-missing"), client.GetEvent(), calendarConnection, calendar.GetEventInput{CalendarID: "primary", EventID: "event12345"})
	require.NoError(t, err)
	require.Equal(t, calendar.GetEventBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)

	for _, input := range []calendar.GetEventInput{
		{CalendarID: "primary"},
		{EventID: "event12345"},
		{CalendarID: "primary", EventID: "event12345", TimeZone: "Mars/Olympus_Mons"},
	} {
		result, err := sdkgo.RunQuery(newCalendarDexContext("get-invalid"), client.GetEvent(), calendarConnection, input)
		require.NoError(t, err)
		require.Equal(t, calendar.GetEventBranchDefect, result.Branch)
	}
	require.Equal(t, 1, provider.requestCount())
}
