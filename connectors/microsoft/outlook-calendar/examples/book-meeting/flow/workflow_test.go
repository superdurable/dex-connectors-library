// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookmeeting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func losAngelesBoundary(dateTime string) outlookcalendar.EventDateTime {
	return outlookcalendar.EventDateTime{DateTime: dateTime, TimeZone: "America/Los_Angeles"}
}

func validMeetingInput() Input {
	return Input{
		Subject: " Meridian kickoff ", Start: losAngelesBoundary("2026-02-24T14:00:00-08:00"),
		End: losAngelesBoundary("2026-02-24T15:00:00-08:00"), Attendees: []string{" priya@meridian.example.com "},
		RequestOnlineMeeting: true,
	}
}

func TestBuildMeetingRequestRecordsTheCalendarAndTrimsInput(t *testing.T) {
	request, err := buildMeetingRequest("team-calendar", validMeetingInput())
	require.NoError(t, err)
	require.Equal(t, MeetingRequest{
		CalendarID: "team-calendar", Subject: "Meridian kickoff",
		Start: losAngelesBoundary("2026-02-24T14:00:00-08:00"), End: losAngelesBoundary("2026-02-24T15:00:00-08:00"),
		Attendees: []string{"priya@meridian.example.com"}, RequestOnlineMeeting: true,
	}, request)
}

func TestBuildMeetingRequestRejectsAmbiguousOrIncompleteMeetings(t *testing.T) {
	tooMany := make([]string, 21)
	for index := range tooMany {
		tooMany[index] = "person" + string(rune('a'+index)) + "@example.com"
	}
	for _, test := range []struct {
		name    string
		change  func(*Input)
		message string
	}{
		{name: "naive start", change: func(input *Input) { input.Start.DateTime = "2026-02-24T14:00:00" }, message: "explicit offset"},
		{name: "missing zone", change: func(input *Input) { input.End.TimeZone = "" }, message: "end.timeZone is required"},
		{name: "no attendees", change: func(input *Input) { input.Attendees = nil }, message: "1 to 20"},
		{name: "too many attendees", change: func(input *Input) { input.Attendees = tooMany }, message: "1 to 20"},
		{name: "repeated attendee", change: func(input *Input) {
			input.Attendees = []string{"priya@meridian.example.com", "PRIYA@meridian.example.com"}
		}, message: "listed twice"},
		{name: "display name attendee", change: func(input *Input) { input.Attendees = []string{"Priya <priya@meridian.example.com>"} }, message: "bare email"},
		{name: "blank subject", change: func(input *Input) { input.Subject = " " }, message: "subject is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := validMeetingInput()
			test.change(&input)
			_, err := buildMeetingRequest("", input)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestOperationMappingsUseTheRecordedCalendarAndExactWindow(t *testing.T) {
	request, err := buildMeetingRequest("", validMeetingInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.QueryFreeBusyInput{
		Schedules: []string{"priya@meridian.example.com"}, TimeMin: "2026-02-24T14:00:00-08:00", TimeMax: "2026-02-24T15:00:00-08:00",
		TimeZone: "America/Los_Angeles",
	}, MapToQueryFreeBusyInput(request))
	require.Equal(t, outlookcalendar.CreateEventInput{
		Subject: "Meridian kickoff", Start: request.Start, End: request.End, RequestOnlineMeeting: true,
	}, MapToCreateEventInput(request), "the hold has no attendees, so Outlook sends no invitation before confirmation")
	require.Equal(t, outlookcalendar.ListEventsInput{
		TimeMin: "2026-02-24T14:00:00-08:00", TimeMax: "2026-02-24T15:00:00-08:00", PageSize: 250, TimeZone: "America/Los_Angeles",
	}, MapToListEventsInput(request))
	require.Equal(t, outlookcalendar.GetEventInput{EventID: "hold-1", TimeZone: "America/Los_Angeles"}, MapToGetEventInput(outlookcalendar.CreateEventResult{
		Value: outlookcalendar.CreateEventOutput{Event: outlookcalendar.Event{ID: "hold-1", Start: request.Start}},
	}))
	attendees := []outlookcalendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}}
	require.Equal(t, outlookcalendar.UpdateEventInput{EventID: "hold-1", Attendees: &attendees, TimeZone: "America/Los_Angeles"},
		MapToUpdateEventInput(MeetingInvitation{EventID: "hold-1", Attendees: request.Attendees, TimeZone: "America/Los_Angeles"}))
}

func TestFindConflictingEventIDsComparesInstantsNotText(t *testing.T) {
	windowStart := time.Date(2026, time.February, 20, 22, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	utc := func(dateTime string) outlookcalendar.EventDateTime {
		return outlookcalendar.EventDateTime{DateTime: dateTime, TimeZone: "UTC"}
	}
	events := []outlookcalendar.Event{
		{ID: "hold-1", ShowAs: "busy", Start: losAngelesBoundary("2026-02-20T14:00:00-08:00"), End: losAngelesBoundary("2026-02-20T15:00:00-08:00")},
		{ID: "board-prep", ShowAs: "busy", Start: utc("2026-02-20T22:30:00Z"), End: utc("2026-02-20T23:30:00Z")},
		// The body says 2:00 PM PST, but the event is 11:00 PST: no overlap.
		{ID: "timezone-trap", ShowAs: "busy", BodyPreview: "Starts 2:00 PM PST", Start: utc("2026-02-20T19:00:00Z"), End: utc("2026-02-20T20:00:00Z")},
		{ID: "focus", ShowAs: "free", Start: utc("2026-02-20T22:00:00Z"), End: utc("2026-02-20T23:00:00Z")},
		{ID: "remote-day", ShowAs: "workingElsewhere", Start: utc("2026-02-20T08:00:00Z"), End: utc("2026-02-21T08:00:00Z")},
		{ID: "cancelled", ShowAs: "busy", IsCancelled: true, Start: utc("2026-02-20T22:00:00Z"), End: utc("2026-02-20T23:00:00Z")},
		{ID: "adjacent", ShowAs: "busy", Start: utc("2026-02-20T23:00:00Z"), End: utc("2026-02-20T23:30:00Z")},
		{ID: "unknown-status", ShowAs: "unknown", Start: utc("2026-02-20T22:45:00Z"), End: utc("2026-02-20T23:15:00Z")},
		// An all-day out-of-office in Los Angeles covers 08:00Z on the 20th to 08:00Z on the 21st.
		{ID: "offsite-day", ShowAs: "oof", IsAllDay: true, Start: losAngelesBoundary("2026-02-20T00:00:00-08:00"), End: losAngelesBoundary("2026-02-21T00:00:00-08:00")},
	}
	conflicting, err := FindConflictingEventIDs(events, "hold-1", windowStart, windowEnd)
	require.NoError(t, err)
	require.Equal(t, []string{"board-prep", "unknown-status", "offsite-day"}, conflicting)
}

func TestVerifyHoldMatchesRequestUsesInstants(t *testing.T) {
	request, err := buildMeetingRequest("", validMeetingInput())
	require.NoError(t, err)
	hold := outlookcalendar.Event{ID: "hold-1",
		Start: outlookcalendar.EventDateTime{DateTime: "2026-02-24T22:00:00Z", TimeZone: "UTC"},
		End:   outlookcalendar.EventDateTime{DateTime: "2026-02-24T18:00:00-05:00", TimeZone: "America/New_York"}}
	require.NoError(t, verifyHoldMatchesRequest(hold, request))
	hold.End.DateTime = "2026-02-24T16:00:00-08:00"
	require.ErrorContains(t, verifyHoldMatchesRequest(hold, request), "end is 2026-02-25T00:00:00Z, not the requested 2026-02-24T23:00:00Z")
}

func TestFlowIdentitiesAndDefaultCalendar(t *testing.T) {
	flow := NewFlow(outlookcalendar.Connection{}, CalendarSelection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Empty(t, flow.calendarID, "a blank selection uses the mailbox's default calendar")
	require.Equal(t, "team-calendar", NewFlow(outlookcalendar.Connection{}, CalendarSelection{CalendarID: " team-calendar "}).calendarID)
	require.Equal(t, recordMeetingRequestStepType, dex.GetFinalStepType[Input](recordMeetingRequest{}))
	require.Equal(t, completeMeetingBookingStepType, dex.GetFinalStepType[outlookcalendar.UpdateEventResult](completeMeetingBooking{}))
	wait, err := recordMeetingRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "outlook-calendar", ConnectionName: ConnectionName, OperationID: "createEvent",
		FlowType: FlowType, StepType: placeMeetingHoldStepType,
	}, CalendarSelectionConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := outlookcalendar.New(outlookcalendar.Config{}, sdkgo.StaticCredentialProvider[outlookcalendar.Credentials]{})
	require.NoError(t, err)
	connection, err := outlookcalendar.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, CalendarSelection{})})
	require.NoError(t, err)

	otherConnection, err := outlookcalendar.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, CalendarSelection{})}) })
}
