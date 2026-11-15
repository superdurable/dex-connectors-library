// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meetingchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind zoom.MeetingChangeKind) sdkgo.TriggerEvent[zoom.MeetingChangedEvent] {
	return sdkgo.TriggerEvent[zoom.MeetingChangedEvent]{
		ID: "zoom-meeting-0123456789abcdef0123456789abcdef",
		Payload: zoom.MeetingChangedEvent{
			ChangeKind: kind, Meeting: zoom.MeetingSummary{ID: 92674392836, Topic: "Consultation", Type: 2},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(zoom.MeetingChangeUpdated)
	require.Equal(t, "zoom-meeting-change-zoom-meeting-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryMeetingChange(event))
	require.Equal(t, zoom.GetMeetingInput{MeetingID: 92674392836}, MapToGetMeetingInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []zoom.MeetingChangeKind{zoom.MeetingChangeScheduled, zoom.MeetingChangeUpdated, zoom.MeetingChangeRemoved} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(zoom.MeetingChangeScheduled))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingMeeting := MapToFlowInput(sampleTriggerEvent(zoom.MeetingChangeScheduled))
	missingMeeting.Change.Meeting.ID = 0
	require.ErrorContains(t, validateInput(missingMeeting), "change.meeting.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("ended"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingHasNoVisibleField(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, zoom.MeetingChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, MeetingChangedTriggerBinding, binding.BindingName)
	require.Nil(t, binding.ConfigurationUI, "the binding watches the authorized user's own meetings; nothing is picked")
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(zoom.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordMeetingChangeStepType, dex.GetFinalStepType[Input](recordMeetingChange{}))
	require.Equal(t, completeMeetingChangeStepType, dex.GetFinalStepType[zoom.GetMeetingResult](completeMeetingChange{}))
	wait, err := recordMeetingChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := zoom.New(zoom.Config{}, sdkgo.StaticCredentialProvider[zoom.Credentials]{})
	require.NoError(t, err)
	connection, err := zoom.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoom", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
