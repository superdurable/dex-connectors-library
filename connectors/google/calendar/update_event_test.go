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

const invitedEventJSON = `{"id":"event12345","status":"confirmed","summary":"Planning","etag":"\"2\"",
  "start":{"dateTime":"2026-02-24T17:00:00Z","timeZone":"America/Los_Angeles"},
  "end":{"dateTime":"2026-02-24T18:00:00Z","timeZone":"America/Los_Angeles"},
  "attendees":[{"email":"Priya@Meridian.example.com","responseStatus":"needsAction"}]}`

func TestUpdateEventPatchesOnlyRequestedFieldsGuardedByTheReadETag(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning"))
			return
		}
		writeJSON(t, response, http.StatusOK, invitedEventJSON)
	})
	client := newCalendarClient(t, provider.URL)
	attendees := []calendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}}

	result, err := sdkgo.RunMutation(newCalendarDexContext("update-invite"), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "event12345", Attendees: &attendees, SendUpdates: "all",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.UpdateEventBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "event12345", result.Receipt.ProviderObjectID)

	patch := provider.request(t, 1)
	require.Equal(t, http.MethodPatch, patch.method)
	require.Equal(t, "/calendars/primary/events/event12345", patch.path)
	require.Equal(t, `"3181161784712000"`, patch.ifMatch)
	require.Equal(t, "all", patch.query.Get("sendUpdates"))
	require.Equal(t, map[string]any{"attendees": []any{map[string]any{"email": "priya@meridian.example.com"}}}, decodeRequestBody(t, patch))
}

func TestUpdateEventSendsNoPatchWhenTheEventAlreadyHoldsTheValues(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, invitedEventJSON)
	})
	client := newCalendarClient(t, provider.URL)
	summary := "Planning"
	attendees := []calendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}}
	// Google echoes the same instants in UTC; equal instants and zones are the same boundary.
	start := calendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles"}
	end := calendar.EventDateTime{DateTime: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles"}

	result, err := sdkgo.RunMutation(newCalendarDexContext("update-applied"), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "event12345", Summary: &summary, Start: &start, End: &end, Attendees: &attendees, SendUpdates: "all",
	})
	require.NoError(t, err)
	require.Equal(t, calendar.UpdateEventBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, []string{http.MethodGet}, provider.methods(), "no second patch and no second guest notification")
}

func TestUpdateEventPatchesWhenOnlyTheDisplayZoneDiffers(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, invitedEventJSON)
	})
	client := newCalendarClient(t, provider.URL)
	start := calendar.EventDateTime{DateTime: "2026-02-24T12:00:00-05:00", TimeZone: "America/New_York"}
	end := calendar.EventDateTime{DateTime: "2026-02-24T13:00:00-05:00", TimeZone: "America/New_York"}
	result, err := sdkgo.RunMutation(newCalendarDexContext("update-zone"), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "event12345", Start: &start, End: &end,
	})
	require.NoError(t, err)
	require.Equal(t, calendar.UpdateEventBranchUpdated, result.Branch)
	require.Equal(t, []string{http.MethodGet, http.MethodPatch}, provider.methods())
}

func TestUpdateEventRetriesAConcurrentChangeAndThenFindsItsOwnPatch(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPatch:
			writeJSON(t, response, http.StatusPreconditionFailed, `{"error":{"errors":[{"reason":"conditionNotMet"}]}}`)
		case index == 0:
			writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning"))
		default:
			writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning v2"))
		}
	})
	client := newCalendarClient(t, provider.URL)
	summary := "Planning v2"
	input := calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345", Summary: &summary}

	_, err := sdkgo.RunMutation(newCalendarDexContext("update-precondition"), client.UpdateEvent(), calendarConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureConflict, retry.Failure.Kind)

	result, err := sdkgo.RunMutation(newCalendarDexContext("update-precondition"), client.UpdateEvent(), calendarConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendar.UpdateEventBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, []string{http.MethodGet, http.MethodPatch, http.MethodGet}, provider.methods())
}

func TestUpdateEventSwitchingToAllDayClearsTheTimedFields(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning"))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"event12345","status":"confirmed","start":{"date":"2026-02-24"},"end":{"date":"2026-02-25"}}`)
	})
	client := newCalendarClient(t, provider.URL)
	start, end := calendar.EventDateTime{Date: "2026-02-24"}, calendar.EventDateTime{Date: "2026-02-25"}

	result, err := sdkgo.RunMutation(newCalendarDexContext("update-all-day"), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "event12345", Start: &start, End: &end,
	})
	require.NoError(t, err)
	require.Equal(t, calendar.UpdateEventBranchUpdated, result.Branch)
	require.True(t, result.Value.Event.IsAllDay)
	require.Equal(t, map[string]any{
		"start": map[string]any{"date": "2026-02-24", "dateTime": nil, "timeZone": nil},
		"end":   map[string]any{"date": "2026-02-25", "dateTime": nil, "timeZone": nil},
	}, decodeRequestBody(t, provider.request(t, 1)))
}

func TestUpdateEventSelectsNotFoundForMissingOrCancelledEvents(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "missing", status: http.StatusNotFound, body: `{}`},
		{name: "gone", status: http.StatusGone, body: `{}`},
		{name: "cancelled", status: http.StatusOK, body: `{"id":"event12345","status":"cancelled"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			summary := "Planning"
			result, err := sdkgo.RunMutation(newCalendarDexContext("update-"+test.name), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
				CalendarID: "primary", EventID: "event12345", Summary: &summary,
			})
			require.NoError(t, err)
			require.Equal(t, calendar.UpdateEventBranchNotFound, result.Branch)
			require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
			require.Equal(t, []string{http.MethodGet}, provider.methods())
		})
	}
}

func TestUpdateEventRetriesALostPatchResponse(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, timedEventJSON("event12345", "Planning"))
			return
		}
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	client := newCalendarClient(t, provider.URL)
	summary := "Planning v2"
	_, err := sdkgo.RunMutation(newCalendarDexContext("update-lost"), client.UpdateEvent(), calendarConnection, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "event12345", Summary: &summary,
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestUpdateEventRejectsInvalidInputBeforeCallingGoogle(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	blank := " "
	start := calendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles"}
	naiveEnd := calendar.EventDateTime{DateTime: "2026-02-24T10:00:00", TimeZone: "America/Los_Angeles"}
	duplicates := []calendar.EventAttendeeInput{{Email: "a@example.com"}, {Email: "a@example.com"}}
	for _, test := range []struct {
		name    string
		input   calendar.UpdateEventInput
		message string
	}{
		{name: "no change", input: calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345"}, message: "set at least one"},
		{name: "missing event ID", input: calendar.UpdateEventInput{CalendarID: "primary", Summary: &blank}, message: "eventId is required"},
		{name: "blank summary", input: calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345", Summary: &blank}, message: "summary cannot be blank"},
		{name: "start without end", input: calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345", Start: &start}, message: "set start and end together"},
		{name: "naive end", input: calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345", Start: &start, End: &naiveEnd}, message: "explicit offset"},
		{name: "duplicate attendees", input: calendar.UpdateEventInput{CalendarID: "primary", EventID: "event12345", Attendees: &duplicates}, message: "duplicated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newCalendarDexContext("update-invalid"), client.UpdateEvent(), calendarConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, calendar.UpdateEventBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}
