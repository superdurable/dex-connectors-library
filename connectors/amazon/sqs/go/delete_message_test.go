// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDeleteMessageAcknowledgesTheMessageSoItIsNeverDeliveredAgain(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]

	result, err := runMutation(t, newDexContext("ack"), client.DeleteMessage(), sqs.DeleteMessageInput{
		ReceiptHandle: received.ReceiptHandle, MessageID: received.MessageID,
	})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, result.Branch)
	require.Equal(t, sqs.DeletedMessage{QueueURL: queues.QueueURL(ordersQueue), MessageID: messageID}, result.Value)
	require.Equal(t, queues.QueueURL(ordersQueue)+"#"+messageID, result.Receipt.ProviderObjectID)
	state, _ := queues.Message(ordersQueue, messageID)
	require.True(t, state.IsDeleted)
	var request map[string]any
	require.NoError(t, json.Unmarshal(queues.Requests()[1].Body, &request))
	require.Equal(t, map[string]any{"QueueUrl": queues.QueueURL(ordersQueue), "ReceiptHandle": received.ReceiptHandle}, request,
		"the message ID labels the Result and is not sent")

	queues.ExpireVisibility(ordersQueue)
	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, receiveOne(t, client, "after-ack").Branch)
}

func TestARepeatedDeleteAttemptConverges(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]
	context := newDexContext("ack")
	input := sqs.DeleteMessageInput{ReceiptHandle: received.ReceiptHandle}
	first, err := runMutation(t, context, client.DeleteMessage(), input)
	require.NoError(t, err)
	retried, err := runMutation(t, context.nextAttempt(), client.DeleteMessage(), input)
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, first.Branch)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, retried.Branch, "deleting an already deleted message succeeds")
	require.Equal(t, 0, queues.RemainingMessageCount(ordersQueue))
}

// TestALostDeleteResponseIsRetriedAndConverges: the first response arrives after the client gave up.
func TestALostDeleteResponseIsRetriedAndConverges(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	received := receiveOne(t, client, "receive").Value.Messages[0]
	queues.DelayResponses("DeleteMessage", 2*time.Second)
	impatient, err := sqs.New(sqs.Config{Endpoint: queues.URL, Region: testRegion, DefaultQueueURL: queues.QueueURL(ordersQueue)},
		staticCredentials(""), sqs.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	require.NoError(t, err)
	context := newDexContext("lost-ack")
	_, err = runMutation(t, context, impatient.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: received.ReceiptHandle})
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Eventually(t, func() bool { return queues.RemainingMessageCount(ordersQueue) == 0 }, 5*time.Second, 20*time.Millisecond)

	retried, err := runMutation(t, context.nextAttempt(), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: received.ReceiptHandle})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, retried.Branch)
}

func TestDeleteMessageSelectsReceiptHandleInvalidForAnUnknownOrExpiredHandle(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	result, err := runMutation(t, newDexContext("unknown"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: "AQEB-never-issued"})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchReceiptHandleInvalid, result.Branch)
	require.Equal(t, "SQS no longer accepts the receipt handle for the message deletion with HTTP 400 ReceiptHandleIsInvalid; the message may be delivered again",
		result.Failure.Message)

	fifo := newFakeClient(t, queues, func(config *sqs.Config) { config.DefaultQueueURL = queues.QueueURL(ordersFIFOQueue) })
	queues.Send(ordersFIFOQueue, sqsfake.SentMessage{Body: orderCreatedBody, MessageGroupID: "order-1042"})
	expired := receiveOne(t, fifo, "fifo-first").Value.Messages[0]
	queues.ExpireVisibility(ordersFIFOQueue)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, receiveOne(t, fifo, "fifo-again").Branch)
	result, err = runMutation(t, newDexContext("expired"), fifo.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: expired.ReceiptHandle})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchReceiptHandleInvalid, result.Branch, "an expired FIFO handle is InvalidParameterValue about the handle")
	require.Contains(t, result.Failure.Message, "InvalidParameterValue")
	require.Equal(t, 1, queues.RemainingMessageCount(ordersFIFOQueue))
	requireNoSecrets(t, result)
}

// TestAnOutdatedStandardHandleIsAcceptedWithoutDeletingTheMessage models the documented Amazon SQS behavior.
func TestAnOutdatedStandardHandleIsAcceptedWithoutDeletingTheMessage(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	outdated := receiveOne(t, client, "first").Value.Messages[0]
	queues.ExpireVisibility(ordersQueue)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, receiveOne(t, client, "second").Branch)

	result, err := runMutation(t, newDexContext("outdated"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: outdated.ReceiptHandle})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, result.Branch)
	state, _ := queues.Message(ordersQueue, messageID)
	require.False(t, state.IsDeleted, "only the latest delivery's handle deletes the message")
}

func TestDeleteMessageRejectsInvalidInputWithoutARequest(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	for name, input := range map[string]sqs.DeleteMessageInput{
		"a blank receipt handle":       {},
		"a receipt handle with spaces": {ReceiptHandle: "AQEB handle"},
		"a message ID with a newline":  {ReceiptHandle: "AQEB", MessageID: "m\n1"},
		"an invalid queue URL":         {ReceiptHandle: "AQEB", QueueURL: "sqs://orders"},
	} {
		result, err := runMutation(t, newDexContext("invalid"), client.DeleteMessage(), input)
		require.NoError(t, err, name)
		require.Equal(t, sqs.DeleteMessageBranchDefect, result.Branch, name)
	}
	require.Zero(t, queues.RequestCount(""))
}
