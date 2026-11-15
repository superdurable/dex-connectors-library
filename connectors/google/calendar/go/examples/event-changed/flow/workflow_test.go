// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind calendar.EventChangeKind) sdkgo.TriggerEvent[calendar.EventChangedEvent] {
	return sdkgo.TriggerEvent[calendar.EventChangedEvent]{
		ID: "gcal-0123456789abcdef0123456789abcdef",
		Payload: calendar.EventChangedEvent{
			CalendarID: "team@group.calendar.google.com", ChangeKind: kind,
			Event: calendar.Event{ID: "kickoff0001", Status: "confirmed", Summary: "Kickoff"},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(calendar.EventChangeUpdated)
	require.Equal(t, "google-calendar-event-change-gcal-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryEventChange(event))
	require.Equal(t, calendar.GetEventInput{CalendarID: "team@group.calendar.google.com", EventID: "kickoff0001"}, MapToGetEventInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []calendar.EventChangeKind{calendar.EventChangeCreated, calendar.EventChangeUpdated, calendar.EventChangeCancelled} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(calendar.EventChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingEvent := MapToFlowInput(sampleTriggerEvent(calendar.EventChangeCreated))
	missingEvent.Change.Event.ID = ""
	require.ErrorContains(t, validateInput(missingEvent), "change.event.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("moved"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedCalendarPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, calendar.EventChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, EventChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, calendar.UIUnitCalendarPicker, unit.UnitID)
	require.Equal(t, "Watched calendar", unit.Label)
	require.False(t, unit.Required, "an unsaved picker watches the primary calendar")
	for _, guidance := range []string{"Google Calendar list", "calendar ID and name", "unsaved", "primary calendar", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: calendar.UICalendarPickerPortCalendarID, JSONPointer: "/calendarId"},
		{Port: calendar.UICalendarPickerPortCalendarName, JSONPointer: "/calendarName"},
	}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(calendar.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordEventChangeStepType, dex.GetFinalStepType[Input](recordEventChange{}))
	require.Equal(t, completeEventChangeStepType, dex.GetFinalStepType[calendar.GetEventResult](completeEventChange{}))
	wait, err := recordEventChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := calendar.New(calendar.Config{}, sdkgo.StaticCredentialProvider[calendar.Credentials]{})
	require.NoError(t, err)
	connection, err := calendar.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
