// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestParseOrderEventAcceptsOnlyValidOrderEvents(t *testing.T) {
	event, err := ParseOrderEvent(`{"orderId":"1042","event":"paid"}`)
	require.NoError(t, err)
	require.Equal(t, OrderEvent{OrderID: "1042", Event: "paid"}, event)
	for name, body := range map[string]string{
		"a body that is not JSON":   "order 1042 paid",
		"a blank order ID":          `{"orderId":"","event":"paid"}`,
		"an order ID with a slash":  `{"orderId":"10/42","event":"paid"}`,
		"an unknown event":          `{"orderId":"1042","event":"refunded"}`,
		"a JSON array":              `[{"orderId":"1042","event":"paid"}]`,
		"an overlong order ID text": `{"orderId":"` + string(make([]byte, 65)) + `","event":"paid"}`,
	} {
		_, err := ParseOrderEvent(body)
		require.Error(t, err, name)
	}
}

func TestConnectorMappersReceiveOneMessageAndAcknowledgeTheSameDelivery(t *testing.T) {
	received := MapToReceiveMessagesInput(ConsumerRequest{QueueURL: "https://sqs.eu-west-1.amazonaws.com/123456789012/orders"})
	require.Equal(t, "https://sqs.eu-west-1.amazonaws.com/123456789012/orders", received.QueueURL)
	require.Equal(t, 1, received.MaxMessages)
	require.Nil(t, received.WaitTimeSeconds, "the connection's receiveWaitSeconds long-polls")
	require.Equal(t, ProcessingVisibilityTimeoutSeconds, *received.VisibilityTimeoutSeconds)

	receipt := MessageReceipt{QueueURL: "https://sqs.eu-west-1.amazonaws.com/123456789012/orders", MessageID: "m-1", ReceiptHandle: "AQEB-handle"}
	require.Equal(t, sqs.DeleteMessageInput{QueueURL: receipt.QueueURL, ReceiptHandle: "AQEB-handle", MessageID: "m-1"}, MapToDeleteMessageInput(receipt))
	require.Equal(t, sqs.ChangeMessageVisibilityInput{QueueURL: receipt.QueueURL, ReceiptHandle: "AQEB-handle", MessageID: "m-1", VisibilityTimeoutSeconds: 0},
		MapToReleaseMessageInput(receipt))
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sqs.Connection{})))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordConsumerRequest{}))
	require.Equal(t, inspectOrderEventStepType, dex.GetFinalStepType[sqs.ReceiveMessagesResult](inspectOrderEvent{}))
	require.Equal(t, recordAcknowledgedStepType, dex.GetFinalStepType[sqs.DeleteMessageResult](recordAcknowledgedOrderEvent{}))
	require.Equal(t, recordReleasedEventStepType, dex.GetFinalStepType[sqs.ChangeMessageVisibilityResult](recordReleasedOrderEvent{}))
	wait, err := recordConsumerRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := sqs.New(sqs.Config{}, sdkgo.StaticCredentialProvider[sqs.Credentials]{})
	require.NoError(t, err)
	connection, err := sqs.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := sqs.NewConnection(client, sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
