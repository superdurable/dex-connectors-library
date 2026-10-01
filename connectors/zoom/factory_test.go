// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type meetingPageTarget struct {
	dex.StepDefaultsNoWaitFor[zoom.ListMeetingsResult]
}

func (meetingPageTarget) Execute(dex.Context, zoom.ListMeetingsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type meetingTarget struct {
	dex.StepDefaultsNoWaitFor[zoom.GetMeetingResult]
}

func (meetingTarget) Execute(dex.Context, zoom.GetMeetingResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type createdMeetingTarget struct {
	dex.StepDefaultsNoWaitFor[zoom.CreateMeetingResult]
}

func (createdMeetingTarget) Execute(dex.Context, zoom.CreateMeetingResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type updatedMeetingTarget struct {
	dex.StepDefaultsNoWaitFor[zoom.UpdateMeetingResult]
}

func (updatedMeetingTarget) Execute(dex.Context, zoom.UpdateMeetingResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type participantPageTarget struct {
	dex.StepDefaultsNoWaitFor[zoom.ListPastMeetingParticipantsResult]
}

func (participantPageTarget) Execute(dex.Context, zoom.ListPastMeetingParticipantsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGeneratedFactoriesExposeEveryTypedBranchAndExecutionDefault(t *testing.T) {
	client, err := zoom.New(zoom.Config{}, staticZoomCredentials())
	require.NoError(t, err)
	connection, err := zoom.NewConnection(client, zoomConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "zoom", GroupLabel: "Zoom", Explanation: "Factory test."}

	list := zoom.NewListMeetingsStep(zoom.ListMeetingsStepConfig[string]{
		StepType: "ListMeetings", Annotations: annotations, Connection: connection, ConnectionName: zoomConnection.Name,
		MapToOperationInput: func(string) zoom.ListMeetingsInput { return zoom.ListMeetingsInput{} },
		Listed:              sdkgo.GoTo(meetingPageTarget{}), ProviderRejected: sdkgo.GoTo(meetingPageTarget{}),
		InvalidResponse: sdkgo.GoTo(meetingPageTarget{}), Defect: sdkgo.GoTo(meetingPageTarget{}),
	})
	requireExecutionDefaults(t, list.GetStepOptions(), dex.StepDurabilityAsync)

	get := zoom.NewGetMeetingStep(zoom.GetMeetingStepConfig[int64]{
		StepType: "GetMeeting", Annotations: annotations, Connection: connection,
		MapToOperationInput: func(meetingID int64) zoom.GetMeetingInput { return zoom.GetMeetingInput{MeetingID: meetingID} },
		Found:               sdkgo.GoTo(meetingTarget{}), NotFound: sdkgo.GoTo(meetingTarget{}),
		ProviderRejected: sdkgo.GoTo(meetingTarget{}), InvalidResponse: sdkgo.GoTo(meetingTarget{}), Defect: sdkgo.GoTo(meetingTarget{}),
	})
	requireExecutionDefaults(t, get.GetStepOptions(), dex.StepDurabilityAsync)

	createResult := dex.DefineAttribute[zoom.CreateMeetingResult]("zoom-create-meeting-result")
	create := zoom.NewCreateMeetingStep(zoom.CreateMeetingStepConfig[string]{
		StepType: "CreateMeeting", Annotations: annotations, Connection: connection,
		MapToOperationInput: func(topic string) zoom.CreateMeetingInput { return zoom.CreateMeetingInput{Topic: topic} },
		Created:             sdkgo.GoTo(createdMeetingTarget{}), ProviderRejected: sdkgo.GoTo(createdMeetingTarget{}),
		Uncertain: sdkgo.GoTo(createdMeetingTarget{}), Defect: sdkgo.GoTo(createdMeetingTarget{}), ResultAttribute: &createResult,
	})
	requireExecutionDefaults(t, create.GetStepOptions(), dex.StepDurabilitySync)

	update := zoom.NewUpdateMeetingStep(zoom.UpdateMeetingStepConfig[int64]{
		StepType: "UpdateMeeting", Annotations: annotations, Connection: connection,
		MapToOperationInput: func(meetingID int64) zoom.UpdateMeetingInput { return zoom.UpdateMeetingInput{MeetingID: meetingID} },
		Updated:             sdkgo.GoTo(updatedMeetingTarget{}), NotFound: sdkgo.GoTo(updatedMeetingTarget{}),
		ProviderRejected: sdkgo.GoTo(updatedMeetingTarget{}), Defect: sdkgo.GoTo(updatedMeetingTarget{}),
	})
	requireExecutionDefaults(t, update.GetStepOptions(), dex.StepDurabilityAsync)

	participants := zoom.NewListPastMeetingParticipantsStep(zoom.ListPastMeetingParticipantsStepConfig[int64]{
		StepType: "ListPastMeetingParticipants", Annotations: annotations, Connection: connection,
		MapToOperationInput: func(meetingID int64) zoom.ListPastMeetingParticipantsInput {
			return zoom.ListPastMeetingParticipantsInput{MeetingID: meetingID}
		},
		Listed: sdkgo.GoTo(participantPageTarget{}), NotFound: sdkgo.GoTo(participantPageTarget{}),
		ProviderRejected: sdkgo.GoTo(participantPageTarget{}), InvalidResponse: sdkgo.GoTo(participantPageTarget{}),
		Defect: sdkgo.GoTo(participantPageTarget{}),
	})
	requireExecutionDefaults(t, participants.GetStepOptions(), dex.StepDurabilityAsync)

	require.Panics(t, func() {
		zoom.NewCreateMeetingStep(zoom.CreateMeetingStepConfig[string]{
			StepType: "CreateMeetingWithoutHappyPath", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) zoom.CreateMeetingInput { return zoom.CreateMeetingInput{} },
			Uncertain:           sdkgo.GoTo(createdMeetingTarget{}),
		})
	}, "the created branch is required")
	require.Panics(t, func() {
		zoom.NewGetMeetingStep(zoom.GetMeetingStepConfig[int64]{
			StepType: "GetMeetingOnAnotherConnection", Annotations: annotations, Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(int64) zoom.GetMeetingInput { return zoom.GetMeetingInput{} },
			Found:               sdkgo.GoTo(meetingTarget{}),
		})
	}, "a static connection name must match the runtime connection")
}

// requireExecutionDefaults checks the manifest defaults; createMeeting stays sync so a slow create is never re-sent.
func requireExecutionDefaults(t *testing.T, options *dex.StepOptions, durability dex.StepDurability) {
	t.Helper()
	require.Equal(t, durability, options.ExecuteDurability)
	require.Equal(t, 30*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, int32(5), options.ExecuteRetry.MaximumAttempts)
	require.Equal(t, 2*time.Minute, options.ExecuteRetry.TotalDuration)
}
