// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employeechanged

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(action bamboohr.EmployeeChangeAction) sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent] {
	lastChanged := time.Date(2026, 10, 13, 9, 0, 1, 0, time.UTC)
	return sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]{
		ID: "employee-140-updated-1791882001", OccurredAt: lastChanged,
		Payload: bamboohr.EmployeeChangedEvent{EmployeeID: "140", Action: action, LastChanged: lastChanged},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(bamboohr.EmployeeChangeActionUpdated)
	require.Equal(t, "bamboohr-employee-change-employee-140-updated-1791882001", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryEmployeeChange(event))
	require.Equal(t, bamboohr.GetEmployeeInput{EmployeeID: "140", Fields: HandOffFields}, MapToGetEmployeeInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, action := range []bamboohr.EmployeeChangeAction{bamboohr.EmployeeChangeActionInserted, bamboohr.EmployeeChangeActionUpdated, bamboohr.EmployeeChangeActionDeleted} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(action))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(bamboohr.EmployeeChangeActionInserted))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingEmployee := MapToFlowInput(sampleTriggerEvent(bamboohr.EmployeeChangeActionInserted))
	missingEmployee.Change.EmployeeID = ""
	require.ErrorContains(t, validateInput(missingEmployee), "change.employeeId")
	missingTime := MapToFlowInput(sampleTriggerEvent(bamboohr.EmployeeChangeActionInserted))
	missingTime.Change.LastChanged = time.Time{}
	require.ErrorContains(t, validateInput(missingTime), "change.lastChanged")
	require.ErrorContains(t, validateInput(MapToFlowInput(sampleTriggerEvent("Moved"))), "change.action")
}

func TestTriggerBindingShowsOneGuidedChangeTypePicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, bamboohr.EmployeeChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, EmployeeChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the change type is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, bamboohr.UIUnitChangeTypePicker, unit.UnitID)
	require.Equal(t, "Employee changes", unit.Label)
	require.False(t, unit.Required, "an unsaved choice delivers every change")
	for _, guidance := range []string{
		"Inserted for new hires", "including terminations", "Deleted", "saves inserted, updated, or deleted",
		"unsaved", "Every change", "API key user can see", "current time", "restart the Worker",
	} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: bamboohr.UIChangeTypePickerPortChangeType, JSONPointer: "/changeType"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(bamboohr.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordEmployeeChangeStepType, dex.GetFinalStepType[Input](recordEmployeeChange{}))
	require.Equal(t, completeEmployeeChangeStepType, dex.GetFinalStepType[bamboohr.GetEmployeeResult](completeEmployeeChange{}))
	wait, err := recordEmployeeChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme"}, sdkgo.StaticCredentialProvider[bamboohr.Credentials]{})
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(client, sdkgo.ConnectionRef{Provider: "bamboohr", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
