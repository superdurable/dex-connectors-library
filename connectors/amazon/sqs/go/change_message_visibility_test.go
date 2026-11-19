// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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

func TestARepeatedReleaseThatFindsTheMessageReleasedSelectsChanged(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]
	queues.ExpireVisibility(ordersQueue)
	release := sqs.ChangeMessageVisibilityInput{ReceiptHandle: received.ReceiptHandle, VisibilityTimeoutSeconds: 0}

	repeated, err := runMutation(t, newDexContext("release").nextAttempt(), client.ChangeMessageVisibility(), release)
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchChanged, repeated.Branch, "an earlier attempt may have released it")

	first, err := runMutation(t, newDexContext("first"), client.ChangeMessageVisibility(), release)
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchReceiptHandleInvalid, first.Branch)
	extend := release
	extend.VisibilityTimeoutSeconds = 60
	_, err = runMutation(t, newDexContext("extend").nextAttempt(), client.ChangeMessageVisibility(), extend)
	requireRetry(t, err, sdkgo.FailureNotFound)
}

func TestAcknowledgementsRetryAnUnlistedStatusBecauseItsOutcomeIsUnknown(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusConflict, http.StatusTooEarly, http.StatusUnavailableForLegalReasons} {
		queues.Intercept(func(sqsfake.Request) *sqsfake.Response {
			return &sqsfake.Response{StatusCode: status, Code: "Unlisted"}
		})
		_, err := runMutation(t, newDexContext("delete"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: "AQEB-handle"})
		requireRetry(t, err, sdkgo.FailureAvailability)
		_, err = runMutation(t, newDexContext("change"), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{ReceiptHandle: "AQEB-handle", VisibilityTimeoutSeconds: 60})
		requireRetry(t, err, sdkgo.FailureAvailability)
	}
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
