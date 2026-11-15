// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(eventType gorgias.TicketEventType) sdkgo.TriggerEvent[gorgias.TicketChangedEvent] {
	occurredAt := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	return sdkgo.TriggerEvent[gorgias.TicketChangedEvent]{
		ID: "gorgias-event-9001", OccurredAt: occurredAt,
		Payload: gorgias.TicketChangedEvent{EventID: 9001, EventType: eventType, TicketID: 7001, OccurredAt: occurredAt, UserID: 12},
	}
}

func TestTriggerMappingNamesOneFlowPerGorgiasEvent(t *testing.T) {
	event := sampleTriggerEvent(gorgias.TicketEventClosed)
	require.Equal(t, "gorgias-ticket-change-gorgias-event-9001", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Event: event.Payload}, input)
	require.True(t, AcceptEveryTicketChange(event))
	require.Equal(t, gorgias.GetTicketInput{TicketID: 7001}, MapToGetTicketInput(input))
}

func TestValidateInputRejectsIncompleteEvents(t *testing.T) {
	for _, eventType := range gorgias.TicketEventTypes() {
		require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(eventType))))
	}
	missingID := MapToFlowInput(sampleTriggerEvent(gorgias.TicketEventCreated))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingEvent := MapToFlowInput(sampleTriggerEvent(gorgias.TicketEventCreated))
	missingEvent.Event.EventID = 0
	require.ErrorContains(t, validateInput(missingEvent), "event.eventId")
	missingTicket := MapToFlowInput(sampleTriggerEvent(gorgias.TicketEventCreated))
	missingTicket.Event.TicketID = 0
	require.ErrorContains(t, validateInput(missingTicket), "event.ticketId")
	messageEvent := MapToFlowInput(sampleTriggerEvent("ticket-message-created"))
	require.ErrorContains(t, validateInput(messageEvent), "event.eventType")
}

func TestTriggerBindingDeclaresTheStableBindingWithoutPickerUnits(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, gorgias.TicketChangedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, TicketChangedTriggerBinding, binding.BindingName)
	if binding.ConfigurationUI != nil {
		require.Empty(t, binding.ConfigurationUI.Units, "Studio setup commands cannot send a Gorgias API key, so no picker is shown")
	}
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(gorgias.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTicketChangeStepType, dex.GetFinalStepType[Input](recordTicketChange{}))
	require.Equal(t, completeTicketChangeStepType, dex.GetFinalStepType[gorgias.GetTicketResult](completeTicketChange{}))
	wait, err := recordTicketChange{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := gorgias.New(gorgias.Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[gorgias.Credentials]{})
	require.NoError(t, err)
	connection, err := gorgias.NewConnection(client, sdkgo.ConnectionRef{Provider: "gorgias", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
