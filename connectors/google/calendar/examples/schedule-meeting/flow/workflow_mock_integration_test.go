//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulemeeting

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/calendarmock"
)

func TestScheduleMeetingExampleRunsOnCalendarMockDefaultsWithRealDex(t *testing.T) {
	mock := calendarmock.New(t, ConnectionName)
	flow, harness := newScheduleMeetingHarnessWithConnection(t, mock.Connection(), CalendarSelection{})
	ctx := integrationContext(t)
	flowID := startScheduleMeeting(t, ctx, harness.client, flow, "mock", integrationMeetingInput())
	outcome := waitForCompletedMeeting(t, ctx, harness.client, flowID)

	require.Equal(t, MeetingConflict, outcome.Status)
	require.True(t, outcome.IsWindowTruncated)
	require.NotNil(t, outcome.Event)
	require.Empty(t, outcome.Event.Attendees)
	require.Len(t, mock.QueryFreeBusy().Calls(), 1)
	require.Len(t, mock.CreateEvent().Calls(), 1)
	require.Len(t, mock.GetEvent().Calls(), 1)
	require.Len(t, mock.ListEvents().Calls(), 1)
	require.Empty(t, mock.UpdateEvent().Calls(), "the Flow must not invite anyone while the window has another page")
}
