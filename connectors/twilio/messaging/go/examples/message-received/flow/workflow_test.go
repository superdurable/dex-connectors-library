// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messagereceived

import (
	"testing"

	"github.com/stretchr/testify/require"
	messaging "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Split literals keep synthetic SIDs from matching repository secret scanning.
const sampleMessageSID = "SM" + "0123456789abcdef0123456789abcdef"

func sampleTriggerEvent(body string) sdkgo.TriggerEvent[messaging.MessageReceivedEvent] {
	return sdkgo.TriggerEvent[messaging.MessageReceivedEvent]{
		ID: "twilio-message-" + sampleMessageSID,
		Payload: messaging.MessageReceivedEvent{
			Message: messaging.Message{SID: sampleMessageSID, From: "+14155550123", To: "+14155550100", Status: messaging.MessageStatusReceived, Direction: messaging.InboundMessageDirection},
			Body:    body,
		},
	}
}

func TestTriggerMappingNamesOneFlowPerMessage(t *testing.T) {
	event := sampleTriggerEvent("hello")
	require.Equal(t, "twilio-inbound-message-twilio-message-"+sampleMessageSID, ResolveFlowID(event))
	input := MapToFlowInput(event)
	require.Equal(t, Input{TriggerEventID: event.ID, Received: event.Payload}, input)
	require.True(t, AcceptEveryInboundMessage(event))
	require.Equal(t, messaging.GetMessageInput{MessageSID: sampleMessageSID}, MapToGetMessageInput(input))
}

func TestValidateInputRejectsIncompleteOrOutboundMessages(t *testing.T) {
	require.NoError(t, validateInput(MapToFlowInput(sampleTriggerEvent("hello"))))
	missingID := MapToFlowInput(sampleTriggerEvent("hello"))
	missingID.TriggerEventID = " "
	require.ErrorContains(t, validateInput(missingID), "triggerEventId")
	missingSID := MapToFlowInput(sampleTriggerEvent("hello"))
	missingSID.Received.Message.SID = ""
	require.ErrorContains(t, validateInput(missingSID), "received.message.sid")
	outbound := MapToFlowInput(sampleTriggerEvent("hello"))
	outbound.Received.Message.Direction = "outbound-api"
	require.ErrorContains(t, validateInput(outbound), "inbound")
}

func TestOptOutKeywordsMatchTheWholeText(t *testing.T) {
	for _, body := range []string{"STOP", " stop ", "Unsubscribe", "quit"} {
		require.True(t, IsOptOutText(body), body)
	}
	for _, body := range []string{"", "please stop texting me", "START", "HELP"} {
		require.False(t, IsOptOutText(body), body)
	}
}

func TestTriggerBindingShowsOneGuidedReceivingNumber(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	binding := bindings[0]
	require.NoError(t, binding.Validate())
	require.Equal(t, messaging.MessageReceivedTriggerDefinition, binding.Definition)
	require.Equal(t, ConnectionName, binding.ConnectionName)
	require.Equal(t, MessageReceivedTriggerBinding, binding.BindingName)
	require.NotNil(t, binding.ConfigurationUI)
	require.Len(t, binding.ConfigurationUI.Units, 1, "the receiving number is the only visible Trigger field")
	unit := binding.ConfigurationUI.Units[0]
	require.Equal(t, messaging.UIUnitReceivingAddressInput, unit.UnitID)
	require.Equal(t, "Receiving number", unit.Label)
	require.False(t, unit.Required, "a blank number receives every inbound message")
	for _, guidance := range []string{"+14155550100", "whatsapp:+14155550100", "https://console.twilio.com/us1/develop/phone-numbers/manage/incoming",
		"Leave it blank", "every inbound message", "baseline", "Restart the Worker"} {
		require.Contains(t, unit.Description, guidance)
	}
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: messaging.UIReceivingAddressInputPortTo, JSONPointer: "/to"}}, unit.Bindings)
}

func TestFlowIdentitiesAndRegistration(t *testing.T) {
	flow := NewFlow(messaging.Connection{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordInboundMessageStepType, dex.GetFinalStepType[Input](recordInboundMessage{}))
	require.Equal(t, completeInboundMessageStepType, dex.GetFinalStepType[messaging.GetMessageResult](completeInboundMessage{}))
	wait, err := recordInboundMessage{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)

	client, err := messaging.New(messaging.Config{AccountSID: "AC" + "0123456789abcdef0123456789abcdef"}, sdkgo.StaticCredentialProvider[messaging.Credentials]{})
	require.NoError(t, err)
	connection, err := messaging.NewConnection(client, sdkgo.ConnectionRef{Provider: "twilio", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
}
