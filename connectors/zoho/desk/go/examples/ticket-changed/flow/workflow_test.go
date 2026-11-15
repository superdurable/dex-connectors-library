// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind desk.TicketChangeKind) sdkgo.TriggerEvent[desk.TicketChangedEvent] {
	return sdkgo.TriggerEvent[desk.TicketChangedEvent]{
		ID: "zdesk-0123456789abcdef0123456789abcdef",
		Payload: desk.TicketChangedEvent{
			OrgID: "2389290", DepartmentID: "1892000000006907", ChangeKind: kind,
			Ticket: desk.Ticket{ID: "1892000000099001", Subject: "Billing question", Status: desk.TicketStatusOpen},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerTicketVersion(t *testing.T) {
	event := sampleTriggerEvent(desk.TicketChangeUpdated)
	require.Equal(t, "zoho-desk-ticket-change-zdesk-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryTicketChange(event))
	require.Equal(t, desk.GetTicketInput{TicketID: "1892000000099001"}, MapToGetTicketInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []desk.TicketChangeKind{desk.TicketChangeCreated, desk.TicketChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(desk.TicketChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingTicket := MapToFlowInput(sampleTriggerEvent(desk.TicketChangeCreated))
	missingTicket.Change.Ticket.ID = ""
	require.ErrorContains(t, validateInput(missingTicket), "change.ticket.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("deleted"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingShowsOneGuidedDepartmentPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, desk.TicketChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, TicketChangedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the picker is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, desk.UIUnitDepartmentPicker, unit.UnitID)
	require.Equal(t, "Watched department", unit.Label)
	require.False(t, unit.Required, "a blank picker watches every department")
	for _, guidance := range []string{"connection's organization", "department ID and name", "blank", "every department", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{
		{Port: desk.UIDepartmentPickerPortDepartmentID, JSONPointer: "/departmentId"},
		{Port: desk.UIDepartmentPickerPortDepartmentName, JSONPointer: "/departmentName"},
	}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(desk.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTicketChangeStepType, dex.GetFinalStepType[Input](recordTicketChange{}))
	require.Equal(t, completeTicketChangeStepType, dex.GetFinalStepType[desk.GetTicketResult](completeTicketChange{}))
	wait, err := recordTicketChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := desk.New(desk.Config{OrgID: "2389290"}, sdkgo.StaticCredentialProvider[desk.Credentials]{})
	require.NoError(t, err)
	connection, err := desk.NewConnection(client, sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
