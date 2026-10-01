// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package inviteerecorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func inviteeEvent(eventID string, webhookEvent string, inviteeStatus string) sdkgo.TriggerEvent[calendly.InviteeEvent] {
	bookedAt := time.Date(2026, time.September, 30, 11, 59, 0, 0, time.UTC)
	return sdkgo.TriggerEvent[calendly.InviteeEvent]{ID: eventID, OccurredAt: bookedAt, Payload: calendly.InviteeEvent{
		Event: webhookEvent, CreatedAt: bookedAt,
		Invitee: calendly.Invitee{
			URI: "https://api.calendly.com/scheduled_events/EVENT0001/invitees/INVITEE01", ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVENT0001",
			Email: "ada@example.com", Name: "Ada Lovelace", Status: inviteeStatus,
		},
	}}
}

func TestRedeliveriesResolveOneFlowAndCarryTheBooking(t *testing.T) {
	event := inviteeEvent("invitee.created:EVENT0001:INVITEE01", calendly.WebhookEventInviteeCreated, calendly.ScheduledEventStatusActive)
	redelivery := event
	redelivery.Payload.Invitee.Name = "Ada King"
	require.Equal(t, "calendly-invitee.created-EVENT0001-INVITEE01", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(redelivery))
	require.Equal(t, Booking{
		EventID: event.ID, InviteeURI: event.Payload.Invitee.URI, InviteeEmail: "ada@example.com", InviteeName: "Ada Lovelace",
		ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVENT0001", BookedAt: event.OccurredAt,
	}, MapToFlowInput(event))
	require.Equal(t, calendly.GetScheduledEventInput{ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVENT0001"},
		MapToGetScheduledEventInput(MapToFlowInput(event)))
}

func TestAcceptBookingAdmitsOnlyActiveNewInvitees(t *testing.T) {
	require.True(t, AcceptBooking(inviteeEvent("a", calendly.WebhookEventInviteeCreated, calendly.ScheduledEventStatusActive)))
	require.False(t, AcceptBooking(inviteeEvent("b", calendly.WebhookEventInviteeCanceled, calendly.ScheduledEventStatusCanceled)))
	require.False(t, AcceptBooking(inviteeEvent("c", calendly.WebhookEventInviteeCreated, calendly.ScheduledEventStatusCanceled)),
		"an invitee that is already canceled when the webhook arrives is not recorded as a booking")
}

func TestFlowDeclaresItsInviteeCreatedBindingWithTheEventTypePicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, InviteeCreatedTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, calendly.InviteeEventReceivedTriggerDefinition, bindings[0].Definition)
	require.NotNil(t, bindings[0].ConfigurationUI)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 1)
	require.Equal(t, calendly.UIUnitEventTypePicker, units[0].UnitID)
	require.False(t, units[0].Required, "blank records every event type")
	require.Contains(t, units[0].Description, "Leave it empty")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: calendly.UIEventTypePickerPortEventTypeURI, JSONPointer: "/eventTypeUri"}}, units[0].Bindings)
	require.Equal(t, FlowType, (&Flow{}).GetFlowType())
}
