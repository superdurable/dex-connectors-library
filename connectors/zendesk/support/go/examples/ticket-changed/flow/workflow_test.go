// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(kind support.TicketChangeKind) sdkgo.TriggerEvent[support.TicketChangedEvent] {
	return sdkgo.TriggerEvent[support.TicketChangedEvent]{
		ID: "zendesk-ticket-0123456789abcdef0123456789abcdef",
		Payload: support.TicketChangedEvent{
			ChangeKind: kind,
			Ticket:     support.Ticket{ID: 35436, Subject: "Double charge on order 88213", Status: support.TicketStatusOpen},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerChange(t *testing.T) {
	event := sampleTriggerEvent(support.TicketChangeUpdated)
	require.Equal(t, "zendesk-ticket-change-zendesk-ticket-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Change: event.Payload}, input)
	require.True(t, AcceptEveryTicketChange(event))
	require.Equal(t, support.GetTicketInput{TicketID: 35436, LatestCommentLimit: 1}, MapToGetTicketInput(input))
}

func TestValidateInputRejectsIncompleteChanges(t *testing.T) {
	for _, kind := range []support.TicketChangeKind{support.TicketChangeCreated, support.TicketChangeUpdated, support.TicketChangeDeleted} {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(kind))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(support.TicketChangeCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingTicket := MapToFlowInput(sampleTriggerEvent(support.TicketChangeCreated))
	missingTicket.Change.Ticket.ID = 0
	require.ErrorContains(t, validateInput(missingTicket), "change.ticket.id")
	unknownKind := MapToFlowInput(sampleTriggerEvent("merged"))
	require.ErrorContains(t, validateInput(unknownKind), "changeKind")
}

// TestTriggerBindingHasNoVisibleConfiguration records that Dex Web shows no Trigger field: the connection
// form carries the guidance, and the zero configuration watches the whole account.
func TestTriggerBindingHasNoVisibleConfiguration(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, support.TicketChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, TicketChangedTriggerBinding, binding.BindingName)
	require.True(t, binding.ConfigurationUI == nil || len(binding.ConfigurationUI.Units) == 0)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(support.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTicketChangeStepType, dex.GetFinalStepType[Input](recordTicketChange{}))
	require.Equal(t, completeTicketChangeStepType, dex.GetFinalStepType[support.GetTicketResult](completeTicketChange{}))
	wait, err := recordTicketChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := support.New(support.Config{Subdomain: "acme"}, sdkgo.StaticCredentialProvider[support.Credentials]{})
	require.NoError(t, err)
	connection, err := support.NewConnection(client, sdkgo.ConnectionRef{Provider: "zendesk", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
