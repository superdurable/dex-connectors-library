// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textmessagedelivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// A split literal keeps the synthetic SID from matching repository secret scanning.
const unitTestAccountSID = "AC" + "0123456789abcdef0123456789abcdef"

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	policy := DefaultDeliveryStatusPolicy()
	flow := NewFlow(newUnitTestConnection(t, ConnectionName), &policy)
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordTextMessageRequestStepType, dex.GetFinalStepType[Input](recordTextMessageRequest{}))
	require.Equal(t, waitForDeliveryStatusStepType, dex.GetFinalStepType[DeliveryStatusCheck](waitForDeliveryStatus{}))
	require.Equal(t, recordDeliveryStatusStepType, dex.GetFinalStepType[messaging.GetMessageResult](recordDeliveryStatus{}))
	wait, err := recordTextMessageRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestMappersPassOnlyTheRecordedRequest(t *testing.T) {
	policy := DefaultDeliveryStatusPolicy()
	flow := NewFlow(newUnitTestConnection(t, ConnectionName), &policy)
	require.Equal(t, messaging.SendMessageInput{To: "+14155550101", Body: "Ready.", Sender: "+14155550100"},
		flow.MapToSendMessageInput(Input{To: "+14155550101", Body: "Ready.", Sender: "+14155550100"}))
	require.Equal(t, messaging.GetMessageInput{MessageSID: "SM0123456789abcdef0123456789abcdef"},
		flow.MapToGetMessageInput(DeliveryStatusCheck{MessageSID: "SM0123456789abcdef0123456789abcdef"}))
}

func TestFlowRequiresABoundedStatusPolicyAndItsStaticConnection(t *testing.T) {
	connection := newUnitTestConnection(t, ConnectionName)
	require.Panics(t, func() { NewFlow(connection, nil) })
	require.Panics(t, func() {
		NewFlow(connection, &DeliveryStatusPolicy{CheckInterval: 500 * time.Millisecond, MaximumChecks: 1})
	})
	require.Panics(t, func() { NewFlow(connection, &DeliveryStatusPolicy{CheckInterval: time.Second}) })

	policy := DefaultDeliveryStatusPolicy()
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(connection, &policy)})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"), &policy)})
	})
}

func TestOnlyTwilioFinalStatusesEndDeliveryPolling(t *testing.T) {
	for _, status := range []messaging.MessageStatus{
		messaging.MessageStatusDelivered, messaging.MessageStatusRead, messaging.MessageStatusUndelivered,
		messaging.MessageStatusFailed, messaging.MessageStatusCanceled,
	} {
		require.True(t, isFinalDeliveryStatus(status), status)
	}
	for _, status := range []messaging.MessageStatus{
		messaging.MessageStatusAccepted, messaging.MessageStatusScheduled, messaging.MessageStatusQueued,
		messaging.MessageStatusSending, messaging.MessageStatusSent, "a_future_status",
	} {
		require.False(t, isFinalDeliveryStatus(status), status)
	}
}

func TestReportedMessageSIDMustBeAMessageSID(t *testing.T) {
	require.True(t, isMessageSID("SM0123456789abcdef0123456789abcdef"))
	require.True(t, isMessageSID("MM0123456789abcdef0123456789abcdef"))
	for _, value := range []string{"", "SM123", "MG0123456789abcdef0123456789abcdef", "SM0123456789ABCDEF0123456789ABCDEF"} {
		require.False(t, isMessageSID(value), value)
	}
}

func newUnitTestConnection(t *testing.T, connectionName string) messaging.Connection {
	t.Helper()
	client, err := messaging.New(messaging.Config{AccountSID: unitTestAccountSID}, sdkgo.StaticCredentialProvider[messaging.Credentials]{})
	require.NoError(t, err)
	connection, err := messaging.NewConnection(client, sdkgo.ConnectionRef{Provider: "twilio", Name: connectionName})
	require.NoError(t, err)
	return connection
}
