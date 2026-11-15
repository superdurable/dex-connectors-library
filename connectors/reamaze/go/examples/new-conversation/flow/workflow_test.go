// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newconversation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func sampleTriggerEvent(status reamaze.ConversationStatus) sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent] {
	return sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]{
		ID: "reamaze-conversation-0123456789abcdef0123456789abcdef",
		Payload: reamaze.ConversationCreatedEvent{
			Brand: "acme", Channel: "support",
			Conversation: reamaze.Conversation{
				ID: "double-charge", Subject: "Double charge on order 88213", Status: status,
				Channel:   reamaze.ConversationChannel{Slug: "support", Name: "Support", Type: 1},
				Requester: &reamaze.ConversationParticipant{Name: "Jane Smith", Email: "jane@acme.example.com"},
				CreatedAt: time.Date(2026, 1, 26, 22, 2, 0, 0, time.UTC),
			},
		},
	}
}

func TestTriggerMappingNamesOneFlowPerConversation(t *testing.T) {
	event := sampleTriggerEvent(reamaze.ConversationStatusOpen)
	require.Equal(t, "reamaze-new-conversation-reamaze-conversation-0123456789abcdef0123456789abcdef", ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Created: event.Payload}, input)
	require.Equal(t, reamaze.GetConversationInput{ConversationID: "double-charge"}, MapToGetConversationInput(input))
}

func TestAcceptCustomerConversationSkipsOnlySpam(t *testing.T) {
	for _, status := range reamaze.ConversationStatuses() {
		isSpam := status == reamaze.ConversationStatusSpam || status == reamaze.ConversationStatusAISpam
		require.Equal(t, !isSpam, AcceptCustomerConversation(sampleTriggerEvent(status)), "status %d", status)
	}
}

func TestValidateInputRejectsIncompleteDeliveries(t *testing.T) {
	require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent(reamaze.ConversationStatusOpen))))
	missingID := MapToFlowInput(sampleTriggerEvent(reamaze.ConversationStatusOpen))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingConversation := MapToFlowInput(sampleTriggerEvent(reamaze.ConversationStatusOpen))
	missingConversation.Created.Conversation.ID = ""
	require.ErrorContains(t, validateInput(missingConversation), "created.conversation.id")
}

func TestNewIntakeSummarizesTheConversationReadBack(t *testing.T) {
	conversation := sampleTriggerEvent(reamaze.ConversationStatusResponded).Payload.Conversation
	intake := NewIntake(reamaze.ConversationDetails{Conversation: conversation, Messages: []reamaze.ConversationMessage{{Body: "I was charged twice."}}})
	require.Equal(t, Intake{
		ConversationID: "double-charge", Channel: "support", Subject: "Double charge on order 88213",
		RequesterEmail: "jane@acme.example.com", Status: reamaze.ConversationStatusResponded, MessageCount: 1,
	}, intake)
	conversation.Requester = nil
	require.Empty(t, NewIntake(reamaze.ConversationDetails{Conversation: conversation}).RequesterEmail)
}

func TestTriggerBindingShowsOneGuidedChannelEntry(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, reamaze.ConversationCreatedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, ConversationCreatedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the channel entry is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, reamaze.UIUnitChannelSlugEntry, unit.UnitID)
	require.Equal(t, "Watched channel", unit.Label)
	require.False(t, unit.Required, "a blank entry watches every channel")
	for _, guidance := range []string{
		"such as support", "GET https://{brand}.reamaze.io/api/v1/channels", "Settings > Channels", "Leave it blank",
		"every channel", "baseline", "Restart the Worker",
	} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: reamaze.UIChannelSlugEntryPortChannel, JSONPointer: "/channel"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(reamaze.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordNewConversationStepType, dex.GetFinalStepType[Input](recordNewConversation{}))
	require.Equal(t, completeNewConversationStepType, dex.GetFinalStepType[reamaze.GetConversationResult](completeNewConversation{}))
	wait, err := recordNewConversation{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := reamaze.New(reamaze.Config{Brand: "acme"}, sdkgo.StaticCredentialProvider[reamaze.Credentials]{})
	require.NoError(t, err)
	connection, err := reamaze.NewConnection(client, sdkgo.ConnectionRef{Provider: "reamaze", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
