//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/sqsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestQueueConsumerProcessesMockPaidOrderWithRealDex(t *testing.T) {
	mock := sqsmock.New(t, ConnectionName)
	flow, harness := newQueueConsumerHarnessWithConnection(t, mock.Connection())
	result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "mock-paid-"+uniqueSuffix(), Input{})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusProcessed, result.outcome.Status)
	require.Equal(t, &OrderEvent{OrderID: "1042", Event: "paid"}, result.outcome.OrderEvent)
	require.True(t, result.outcome.IsAcknowledgementConfirmed)
	visibilityTimeoutSeconds := ProcessingVisibilityTimeoutSeconds
	receives := mock.ReceiveMessages().Calls()
	require.Len(t, receives, 1)
	require.Equal(t, sqs.ReceiveMessagesInput{MaxMessages: 1, VisibilityTimeoutSeconds: &visibilityTimeoutSeconds}, receives[0].Input)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, receives[0].Branch)
	deletes := mock.DeleteMessage().Calls()
	require.Len(t, deletes, 1)
	require.Equal(t, sqs.DeleteMessageInput{
		QueueURL:      "https://sqs.eu-west-1.amazonaws.com/123456789012/orders",
		ReceiptHandle: "receipt-order-1042", MessageID: "order-1042",
	}, deletes[0].Input)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, deletes[0].Branch)
	require.Empty(t, mock.ChangeMessageVisibility().Calls())
}

func TestQueueConsumerReleasesMockInvalidOrderWithRealDex(t *testing.T) {
	mock := sqsmock.New(t, ConnectionName)
	mock.ReceiveMessages().Respond(sqsmock.ReceiveMessagesReceived(sqs.ReceivedMessages{
		QueueURL: "https://sqs.eu-west-1.amazonaws.com/123456789012/orders",
		Messages: []sqs.Message{{MessageID: "order-1043", ReceiptHandle: "receipt-order-1043", Body: "invalid order", ReceiveCount: 1}},
	}, nil))
	mock.ChangeMessageVisibility().Respond(sqsmock.ChangeMessageVisibilityChanged(sqs.ChangedMessageVisibility{
		QueueURL: "https://sqs.eu-west-1.amazonaws.com/123456789012/orders", MessageID: "order-1043",
	}, nil))
	flow, harness := newQueueConsumerHarnessWithConnection(t, mock.Connection())
	result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "mock-invalid-"+uniqueSuffix(), Input{})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusRejected, result.outcome.Status)
	require.Equal(t, "the message body is not a JSON order event", result.outcome.RejectionReason)
	require.True(t, result.outcome.IsAcknowledgementConfirmed)
	require.Len(t, mock.ReceiveMessages().Calls(), 1)
	require.Empty(t, mock.DeleteMessage().Calls())
	releases := mock.ChangeMessageVisibility().Calls()
	require.Len(t, releases, 1)
	require.Equal(t, sqs.ChangeMessageVisibilityInput{
		QueueURL:      "https://sqs.eu-west-1.amazonaws.com/123456789012/orders",
		ReceiptHandle: "receipt-order-1043", MessageID: "order-1043", VisibilityTimeoutSeconds: 0,
	}, releases[0].Input)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchChanged, releases[0].Branch)
}
