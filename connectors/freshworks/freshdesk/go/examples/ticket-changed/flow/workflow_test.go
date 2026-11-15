// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind freshdesk.TicketChangeKind) sdkgo.TriggerEvent[freshdesk.TicketChangedEvent] {
	updatedAt := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	return sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]{
		ID: "freshdesk-ticket-1201-1772442000", OccurredAt: updatedAt,
		Payload: freshdesk.TicketChangedEvent{
			ChangeKind: kind,
			Ticket:     freshdesk.Ticket{ID: 1201, Subject: "Double charge", Status: freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityLow, UpdatedAt: updatedAt},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerTicketVersion(t *testing.T) {
	event := sampleTriggerEvent(freshdesk.TicketChangeUpdated)
	require.Equal(t, "freshdesk-ticket-change-freshdesk-ticket-1201-1772442000", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryTicketChange(event))
	require.Equal(t, freshdesk.GetTicketInput{TicketID: 1201}, MapToGetTicketInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []freshdesk.TicketChangeKind{freshdesk.TicketChangeCreated, freshdesk.TicketChangeUpdated} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(freshdesk.TicketChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingTicket := MapToFlowInput(sampleTriggerEvent(freshdesk.TicketChangeCreated))
	missingTicket.Change.Ticket.ID = 0
	require.ErrorContains(t, validateInput(missingTicket), "change.ticket.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("merged"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

func TestTriggerBindingDeclaresTheStableBindingWithoutPickerUnits(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, freshdesk.TicketChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, TicketChangedTriggerBinding, binding.BindingName)
	if binding.ConfigurationUI != nil {
		require.Empty(t, binding.ConfigurationUI.Units, "Freshdesk setup commands cannot authenticate, so no picker is shown")
	}
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(freshdesk.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTicketChangeStepType, dex.GetFinalStepType[Input](recordTicketChange{}))
	require.Equal(t, completeTicketChangeStepType, dex.GetFinalStepType[freshdesk.GetTicketResult](completeTicketChange{}))
	wait, err := recordTicketChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := freshdesk.New(freshdesk.Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[freshdesk.Credentials]{})
	require.NoError(t, err)
	connection, err := freshdesk.NewConnection(client, sdkgo.ConnectionRef{Provider: "freshdesk", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
