// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newconversationintake

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	hiver "github.com/superdurable/dex-connectors-library/connectors/hiver/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func receivedEvent() sdkgo.TriggerEvent[hiver.ConversationReceivedEvent] {
	return sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]{
		ID: "conversation-105902-573741352", OccurredAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC),
		Payload: hiver.ConversationReceivedEvent{InboxID: "105902", Conversation: hiver.Conversation{
			ID: "573741352", InboxID: "105902", Status: hiver.ConversationStatusOpen, TagIDs: []string{},
		}},
	}
}

func TestResolveFlowIDAndMappingsAreDeterministic(t *testing.T) {
	event := receivedEvent()
	require.Equal(t, "hiver-new-conversation-conversation-105902-573741352", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(receivedEvent()), "a redelivered conversation maps to the same Flow")
	require.True(t, AcceptEveryConversation(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Received: event.Payload, DetectedAt: event.OccurredAt}, input)
	require.Equal(t, hiver.GetConversationInput{InboxID: "105902", ConversationID: "573741352"}, MapToGetConversationInput(input))
}

func TestFlowDeclaresItsConversationReceivedBindingWithTheInboxPicker(t *testing.T) {
	connection, err := hiver.NewConnection(newTestClient(t), sdkgo.ConnectionRef{Provider: "hiver", Name: ConnectionName})
	require.NoError(t, err)
	bindings := NewFlow(connection).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, ConversationReceivedTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, hiver.ConversationReceivedTriggerDefinition, bindings[0].Definition)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 1, "the binding shows exactly the shared inbox field")
	unit := units[0]
	require.Equal(t, hiver.UIUnitInboxPicker, unit.UnitID)
	require.True(t, unit.Required)
	for _, guidance := range []string{"Load inboxes", "https://api2.hiverhq.com/v1/inboxes", "105902", "not secret", "never start Flows", "Restart the Worker", "It cannot be blank."} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: hiver.UIInboxPickerPortInboxID, JSONPointer: "/inboxId"}}, unit.Bindings)

	// The picker output decodes into the Trigger configuration the runner loads.
	var configuration hiver.ConversationReceivedTriggerConfiguration
	require.NoError(t, json.Unmarshal([]byte(`{"inboxId":"105902"}`), &configuration))
	require.NoError(t, configuration.Validate())
}

func TestFlowIdentitiesAndSchema(t *testing.T) {
	connection, err := hiver.NewConnection(newTestClient(t), sdkgo.ConnectionRef{Provider: "hiver", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordReceivedConversationStepType, dex.GetFinalStepType[Input](recordReceivedConversation{}))
	require.Equal(t, completeConversationIntakeStepType, dex.GetFinalStepType[hiver.GetConversationResult](completeConversationIntake{}))
	require.Equal(t, []dex.AttributeDef{receivedConversationAttribute, intakeOutcomeAttribute}, flow.GetPersistenceSchema().Attributes)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
}

func newTestClient(t *testing.T) *hiver.Client {
	t.Helper()
	client, err := hiver.New(hiver.Config{}, sdkgo.StaticCredentialProvider[hiver.Credentials]{})
	require.NoError(t, err)
	return client
}
