//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/calendlymock"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestInviteeRecorderRunsOnCalendlyMockWithRealDex(t *testing.T) {
	mock := calendlymock.New(t, inviteerecorder.ConnectionName)
	flow := inviteerecorder.NewFlow(mock.Connection())
	client := startWorkerAndClient(t, flow)

	booking := inviteerecorder.Booking{
		EventID:      "invitee.created:EVT00001:INV00001",
		InviteeURI:   "https://api.calendly.com/scheduled_events/EVT00001/invitees/INV00001",
		InviteeEmail: "ada@example.com", InviteeName: "Ada Lovelace",
		ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVT00001",
		BookedAt:          time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC),
	}
	flowID := fmt.Sprintf("calendly-mock-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, booking, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var recorded inviteerecorder.RecordedScheduledEvent
	require.NoError(t, result.DecodeSingleOutput(&recorded))
	require.Equal(t, calendly.GetScheduledEventBranchFound, recorded.Branch)
	require.Equal(t, booking.ScheduledEventURI, recorded.ScheduledEvent.URI)
	calls := mock.GetScheduledEvent().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, calendly.GetScheduledEventInput{ScheduledEventURI: booking.ScheduledEventURI}, calls[0].Input)
	require.Equal(t, calendly.GetScheduledEventBranchFound, calls[0].Branch)
}
