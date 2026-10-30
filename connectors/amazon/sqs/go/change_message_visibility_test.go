// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/internal/sqsfake"
)

func TestAZeroVisibilityTimeoutReleasesTheMessageToTheNextReceive(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]

	result, err := runMutation(t, newDexContext("release"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{
		ReceiptHandle: received.ReceiptHandle, MessageID: received.MessageID, VisibilityTimeoutSeconds: 0,
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchChanged, result.Branch)
	require.Equal(t, messageID, result.Value.MessageID)
	require.Zero(t, result.Value.VisibilityTimeoutSeconds)
	require.Equal(t, result.Receipt.ObservedAt, result.Value.VisibleAfter)

	again := receiveOne(t, client, "again")
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, again.Branch, "a released message is visible at once")
	require.Equal(t, 2, again.Value.Messages[0].ReceiveCount)
}

func TestALongerVisibilityTimeoutKeepsTheMessageHidden(t *testing.T) {
	queues := newFakeQueues(t, func(config *sqsfake.Config) { config.VisibilityTimeout = time.Second })
	client := newFakeClient(t, queues)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]

	result, err := runMutation(t, newDexContext("extend"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{
		ReceiptHandle: received.ReceiptHandle, VisibilityTimeoutSeconds: 600,
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchChanged, result.Branch)
	require.Equal(t, result.Receipt.ObservedAt.Add(10*time.Minute), result.Value.VisibleAfter)
	state, _ := queues.Message(ordersQueue, messageID)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), state.VisibleAt, time.Minute)
}

func TestChangeMessageVisibilitySelectsReceiptHandleInvalidForAMessageNoLongerInFlight(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]
	queues.ExpireVisibility(ordersQueue)

	result, err := runMutation(t, newDexContext("late"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{
		ReceiptHandle: received.ReceiptHandle, VisibilityTimeoutSeconds: 60,
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchReceiptHandleInvalid, result.Branch)
	require.Contains(t, result.Failure.Message, "MessageNotInflight")
	requireNoSecrets(t, result)
}

func TestChangeMessageVisibilityRejectsATimeoutOutsideTheSQSRangeWithoutARequest(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	for _, timeout := range []int{-1, 43201} {
		result, err := runMutation(t, newDexContext("invalid"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{
			ReceiptHandle: "AQEB-handle", VisibilityTimeoutSeconds: timeout,
		})
		require.NoError(t, err)
		require.Equal(t, sqs.ChangeMessageVisibilityBranchDefect, result.Branch)
	}
	require.Zero(t, queues.RequestCount(""))
}
