// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bookedmeeting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	policy := DefaultAttendancePolicy()
	flow := NewFlow(newUnitTestConnection(t, ConnectionName), &policy)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordBookingStepType, dex.GetFinalStepType[Input](recordBooking{}))
	require.Equal(t, awaitBookedMeetingStepType, dex.GetFinalStepType[MeetingWait](awaitBookedMeeting{}))
	wait, err := recordBooking{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRequiresAnAttendancePolicyAndItsStaticConnection(t *testing.T) {
	connection := newUnitTestConnection(t, ConnectionName)
	require.Panics(t, func() { NewFlow(connection, nil) })
	require.Panics(t, func() { NewFlow(connection, &AttendancePolicy{CheckDelay: -time.Second}) })

	policy := DefaultAttendancePolicy()
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(connection, &policy)})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"), &policy)})
	})
}

func TestMappersPassOnlyTheRecordedBooking(t *testing.T) {
	isWaitingRoomOn := true
	booking := Input{
		BookingID: "BK-77", Topic: "Consultation", StartTime: "2036-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles",
		DurationMinutes: 30, Agenda: "Intro call",
	}
	require.Equal(t, zoom.CreateMeetingInput{
		Topic: "Consultation", StartTime: "2036-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles", DurationMinutes: 30,
		Agenda: "Intro call", Settings: &zoom.MeetingSettingsInput{WaitingRoom: &isWaitingRoomOn},
	}, MapToCreateMeetingInput(booking))
	require.Equal(t, zoom.ListMeetingsInput{Type: zoom.MeetingListUpcoming, PageSize: 300}, MapToFindUncertainMeetingInput(UncertainCreate{}))
	require.Equal(t, zoom.UpdateMeetingInput{MeetingID: 9, StartTime: "2036-02-25T09:00:00-08:00", TimeZone: "America/Los_Angeles", DurationMinutes: 45},
		MapToUpdateMeetingInput(MeetingReschedule{MeetingID: 9, Request: RescheduleBookedMeetingInput{
			StartTime: "2036-02-25T09:00:00-08:00", TimeZone: "America/Los_Angeles", DurationMinutes: 45,
		}}))
	require.Equal(t, zoom.GetMeetingInput{MeetingID: 9}, MapToGetMeetingInput(zoom.UpdateMeetingResult{Value: zoom.UpdatedMeeting{MeetingID: 9}}))
	require.Equal(t, zoom.ListPastMeetingParticipantsInput{MeetingID: 9, PageSize: 300}, MapToListPastMeetingParticipantsInput(AttendanceCheck{MeetingID: 9}))
}

func TestBookingValidationRefusesAmbiguousSlots(t *testing.T) {
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	valid := Input{BookingID: "BK-77", Topic: "Consultation", StartTime: "2026-10-08T09:00:00-07:00", TimeZone: "America/Los_Angeles", DurationMinutes: 30}
	_, err := validateBooking(valid, now)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*Input){
		"naive start":   func(input *Input) { input.StartTime = "2026-10-08T09:00:00" },
		"abbreviation":  func(input *Input) { input.TimeZone = "PDT" },
		"past start":    func(input *Input) { input.StartTime = "2026-09-29T09:00:00-07:00" },
		"booking id":    func(input *Input) { input.BookingID = "BK 77" },
		"blank topic":   func(input *Input) { input.Topic = " " },
		"long meeting":  func(input *Input) { input.DurationMinutes = 1441 },
		"zero duration": func(input *Input) { input.DurationMinutes = 0 },
	} {
		input := valid
		mutate(&input)
		_, err := validateBooking(input, now)
		require.Error(t, err, name)
	}
}

// TestReconciliationMatchesInstantsNotText guards the timezone trap: equal text in another zone is a different meeting.
func TestReconciliationMatchesInstantsNotText(t *testing.T) {
	recordedAt := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	booking := Input{Topic: "Consultation", StartTime: "2026-10-08T09:00:00-07:00", DurationMinutes: 30}
	start := time.Date(2026, time.October, 8, 16, 0, 0, 0, time.UTC)
	sameTextOtherZone := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)
	createdAfter, createdBefore := recordedAt.Add(time.Minute), recordedAt.Add(-time.Hour)
	meetings := []zoom.MeetingSummary{
		{ID: 1, Topic: "Consultation", StartTime: &start, DurationMinutes: 30, CreatedAt: &createdAfter},
		{ID: 2, Topic: "Consultation", StartTime: &sameTextOtherZone, DurationMinutes: 30, CreatedAt: &createdAfter},
		{ID: 3, Topic: "Consultation", StartTime: &start, DurationMinutes: 30, CreatedAt: &createdBefore},
		{ID: 4, Topic: "Consultation", StartTime: &start, DurationMinutes: 60, CreatedAt: &createdAfter},
		{ID: 5, Topic: "Consultation", DurationMinutes: 30, CreatedAt: &createdAfter},
		{ID: 6, Topic: "Consultation", StartTime: &start, DurationMinutes: 30},
	}
	matches, err := FindCreatedMeeting(meetings, booking, recordedAt)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, int64(1), matches[0].ID)
}

func TestStoredSlotMustEqualTheRequestedInstantAndLength(t *testing.T) {
	stored := time.Date(2026, time.October, 8, 16, 0, 0, 0, time.UTC)
	record := BookedMeeting{StartTime: &stored, DurationMinutes: 30}
	require.Empty(t, describeStoredSlotMismatch(record, "2026-10-08T09:00:00-07:00", 30))
	require.NotEmpty(t, describeStoredSlotMismatch(record, "2026-10-08T09:00:00Z", 30))
	require.NotEmpty(t, describeStoredSlotMismatch(record, "2026-10-08T09:00:00-07:00", 45))
	require.NotEmpty(t, describeStoredSlotMismatch(BookedMeeting{DurationMinutes: 30}, "2026-10-08T09:00:00-07:00", 30))
	require.Equal(t, stored.Add(45*time.Minute), attendanceCheckTime(record, 15*time.Minute))
}

func TestOnlyGuestsCountAsAttendance(t *testing.T) {
	participants := []zoom.MeetingParticipant{{ID: "host-1"}, {ID: "host-1"}, {ID: "guest-1"}, {ID: ""}}
	require.Equal(t, 2, CountGuestJoins(participants, "host-1"))
	require.Zero(t, CountGuestJoins([]zoom.MeetingParticipant{{ID: "host-1"}}, "host-1"))
}

func newUnitTestConnection(t *testing.T, connectionName string) zoom.Connection {
	t.Helper()
	client, err := zoom.New(zoom.Config{}, sdkgo.StaticCredentialProvider[zoom.Credentials]{})
	require.NoError(t, err)
	connection, err := zoom.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoom", Name: connectionName})
	require.NoError(t, err)
	return connection
}
