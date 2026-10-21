//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/internal/sqslive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// liveQueues is a real SQS API configured through SQS_CONNECTOR_TEST_* variables, such as Amazon SQS or ElasticMQ.
type liveQueues struct {
	sqslive.Server
	credentials sqs.Credentials
}

func requireLiveQueues(t *testing.T) liveQueues {
	t.Helper()
	server := sqslive.RequireServer(t)
	return liveQueues{Server: server, credentials: sqs.Credentials{
		AccessKeyID: sdkgo.NewSecretString(server.AccessKeyID), SecretAccessKey: sdkgo.NewSecretString(server.SecretAccessKey),
		SessionToken: sdkgo.NewSecretString(server.SessionToken),
	}}
}

func (queues liveQueues) client(t *testing.T, queueURL string) *sqs.Client {
	t.Helper()
	client, err := sqs.New(sqs.Config{Endpoint: queues.Endpoint, Region: queues.Region, DefaultQueueURL: queueURL, ReceiveWaitSeconds: 2},
		sdkgo.StaticCredentialProvider[sqs.Credentials]{testConnection: queues.credentials})
	require.NoError(t, err)
	return client
}

func receiveLive(t *testing.T, client *sqs.Client, context *fakeDexContext, input sqs.ReceiveMessagesInput) sqs.ReceiveMessagesResult {
	t.Helper()
	result, err := runMutation(t, context, client.ReceiveMessages(), input)
	require.NoError(t, err)
	return result
}

func TestLiveQueueReceivesReleasesAndAcknowledgesAMessage(t *testing.T) {
	queues := requireLiveQueues(t)
	queueURL := queues.CreateQueue(t, "dex-sqs-live", false, 60)
	client := queues.client(t, queueURL)
	messageID := queues.SendMessage(t, queueURL, `{"orderId":"1042","event":"paid"} é`, "")

	first := receiveLive(t, client, newDexContext("live-receive-"+queueURL), sqs.ReceiveMessagesInput{})
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, first.Branch, "%+v", first.Failure)
	message := first.Value.Messages[0]
	require.Equal(t, messageID, message.MessageID)
	require.Equal(t, `{"orderId":"1042","event":"paid"} é`, message.Body, "non-ASCII text survives the MD5 check")
	require.Equal(t, 1, message.ReceiveCount)
	require.False(t, message.SentAt.IsZero())
	require.Equal(t, sqs.MessageAttributeValue{DataType: "String", StringValue: "acme"}, message.MessageAttributes["tenant"])

	hidden := receiveLive(t, client, newDexContext("live-hidden-"+queueURL), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(1)})
	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, hidden.Branch, "an in-flight message is hidden")

	released, err := runMutation(t, newDexContext("live-release-"+queueURL), client.ChangeMessageVisibility(), sqs.ChangeMessageVisibilityInput{
		ReceiptHandle: message.ReceiptHandle, MessageID: message.MessageID,
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ChangeMessageVisibilityBranchChanged, released.Branch, "%+v", released.Failure)

	again := receiveLive(t, client, newDexContext("live-again-"+queueURL), sqs.ReceiveMessagesInput{})
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, again.Branch, "%+v", again.Failure)
	require.Equal(t, 2, again.Value.Messages[0].ReceiveCount)

	acknowledge := newDexContext("live-ack-" + queueURL)
	deleted, err := runMutation(t, acknowledge, client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: again.Value.Messages[0].ReceiptHandle})
	require.NoError(t, err)
	require.Equal(t, sqs.DeleteMessageBranchDeleted, deleted.Branch, "%+v", deleted.Failure)
	repeated, err := runMutation(t, acknowledge.nextAttempt(), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: again.Value.Messages[0].ReceiptHandle})
	require.NoError(t, err)
	t.Logf("repeated delete: branch=%s failure=%+v", repeated.Branch, repeated.Failure)
	require.Contains(t, []sdkgo.BranchID{sqs.DeleteMessageBranchDeleted, sqs.DeleteMessageBranchReceiptHandleInvalid}, repeated.Branch,
		"a server may forget the handle of a deleted message")

	empty := receiveLive(t, client, newDexContext("live-empty-"+queueURL), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(1)})
	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, empty.Branch)
	requireNoSecrets(t, []any{first, released, again, deleted, empty})
}

func TestLiveFIFOQueueRepeatsTheBatchForARepeatedReceiveAttempt(t *testing.T) {
	queues := requireLiveQueues(t)
	queueURL := queues.CreateQueue(t, "dex-sqs-live", true, 60)
	client := queues.client(t, queueURL)
	queues.SendMessage(t, queueURL, "first", "order-1042")
	queues.SendMessage(t, queueURL, "second", "order-1042")

	receive := newDexContext("live-fifo-" + queueURL)
	first := receiveLive(t, client, receive, sqs.ReceiveMessagesInput{MaxMessages: 1})
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, first.Branch, "%+v", first.Failure)
	require.True(t, first.Value.IsFIFO)
	require.Equal(t, "first", first.Value.Messages[0].Body)
	require.Equal(t, "order-1042", first.Value.Messages[0].MessageGroupID)
	retried := receiveLive(t, client, receive.nextAttempt(), sqs.ReceiveMessagesInput{MaxMessages: 1})
	t.Logf("repeated FIFO receive: branch=%s messages=%d", retried.Branch, len(retried.Value.Messages))
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, retried.Branch)
	require.Equal(t, first.Value.Messages[0].MessageID, retried.Value.Messages[0].MessageID, "the attempt ID returned the same message")
}

func TestLiveQueueClassifiesAMissingQueueAndAnInvalidHandle(t *testing.T) {
	queues := requireLiveQueues(t)
	queueURL := queues.CreateQueue(t, "dex-sqs-live", false, 60)
	client := queues.client(t, queueURL)
	missingURL := queueURL + "-missing"
	missing := receiveLive(t, client, newDexContext("live-missing"), sqs.ReceiveMessagesInput{QueueURL: missingURL, WaitTimeSeconds: seconds(0)})
	require.Equal(t, sqs.ReceiveMessagesBranchProviderRejected, missing.Branch, "%+v", missing.Failure)
	t.Logf("missing queue: %s", missing.Failure.Message)

	invalid, err := runMutation(t, newDexContext("live-invalid-handle"), client.DeleteMessage(), sqs.DeleteMessageInput{ReceiptHandle: "AQEBnot-a-receipt-handle"})
	require.NoError(t, err)
	t.Logf("invalid handle: branch=%s %+v", invalid.Branch, invalid.Failure)
	require.Equal(t, sqs.DeleteMessageBranchReceiptHandleInvalid, invalid.Branch)
	requireNoSecrets(t, []any{missing, invalid})
}
