// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetEventReturnsTheEventWithExplicitOffsets(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"@odata.etag":"W/\"etag-1\"","id":"event-1","subject":"Kickoff","showAs":"busy",`+
			`"body":{"contentType":"text","content":"Agenda"},"location":{"displayName":"Room 4"},"isOnlineMeeting":true,`+
			`"onlineMeetingProvider":"teamsForBusiness","onlineMeeting":{"joinUrl":"https://teams.microsoft.com/l/meetup-join/1"},`+
			`"organizer":{"emailAddress":{"address":"owner@contoso.com"}},"transactionId":"tx-1",`+
			`"attendees":[{"type":"required","status":{"response":"accepted"},"emailAddress":{"name":"Priya","address":"priya@contoso.com"}}],`+
			`"start":{"dateTime":"2026-02-24T17:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T18:00:00.0000000","timeZone":"UTC"},`+
			`"originalStartTimeZone":"Pacific Standard Time"}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("get"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{
		EventID: "event-1", TimeZone: "America/Los_Angeles",
	})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchFound, result.Branch)
	require.Equal(t, outlookcalendar.Event{
		ID: "event-1", Subject: "Kickoff", Body: "Agenda", Location: "Room 4",
		Start: losAngeles("2026-02-24T09:00:00-08:00"), End: losAngeles("2026-02-24T10:00:00-08:00"),
		OriginalStartTimeZone: "Pacific Standard Time", ShowAs: "busy",
		Attendees:      []outlookcalendar.EventAttendee{{Email: "priya@contoso.com", Name: "Priya", Type: "required", ResponseStatus: "accepted"}},
		OrganizerEmail: "owner@contoso.com",
		OnlineMeeting:  &outlookcalendar.EventOnlineMeeting{Provider: "teamsForBusiness", JoinURL: "https://teams.microsoft.com/l/meetup-join/1"},
		ETag:           `W/"etag-1"`, TransactionID: "tx-1",
	}, result.Value)
	require.Equal(t, "event-1", result.Receipt.ProviderObjectID)
}

func TestGetEventMapsMissingAndInvalidEvents(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing", status: http.StatusNotFound, body: `{"error":{"code":"ErrorItemNotFound"}}`, branch: outlookcalendar.GetEventBranchNotFound},
		{name: "no ID", status: http.StatusOK, body: `{"start":{"dateTime":"2026-02-24T17:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T18:00:00","timeZone":"UTC"}}`, branch: outlookcalendar.GetEventBranchInvalidResponse},
		{name: "ends before it starts", status: http.StatusOK, body: `{"id":"e","start":{"dateTime":"2026-02-24T18:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T17:00:00","timeZone":"UTC"}}`, branch: outlookcalendar.GetEventBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newCalendarDexContext("get-"+test.name), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: "e"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestGetEventRequiresAnEventIDWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("get-blank"), client.GetEvent(), calendarConnection, outlookcalendar.GetEventInput{EventID: " "})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchDefect, result.Branch)
	require.Zero(t, provider.requestCount())
}
