//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulemeeting

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/calendarmock"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
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
	holds := mock.CreateEvent().Calls()
	require.Len(t, holds, 1)
	require.Equal(t, "none", holds[0].Input.SendUpdates, "the hold is placed without notifying anyone")
	require.Empty(t, holds[0].Input.Attendees, "the hold is placed before anyone is invited")
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

func TestScheduleMeetingFailsOnAnIncompleteMockListingWithRealDex(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		script        func(*calendarmock.Mock)
		wantMessage   string
		wantListCalls int
	}{
		{
			name: "repeated-page-token",
			script: func(mock *calendarmock.Mock) {
				mock.ListEvents().When(func(calendar.ListEventsInput) bool { return true }, calendarmock.ListEventsListed(calendar.ListEventsOutput{
					CalendarID: "primary", TimeZone: "America/Los_Angeles", NextPageToken: "same-page",
				}, nil))
			},
			wantMessage: "repeated a page token", wantListCalls: 2,
		},
		{
			name: "page-limit",
			script: func(mock *calendarmock.Mock) {
				pages := make([]calendar.ListEventsOutput, maximumMeetingWindowPages)
				for index := range pages {
					pages[index] = calendar.ListEventsOutput{
						CalendarID: "primary", TimeZone: "America/Los_Angeles", NextPageToken: fmt.Sprintf("limit-page-%d", index+2),
					}
				}
				pageNumber := connectormock.CursorPageNumber[calendar.ListEventsInput]("pageToken", "nextPageToken", pages)
				mock.ListEvents().When(func(calendar.ListEventsInput) bool { return true },
					connectormock.Paged(calendar.ListEventsBranchListed, pageNumber, pages...))
			},
			wantMessage: "exceeds the page limit", wantListCalls: maximumMeetingWindowPages,
		},
		{
			name: "oversized-page",
			script: func(mock *calendarmock.Mock) {
				events := make([]calendar.Event, maximumListedWindowEvents+1)
				for index := range events {
					events[index] = calendar.Event{
						ID: fmt.Sprintf("focusblock%04d", index), CalendarID: "primary", Status: "confirmed", Transparency: "transparent",
						Start: losAngelesBoundary("2026-02-20T14:15:00-08:00"), End: losAngelesBoundary("2026-02-20T14:45:00-08:00"),
					}
				}
				mock.ListEvents().When(func(calendar.ListEventsInput) bool { return true }, calendarmock.ListEventsListed(calendar.ListEventsOutput{
					CalendarID: "primary", TimeZone: "America/Los_Angeles", Events: events,
				}, nil))
			},
			wantMessage: "exceeds the listing limit", wantListCalls: 1,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mock := calendarmock.New(t, ConnectionName)
			scenario.script(mock)
			flow, harness := newScheduleMeetingHarnessWithConnection(t, mock.Connection(), CalendarSelection{})
			ctx := integrationContext(t)
			flowID := startScheduleMeeting(t, ctx, harness.client, flow, "mock-"+scenario.name, integrationMeetingInput())
			result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})

			require.NoError(t, err)
			require.Equal(t, dex.FlowFailed, result.Status)
			require.Contains(t, result.ErrorMessage, scenario.wantMessage)
			require.Len(t, mock.ListEvents().Calls(), scenario.wantListCalls)
			require.Empty(t, mock.UpdateEvent().Calls(), "an incomplete listing must not invite attendees")
		})
	}
}

func listedPageTokens(mock *calendarmock.Mock) []string {
	var pageTokens []string
	for _, call := range mock.ListEvents().Calls() {
		pageTokens = append(pageTokens, call.Input.PageToken)
	}
	return pageTokens
}
