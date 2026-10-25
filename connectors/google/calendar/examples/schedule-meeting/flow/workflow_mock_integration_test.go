//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulemeeting

import (
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/calendarmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestScheduleMeetingExampleRunsOnCalendarMockDefaultsWithRealDex(t *testing.T) {
	mock := calendarmock.New(t, ConnectionName)
	flow, harness := newScheduleMeetingHarnessWithConnection(t, mock.Connection(), CalendarSelection{})
	ctx := integrationContext(t)
	input := integrationMeetingInput()
	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "mock", input)
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingScheduled, outcome.Status)
	require.NotNil(t, outcome.Event)
	require.Len(t, mock.QueryFreeBusy().Calls(), 1)
	require.Len(t, mock.CreateEvent().Calls(), 1)
	require.Len(t, mock.GetEvent().Calls(), 1)
	require.Equal(t, []string{"", "calendar-page-2", "calendar-page-3"}, listedPageTokens(mock), "the Flow must read every page in order")
	invitations := mock.UpdateEvent().Calls()
	require.Len(t, invitations, 1)
	wantAttendees := make([]calendar.EventAttendeeInput, len(input.Attendees))
	for index, email := range input.Attendees {
		wantAttendees[index] = calendar.EventAttendeeInput{Email: email}
	}
	require.Equal(t, "meetinghold0001", invitations[0].Input.EventID, "the guests join the hold the Flow created")
	require.Equal(t, &wantAttendees, invitations[0].Input.Attendees, "the Flow invites the requested guests")
	require.Equal(t, "all", invitations[0].Input.SendUpdates)
}

func TestScheduleMeetingExampleFindsABusyEventAcrossMockPagesWithRealDex(t *testing.T) {
	mock := calendarmock.New(t, ConnectionName)
	mock.ListEvents().When(func(calendar.ListEventsInput) bool { return true }, calendarmock.ListEventsBusyMeetingWindow())
	flow, harness := newScheduleMeetingHarnessWithConnection(t, mock.Connection(), CalendarSelection{})
	ctx := integrationContext(t)
	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "mock-busy", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingConflict, outcome.Status)
	require.Equal(t, []string{"boardprep0001"}, outcome.ConflictingEventIDs)
	require.NotNil(t, outcome.Event)
	require.Empty(t, outcome.Event.Attendees)
	require.Equal(t, []string{"", "busy-page-2"}, listedPageTokens(mock), "the Flow must read both pages before it decides")
	require.Empty(t, mock.UpdateEvent().Calls(), "a busy meeting must not invite attendees")
}

func TestScheduleMeetingFailsOnRepeatedMockPageTokenWithRealDex(t *testing.T) {
	mock := calendarmock.New(t, ConnectionName)
	mock.ListEvents().When(func(calendar.ListEventsInput) bool { return true }, calendarmock.ListEventsListed(calendar.ListEventsOutput{
		CalendarID: "primary", TimeZone: "America/Los_Angeles", NextPageToken: "same-page",
	}, nil))
	flow, harness := newScheduleMeetingHarnessWithConnection(t, mock.Connection(), CalendarSelection{})
	ctx := integrationContext(t)
	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "mock-repeated-page", integrationMeetingInput())
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})

	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Contains(t, result.ErrorMessage, "repeated a page token")
	require.Empty(t, mock.UpdateEvent().Calls(), "an incomplete listing must not invite attendees")
}

func listedPageTokens(mock *calendarmock.Mock) []string {
	var pageTokens []string
	for _, call := range mock.ListEvents().Calls() {
		pageTokens = append(pageTokens, call.Input.PageToken)
	}
	return pageTokens
}
