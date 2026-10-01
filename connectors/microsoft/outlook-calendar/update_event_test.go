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

func invitationInput() outlookcalendar.UpdateEventInput {
	attendees := []outlookcalendar.EventAttendeeInput{{Email: "priya@contoso.com"}}
	return outlookcalendar.UpdateEventInput{EventID: "event-1", Attendees: &attendees}
}

func eventWithAttendeeJSON(etag string) string {
	return strings.Replace(timedEventJSON("event-1", "Kickoff"), `"@odata.etag":"W/\"etag-1\""`, `"@odata.etag":"`+etag+`",`+
		`"attendees":[{"type":"required","status":{"response":"none"},"emailAddress":{"address":"Priya@contoso.com"}}]`, 1)
}

func TestUpdateEventPatchesOnlyRequestedFieldsWithIfMatch(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Kickoff"))
			return
		}
		writeJSON(t, response, http.StatusOK, eventWithAttendeeJSON(`W/\"etag-2\"`))
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newCalendarDexContext("update"), client.UpdateEvent(), calendarConnection, invitationInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.UpdateEventBranchUpdated, result.Branch, "%+v", result.Failure)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, `W/"etag-2"`, result.Value.Event.ETag)
	require.Equal(t, []string{http.MethodGet, http.MethodPatch}, provider.methods())
	patch := provider.request(t, 1)
	require.Equal(t, "/v1.0/me/events/event-1", patch.path)
	require.Equal(t, `W/"etag-1"`, patch.header.Get("If-Match"))
	require.Equal(t, map[string]any{"attendees": []any{
		map[string]any{"emailAddress": map[string]any{"address": "priya@contoso.com"}, "type": "required"},
	}}, decodeRequestBody(t, patch), "only the attendees are sent, so Graph updates only the changed attendees")
}

func TestUpdateEventSkipsAChangeTheEventAlreadyHolds(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, eventWithAttendeeJSON(`W/\"etag-2\"`))
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCalendarDexContext("update-applied"), client.UpdateEvent(), calendarConnection, invitationInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.UpdateEventBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied, "attendee addresses compare without case")
	require.Equal(t, []string{http.MethodGet}, provider.methods(), "a retried attempt sends no second PATCH or meeting update")
}

func TestUpdateEventComparesTimesByInstantAndBodiesAfterTextConversion(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, strings.Replace(timedEventJSON("event-1", "Kickoff"), `"subject"`,
			`"body":{"contentType":"text","content":"Agenda\r\nto follow.\r\n"},"subject"`, 1))
	})
	client := newCalendarClient(t, provider.URL)
	body := "Agenda\nto follow."
	start := outlookcalendar.EventDateTime{DateTime: "2026-02-24T12:00:00-05:00", TimeZone: "America/New_York"}
	end := outlookcalendar.EventDateTime{DateTime: "2026-02-24T13:00:00-05:00", TimeZone: "America/New_York"}
	result, err := sdkgo.RunMutation(newCalendarDexContext("update-instants"), client.UpdateEvent(), calendarConnection, outlookcalendar.UpdateEventInput{
		EventID: "event-1", Body: &body, Start: &start, End: &end,
	})
	require.NoError(t, err)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, start, result.Value.Event.Start, "the result is shown in the requested start's zone")
}

func TestUpdateEventSendsTimesAndTheAllDayFormTogether(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Kickoff"))
	})
	client := newCalendarClient(t, provider.URL)
	start, end := losAngeles("2026-02-24T10:00:00-08:00"), losAngeles("2026-02-24T11:00:00-08:00")
	_, err := sdkgo.RunMutation(newCalendarDexContext("update-move"), client.UpdateEvent(), calendarConnection, outlookcalendar.UpdateEventInput{
		EventID: "event-1", Start: &start, End: &end,
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"start":    map[string]any{"dateTime": "2026-02-24T10:00:00", "timeZone": "America/Los_Angeles"},
		"end":      map[string]any{"dateTime": "2026-02-24T11:00:00", "timeZone": "America/Los_Angeles"},
		"isAllDay": false,
	}, decodeRequestBody(t, provider.request(t, 1)))
}

func TestUpdateEventRetriesAConcurrentEditFromTheRead(t *testing.T) {
	for _, status := range []int{http.StatusPreconditionFailed, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Kickoff"))
					return
				}
				writeJSON(t, response, status, `{"error":{"code":"ErrorIrresolvableConflict"}}`)
			})
			client := newCalendarClient(t, provider.URL)
			_, err := sdkgo.RunMutation(newCalendarDexContext("update-conflict"), client.UpdateEvent(), calendarConnection, invitationInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureConflict, retry.Failure.Kind)
		})
	}
}

func TestUpdateEventReportsAMissingOrCancelledEventAsNotFound(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "missing", status: http.StatusNotFound, body: `{"error":{"code":"ErrorItemNotFound"}}`},
		{name: "cancelled", status: http.StatusOK, body: strings.Replace(timedEventJSON("event-1", "Kickoff"), `"subject"`, `"isCancelled":true,"subject"`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newCalendarDexContext("update-"+test.name), client.UpdateEvent(), calendarConnection, invitationInput())
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.UpdateEventBranchNotFound, result.Branch)
			require.Equal(t, []string{http.MethodGet}, provider.methods())
		})
	}
}

func TestUpdateEventRejectsAnIncompleteChangeWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	start := losAngeles("2026-02-24T10:00:00-08:00")
	blank, subject := " ", "Kickoff"
	for _, test := range []struct {
		name    string
		input   outlookcalendar.UpdateEventInput
		message string
	}{
		{name: "nothing to change", input: outlookcalendar.UpdateEventInput{EventID: "event-1"}, message: "set at least one"},
		{name: "start without end", input: outlookcalendar.UpdateEventInput{EventID: "event-1", Start: &start}, message: "together"},
		{name: "all-day without times", input: outlookcalendar.UpdateEventInput{EventID: "event-1", IsAllDay: true, Subject: &subject}, message: "isAllDay applies only"},
		{name: "blank subject", input: outlookcalendar.UpdateEventInput{EventID: "event-1", Subject: &blank}, message: "subject cannot be blank"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newCalendarDexContext("update-invalid-"+test.name), client.UpdateEvent(), calendarConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.UpdateEventBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}
