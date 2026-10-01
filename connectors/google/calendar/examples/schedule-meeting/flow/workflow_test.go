// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulemeeting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func losAngelesBoundary(dateTime string) calendar.EventDateTime {
	return calendar.EventDateTime{DateTime: dateTime, TimeZone: "America/Los_Angeles"}
}

func validMeetingInput() Input {
	return Input{
		Summary: " Meridian kickoff ", Start: losAngelesBoundary("2026-02-24T14:00:00-08:00"),
		End: losAngelesBoundary("2026-02-24T15:00:00-08:00"), Attendees: []string{" priya@meridian.example.com "},
		RequestConference: true,
	}
}

func TestBuildMeetingRequestRecordsTheCalendarAndTrimsInput(t *testing.T) {
	request, err := buildMeetingRequest("team@group.calendar.google.com", validMeetingInput())
	require.NoError(t, err)
	require.Equal(t, MeetingRequest{
		CalendarID: "team@group.calendar.google.com", Summary: "Meridian kickoff",
		Start: losAngelesBoundary("2026-02-24T14:00:00-08:00"), End: losAngelesBoundary("2026-02-24T15:00:00-08:00"),
		Attendees: []string{"priya@meridian.example.com"}, RequestConference: true,
	}, request)
}

func TestBuildMeetingRequestRejectsAmbiguousOrIncompleteMeetings(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*Input)
		message string
	}{
		{name: "naive start", change: func(input *Input) { input.Start.DateTime = "2026-02-24T14:00:00" }, message: "explicit offset"},
		{name: "missing zone", change: func(input *Input) { input.End.TimeZone = "" }, message: "end.timeZone is required"},
		{name: "all-day meeting", change: func(input *Input) {
			input.Start, input.End = calendar.EventDateTime{Date: "2026-02-24"}, calendar.EventDateTime{Date: "2026-02-25"}
		}, message: "timed meetings"},
		{name: "no attendees", change: func(input *Input) { input.Attendees = nil }, message: "at least one guest"},
		{name: "display name attendee", change: func(input *Input) { input.Attendees = []string{"Priya <priya@meridian.example.com>"} }, message: "bare email"},
		{name: "blank summary", change: func(input *Input) { input.Summary = " " }, message: "summary is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := validMeetingInput()
			test.change(&input)
			_, err := buildMeetingRequest(DefaultCalendarID, input)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestOperationMappingsUseTheRecordedCalendarAndExactWindow(t *testing.T) {
	request, err := buildMeetingRequest("primary", validMeetingInput())
	require.NoError(t, err)
	require.Equal(t, calendar.QueryFreeBusyInput{
		CalendarIDs: []string{"primary"}, TimeMin: "2026-02-24T14:00:00-08:00", TimeMax: "2026-02-24T15:00:00-08:00",
		TimeZone: "America/Los_Angeles",
	}, MapToQueryFreeBusyInput(request))
	require.Equal(t, calendar.CreateEventInput{
		CalendarID: "primary", Summary: "Meridian kickoff", Start: request.Start, End: request.End,
		RequestConference: true, SendUpdates: "none",
	}, MapToCreateEventInput(request), "the hold has no guests, so nobody is notified before confirmation")
	require.Equal(t, calendar.ListEventsInput{
		CalendarID: "primary", TimeMin: "2026-02-24T14:00:00-08:00", TimeMax: "2026-02-24T15:00:00-08:00",
		PageSize: 250, TimeZone: "America/Los_Angeles",
	}, MapToListEventsInput(request))
	require.Equal(t, calendar.GetEventInput{CalendarID: "primary", EventID: "holdevent01"}, MapToGetEventInput(calendar.CreateEventResult{
		Value: calendar.CreateEventOutput{Event: calendar.Event{ID: "holdevent01", CalendarID: "primary"}},
	}))
	attendees := []calendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}}
	require.Equal(t, calendar.UpdateEventInput{
		CalendarID: "primary", EventID: "holdevent01", Attendees: &attendees, SendUpdates: "all",
	}, MapToUpdateEventInput(MeetingInvitation{CalendarID: "primary", EventID: "holdevent01", Attendees: request.Attendees}))
}

func TestFindConflictingEventIDsComparesInstantsNotText(t *testing.T) {
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	windowStart := time.Date(2026, time.February, 20, 22, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	events := []calendar.Event{
		{ID: "holdevent01", Start: losAngelesBoundary("2026-02-20T14:00:00-08:00"), End: losAngelesBoundary("2026-02-20T15:00:00-08:00")},
		{ID: "boardprep01", Status: "confirmed", Transparency: "opaque", Start: calendar.EventDateTime{DateTime: "2026-02-20T22:30:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-20T23:30:00Z"}},
		// The description says 2:00 PM PST, but the event is 11:00 PST: no overlap.
		{ID: "tztrap0001", Status: "confirmed", Transparency: "opaque", Description: "Starts 2:00 PM PST", Start: calendar.EventDateTime{DateTime: "2026-02-20T19:00:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-20T20:00:00Z"}},
		{ID: "focusblock1", Status: "confirmed", Transparency: "transparent", Start: calendar.EventDateTime{DateTime: "2026-02-20T22:00:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-20T23:00:00Z"}},
		{ID: "cancelled01", Status: "cancelled", Transparency: "opaque", Start: calendar.EventDateTime{DateTime: "2026-02-20T22:00:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-20T23:00:00Z"}},
		{ID: "adjacent001", Status: "confirmed", Transparency: "opaque", Start: calendar.EventDateTime{DateTime: "2026-02-20T23:00:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-20T23:30:00Z"}},
		// An opaque all-day event on the 20th in Los Angeles covers 08:00Z on the 20th to 08:00Z on the 21st.
		{ID: "offsiteday1", Status: "confirmed", Transparency: "opaque", Start: calendar.EventDateTime{Date: "2026-02-20"}, End: calendar.EventDateTime{Date: "2026-02-21"}},
		// The same all-day event on the 21st starts after the window in Los Angeles.
		{ID: "offsiteday2", Status: "confirmed", Transparency: "opaque", Start: calendar.EventDateTime{Date: "2026-02-21"}, End: calendar.EventDateTime{Date: "2026-02-22"}},
	}
	conflicting, err := FindConflictingEventIDs(events, "holdevent01", windowStart, windowEnd, losAngeles)
	require.NoError(t, err)
	require.Equal(t, []string{"boardprep01", "offsiteday1"}, conflicting)

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	conflicting, err = FindConflictingEventIDs(events[7:], "holdevent01", windowStart, windowEnd, tokyo)
	require.NoError(t, err)
	require.Equal(t, []string{"offsiteday2"}, conflicting, "in Tokyo the 21st begins at 15:00Z on the 20th")
}

func TestVerifyHoldMatchesRequestUsesInstants(t *testing.T) {
	request, err := buildMeetingRequest("primary", validMeetingInput())
	require.NoError(t, err)
	// Google may echo the same instants in another offset.
	hold := calendar.Event{ID: "holdevent01", Status: "confirmed",
		Start: calendar.EventDateTime{DateTime: "2026-02-24T22:00:00Z"}, End: calendar.EventDateTime{DateTime: "2026-02-24T18:00:00-05:00"}}
	require.NoError(t, verifyHoldMatchesRequest(hold, request))
	hold.End.DateTime = "2026-02-24T16:00:00-08:00"
	require.ErrorContains(t, verifyHoldMatchesRequest(hold, request), "end is 2026-02-25T00:00:00Z, not the requested 2026-02-24T23:00:00Z")
}

func TestFlowIdentitiesAndDefaultCalendar(t *testing.T) {
	flow := NewFlow(calendar.Connection{}, CalendarSelection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, DefaultCalendarID, flow.calendarID)
	require.Equal(t, "team@group.calendar.google.com", NewFlow(calendar.Connection{}, CalendarSelection{CalendarID: " team@group.calendar.google.com "}).calendarID)
	require.Equal(t, recordMeetingRequestStepType, dex.GetFinalStepType[Input](recordMeetingRequest{}))
	require.Equal(t, completeMeetingBookingStepType, dex.GetFinalStepType[calendar.UpdateEventResult](completeMeetingBooking{}))
	wait, err := recordMeetingRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "google-calendar", ConnectionName: ConnectionName, OperationID: "createEvent",
		FlowType: FlowType, StepType: placeMeetingHoldStepType,
	}, CalendarSelectionConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := calendar.New(calendar.Config{}, sdkgo.StaticCredentialProvider[calendar.Credentials]{})
	require.NoError(t, err)
	connection, err := calendar.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, CalendarSelection{})})
	require.NoError(t, err)

	otherConnection, err := calendar.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, CalendarSelection{})}) })
}
