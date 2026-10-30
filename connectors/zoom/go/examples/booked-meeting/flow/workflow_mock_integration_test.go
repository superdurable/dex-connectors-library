//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookedmeeting

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/zoommock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBookedMeetingUsesZoomMocksWithRealDex(t *testing.T) {
	mock := zoommock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection(), &AttendancePolicy{CheckDelay: time.Hour})
	movedStart := time.Date(2036, time.February, 25, 18, 0, 0, 0, time.UTC)
	mock.GetMeeting().Respond(zoommock.GetMeetingFound(zoom.Meeting{
		ID: 92674392836, HostID: "zoom-host-1", Topic: "Consultation", Type: 2,
		StartTime: &movedStart, DurationMinutes: 45, TimeZone: "America/Los_Angeles",
		JoinURL: "https://us05web.zoom.us/j/92674392836?pwd=mock", Settings: zoom.MeetingSettings{WaitingRoom: true},
	}, nil))
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "zoom-booked-meeting-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	booking := Input{
		BookingID: "BK-MOCK-1", Topic: "Consultation", StartTime: "2036-02-24T09:00:00-08:00",
		TimeZone: "America/Los_Angeles", DurationMinutes: 30, Agenda: "Intro call",
	}
	_, err = client.StartFlow(ctx, flow, flowID, booking, dex.StartFlowOptions{})
	require.NoError(t, err)
	var scheduled BookedMeeting
	require.Eventually(t, func() bool {
		return client.InvokeRPC(ctx, flowID, flow.GetBookedMeeting, nil, &scheduled) == nil && scheduled.Phase == PhaseScheduled
	}, time.Minute, 100*time.Millisecond)
	require.Equal(t, int64(92674392836), scheduled.MeetingID)
	require.Equal(t, "https://us05web.zoom.us/j/92674392836?pwd=mock", scheduled.JoinURL)

	moved := RescheduleBookedMeetingInput{StartTime: "2036-02-25T10:00:00-08:00", TimeZone: "America/Los_Angeles", DurationMinutes: 45}
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.RescheduleBookedMeeting, moved, nil))
	require.Eventually(t, func() bool {
		return client.InvokeRPC(ctx, flowID, flow.GetBookedMeeting, nil, &scheduled) == nil && scheduled.Phase == PhaseScheduled && scheduled.Reschedules == 1
	}, time.Minute, 100*time.Millisecond)
	require.Equal(t, 45, scheduled.DurationMinutes)
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.CheckBookedMeetingAttendance, nil, nil))
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var completed BookedMeeting
	require.NoError(t, result.DecodeSingleOutput(&completed))
	require.Equal(t, PhaseAttended, completed.Phase)
	require.Equal(t, 1, completed.GuestJoins)

	createCalls := mock.CreateMeeting().Calls()
	require.Len(t, createCalls, 1)
	isWaitingRoomOn := true
	require.Equal(t, zoom.CreateMeetingInput{
		Topic: "Consultation", StartTime: "2036-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles",
		DurationMinutes: 30, Agenda: "Intro call", Settings: &zoom.MeetingSettingsInput{WaitingRoom: &isWaitingRoomOn},
	}, createCalls[0].Input)
	updateCalls := mock.UpdateMeeting().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, zoom.UpdateMeetingInput{
		MeetingID: 92674392836, StartTime: "2036-02-25T10:00:00-08:00", TimeZone: "America/Los_Angeles", DurationMinutes: 45,
	}, updateCalls[0].Input)
	getCalls := mock.GetMeeting().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, zoom.GetMeetingInput{MeetingID: 92674392836}, getCalls[0].Input)
	participantCalls := mock.ListPastMeetingParticipants().Calls()
	require.Len(t, participantCalls, 1)
	require.Equal(t, zoom.ListPastMeetingParticipantsInput{MeetingID: 92674392836, PageSize: 300}, participantCalls[0].Input)
	require.Empty(t, mock.ListMeetings().Calls())
}
