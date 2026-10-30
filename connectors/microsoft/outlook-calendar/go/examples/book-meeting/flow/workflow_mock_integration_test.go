//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookmeeting

import (
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/go/outlookcalendarmock"
)

func TestBookMeetingUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := outlookcalendarmock.New(t, ConnectionName)
	flow, harness := newBookMeetingHarness(t, mock.Connection(), CalendarSelection{CalendarID: "team-calendar", CalendarName: "Ops Team"})
	ctx := integrationContext(t)
	input := integrationMeetingInput()
	flowID := startBookMeeting(t, ctx, harness.client, flow, "mock", input)
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)
	require.Equal(t, MeetingScheduled, outcome.Status)
	require.Equal(t, "team-calendar", outcome.CalendarID)
	require.Equal(t, "hold-1", outcome.Event.ID)
	require.Empty(t, outcome.Event.CalendarID)
	require.True(t, outcome.Event.IsOrganizer)
	require.NotNil(t, outcome.Event.OnlineMeeting)
	require.Equal(t, "teamsForBusiness", outcome.Event.OnlineMeeting.Provider)
	require.Equal(t, input.Attendees, attendeeEmails(outcome.Event.Attendees))
	require.Empty(t, outcome.ConflictingEventIDs)

	freeBusy := mock.QueryFreeBusy().Calls()
	require.Len(t, freeBusy, 1)
	require.Equal(t, outlookcalendar.QueryFreeBusyInput{Schedules: []string{"priya@meridian.example.com", "alice@example.com"}, TimeMin: "2026-02-20T14:00:00-08:00", TimeMax: "2026-02-20T15:00:00-08:00", TimeZone: "America/Los_Angeles"}, freeBusy[0].Input)
	creates := mock.CreateEvent().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, outlookcalendar.CreateEventInput{CalendarID: "team-calendar", Subject: "Meridian kickoff", Body: "Agenda to follow.", Start: outlookcalendar.EventDateTime{DateTime: "2026-02-20T14:00:00-08:00", TimeZone: "America/Los_Angeles"}, End: outlookcalendar.EventDateTime{DateTime: "2026-02-20T15:00:00-08:00", TimeZone: "America/Los_Angeles"}, RequestOnlineMeeting: true}, creates[0].Input)
	reads := mock.GetEvent().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, outlookcalendar.GetEventInput{EventID: "hold-1", TimeZone: "America/Los_Angeles"}, reads[0].Input)
	listings := mock.ListEvents().Calls()
	require.Len(t, listings, 1)
	require.Equal(t, outlookcalendar.ListEventsInput{CalendarID: "team-calendar", TimeMin: "2026-02-20T14:00:00-08:00", TimeMax: "2026-02-20T15:00:00-08:00", PageSize: 250, TimeZone: "America/Los_Angeles"}, listings[0].Input)
	updates := mock.UpdateEvent().Calls()
	require.Len(t, updates, 1)
	attendees := []outlookcalendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}, {Email: "alice@example.com"}}
	require.Equal(t, outlookcalendar.UpdateEventInput{EventID: "hold-1", Attendees: &attendees, TimeZone: "America/Los_Angeles"}, updates[0].Input)
	require.Equal(t, outlookcalendar.UpdateEventBranchUpdated, updates[0].Branch)
}
