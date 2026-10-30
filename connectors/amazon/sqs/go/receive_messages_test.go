// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const orderCreatedBody = `{"orderId":"1042","event":"created"}`

func TestReceiveMessagesReturnsVerifiedBodiesAttributesAndReceiptHandles(t *testing.T) {
	queues := newFakeQueues(t)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody, MessageAttributes: map[string]sqsfake.MessageAttribute{
		"tenant":   {DataType: "String", StringValue: "acme"},
		"priority": {DataType: "Number.int", StringValue: "2"},
		"checksum": {DataType: "Binary", BinaryValue: []byte{0xde, 0xad}},
	}})
	result, err := runMutation(t, newDexContext("receive"), newFakeClient(t, queues).ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, result.Branch)
	require.Equal(t, queues.QueueURL(ordersQueue), result.Value.QueueURL)
	require.False(t, result.Value.IsFIFO)
	require.Len(t, result.Value.Messages, 1)
	message := result.Value.Messages[0]
	state, _ := queues.Message(ordersQueue, messageID)
	require.Equal(t, messageID, message.MessageID)
	require.Equal(t, state.ReceiptHandle, message.ReceiptHandle)
	require.Equal(t, orderCreatedBody, message.Body)
	require.Equal(t, 1, message.ReceiveCount)
	require.False(t, message.SentAt.IsZero())
	require.False(t, message.FirstReceivedAt.IsZero())
	require.Equal(t, map[string]sqs.MessageAttributeValue{
		"tenant":   {DataType: "String", StringValue: "acme"},
		"priority": {DataType: "Number.int", StringValue: "2"},
		"checksum": {DataType: "Binary", BinaryValue: []byte{0xde, 0xad}},
	}, message.MessageAttributes)
	require.Equal(t, queues.QueueURL(ordersQueue), result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.ProviderRequestID)

	var request map[string]any
	require.NoError(t, json.Unmarshal(queues.Requests()[0].Body, &request))
	require.Equal(t, float64(10), request["MaxNumberOfMessages"], "zero maxMessages uses the connection's maxMessagesPerReceive")
	require.Equal(t, []any{"All"}, request["MessageAttributeNames"])
	require.NotContains(t, request, "VisibilityTimeout", "nil keeps the queue's own visibility timeout")
	require.NotContains(t, request, "ReceiveRequestAttemptId", "a standard queue takes no receive-attempt ID")
	requireNoSecrets(t, result)
}

func TestReceiveMessagesSelectsEmptyWhenNoMessageArrives(t *testing.T) {
	queues := newFakeQueues(t)
	result := receiveOne(t, newFakeClient(t, queues), "empty")
	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, result.Branch)
	require.Empty(t, result.Value.Messages)
	require.Nil(t, result.Failure, "an empty queue is not a failure")
}

// TestReceiveMessagesLongPollReturnsWhenAMessageArrives: elapsed time is the behavior under test.
func TestReceiveMessagesLongPollReturnsWhenAMessageArrives(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	time.AfterFunc(300*time.Millisecond, func() { queues.Send(ordersQueue, sqsfake.SentMessage{Body: "late"}) })
	started := time.Now()
	result, err := runMutation(t, newDexContext("long-poll"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(5)})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, result.Branch)
	require.Equal(t, "late", result.Value.Messages[0].Body)
	require.Less(t, time.Since(started), 4*time.Second, "a long poll returns as soon as a message arrives")
	var request map[string]any
	require.NoError(t, json.Unmarshal(queues.Requests()[0].Body, &request))
	require.Equal(t, float64(5), request["WaitTimeSeconds"])
}

func TestReceivedMessagesStayHiddenUntilTheirVisibilityTimeoutEnds(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	messageID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: orderCreatedBody})
	first, err := runMutation(t, newDexContext("first"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{
		WaitTimeSeconds: seconds(0), VisibilityTimeoutSeconds: seconds(120),
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, first.Branch)
	var request map[string]any
	require.NoError(t, json.Unmarshal(queues.Requests()[0].Body, &request))
	require.Equal(t, float64(120), request["VisibilityTimeout"])

	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, receiveOne(t, client, "hidden").Branch, "another consumer cannot see an in-flight message")
	queues.ExpireVisibility(ordersQueue)
	redelivered := receiveOne(t, client, "redelivered")
	require.Equal(t, sqs.ReceiveMessagesBranchReceived, redelivered.Branch)
	require.Equal(t, messageID, redelivered.Value.Messages[0].MessageID)
	require.Equal(t, 2, redelivered.Value.Messages[0].ReceiveCount)
	require.NotEqual(t, first.Value.Messages[0].ReceiptHandle, redelivered.Value.Messages[0].ReceiptHandle, "each delivery has its own handle")
}

// TestARepeatedFIFOReceiveAttemptGetsTheSameBatch: the receive-attempt ID is the Step execution's key.
func TestARepeatedFIFOReceiveAttemptGetsTheSameBatch(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues, func(config *sqs.Config) { config.DefaultQueueURL = queues.QueueURL(ordersFIFOQueue) })
	queues.Send(ordersFIFOQueue, sqsfake.SentMessage{Body: "first", MessageGroupID: "order-1042"})
	queues.Send(ordersFIFOQueue, sqsfake.SentMessage{Body: "second", MessageGroupID: "order-1042"})
	context := newDexContext("fifo")
	first, err := runMutation(t, context, client.ReceiveMessages(), sqs.ReceiveMessagesInput{MaxMessages: 1, WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	retried, err := runMutation(t, context.nextAttempt(), client.ReceiveMessages(), sqs.ReceiveMessagesInput{MaxMessages: 1, WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)

	require.True(t, first.Value.IsFIFO)
	require.Equal(t, first.Value, retried.Value, "a lost FIFO response is recovered, not skipped")
	message := first.Value.Messages[0]
	require.Equal(t, "first", message.Body)
	require.Equal(t, "order-1042", message.MessageGroupID)
	require.NotEmpty(t, message.SequenceNumber)
	require.NotEmpty(t, message.MessageDeduplicationID)
	requests := queues.Requests()
	var firstRequest, retriedRequest map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &firstRequest))
	require.NoError(t, json.Unmarshal(requests[1].Body, &retriedRequest))
	require.Equal(t, string(first.Receipt.IdempotencyKey), firstRequest["ReceiveRequestAttemptId"])
	require.Equal(t, firstRequest["ReceiveRequestAttemptId"], retriedRequest["ReceiveRequestAttemptId"])

	next := receiveOne(t, client, "fifo-next")
	require.Equal(t, sqs.ReceiveMessagesBranchEmpty, next.Branch, "the group stays blocked while its first message is in flight")
}

// TestARepeatedStandardReceiveAttemptLeavesEarlierMessagesHiddenNotLost records the standard-queue trade-off.
func TestARepeatedStandardReceiveAttemptLeavesEarlierMessagesHiddenNotLost(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	firstID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: "first"})
	secondID := queues.Send(ordersQueue, sqsfake.SentMessage{Body: "second"})
	context := newDexContext("standard-retry")
	first, err := runMutation(t, context, client.ReceiveMessages(), sqs.ReceiveMessagesInput{MaxMessages: 1, WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	retried, err := runMutation(t, context.nextAttempt(), client.ReceiveMessages(), sqs.ReceiveMessagesInput{MaxMessages: 1, WaitTimeSeconds: seconds(0)})
	require.NoError(t, err)
	require.Equal(t, firstID, first.Value.Messages[0].MessageID)
	require.Equal(t, secondID, retried.Value.Messages[0].MessageID)
	queues.ExpireVisibility(ordersQueue)
	require.Equal(t, firstID, receiveOne(t, client, "after-timeout").Value.Messages[0].MessageID, "the unprocessed message returns")
}

func TestReceiveMessagesRejectsAnUntrustworthyBatch(t *testing.T) {
	validMessage := map[string]any{
		"MessageId": "m-1", "ReceiptHandle": "AQEB-handle", "Body": "hello", "MD5OfBody": "5d41402abc4b2a76b9719d911017c592",
		"Attributes": map[string]string{"ApproximateReceiveCount": "1", "SentTimestamp": "1767225600000"},
	}
	for name, test := range map[string]struct {
		mutate  func(map[string]any)
		raw     string
		kind    sdkgo.FailureKind
		message string
	}{
		"a body that does not match its digest": {mutate: func(message map[string]any) { message["Body"] = "hellO" }, kind: sdkgo.FailureProtocol, message: "MD5"},
		"a missing receipt handle":              {mutate: func(message map[string]any) { delete(message, "ReceiptHandle") }, kind: sdkgo.FailureProtocol},
		"a missing message ID":                  {mutate: func(message map[string]any) { message["MessageId"] = "" }, kind: sdkgo.FailureProtocol},
		"a missing receive count":               {mutate: func(message map[string]any) { delete(message, "Attributes") }, kind: sdkgo.FailureProtocol},
		"a timestamp that is not milliseconds": {mutate: func(message map[string]any) {
			message["Attributes"] = map[string]string{"ApproximateReceiveCount": "1", "SentTimestamp": "yesterday"}
		}, kind: sdkgo.FailureProtocol},
		"a body that repeats the secret": {mutate: func(message map[string]any) {
			message["Body"], message["MD5OfBody"] = testSecretAccessKey, "ignored"
		}, kind: sdkgo.FailureProtocol, message: "credentials"},
		"more messages than requested": {raw: "two", kind: sdkgo.FailureProtocol, message: "more messages"},
		"a document that is not JSON":  {raw: "<ReceiveMessageResponse/>", kind: sdkgo.FailureProtocol},
		"a batch above maxResponseBytes": {mutate: func(message map[string]any) {
			body := strings.Repeat("a", 2048)
			message["Body"], message["MD5OfBody"] = body, "ignored"
		}, kind: sdkgo.FailureResponseTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			server := newStaticServer(t, func(response http.ResponseWriter, _ *http.Request) {
				message := map[string]any{}
				for key, value := range validMessage {
					message[key] = value
				}
				var body []byte
				switch {
				case test.raw == "two":
					body, _ = json.Marshal(map[string]any{"Messages": []any{message, message}})
				case test.raw != "":
					body = []byte(test.raw)
				default:
					test.mutate(message)
					body, _ = json.Marshal(map[string]any{"Messages": []any{message}})
				}
				_, _ = response.Write(body)
			})
			client, err := sqs.New(sqs.Config{Endpoint: server.URL, Region: testRegion, MaxResponseBytes: 1024}, staticCredentials(""))
			require.NoError(t, err)
			result, err := runMutation(t, newDexContext("invalid"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{
				QueueURL: server.URL + "/123456789012/orders", MaxMessages: 1, WaitTimeSeconds: seconds(0),
			})
			require.NoError(t, err)
			require.Equal(t, sqs.ReceiveMessagesBranchInvalidResponse, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.message)
			require.Empty(t, result.Value.Messages)
			requireNoSecrets(t, result)
		})
	}
}

func TestReceiveMessagesRejectsInvalidInputWithoutARequest(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	for name, input := range map[string]sqs.ReceiveMessagesInput{
		"more than ten messages":           {MaxMessages: 11},
		"a negative message count":         {MaxMessages: -1},
		"a wait above 20 seconds":          {WaitTimeSeconds: seconds(21)},
		"a negative wait":                  {WaitTimeSeconds: seconds(-1)},
		"a visibility above 12 hours":      {VisibilityTimeoutSeconds: seconds(43201)},
		"a queue URL without a queue name": {QueueURL: queues.URL + "/123456789012"},
		"a queue URL with a query":         {QueueURL: queues.QueueURL(ordersQueue) + "?Action=ReceiveMessage"},
		"a queue name with a dot":          {QueueURL: queues.URL + "/123456789012/orders.v2"},
		"a queue URL with user info":       {QueueURL: "https://user:pass@sqs.eu-west-1.amazonaws.com/123456789012/orders"},
	} {
		result, err := runMutation(t, newDexContext("invalid"), client.ReceiveMessages(), input)
		require.NoError(t, err, name)
		require.Equal(t, sqs.ReceiveMessagesBranchDefect, result.Branch, name)
	}
	withoutDefault := newFakeClient(t, queues, func(config *sqs.Config) { config.DefaultQueueURL = "" })
	result, err := runMutation(t, newDexContext("no-queue"), withoutDefault.ReceiveMessages(), sqs.ReceiveMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "defaultQueueUrl")
	require.Zero(t, queues.RequestCount(""))
}

func TestReceiveMessagesRetriesATransportFailureBecauseMessagesReturnAfterTheirTimeout(t *testing.T) {
	queues := newFakeQueues(t)
	client := newFakeClient(t, queues)
	queues.Close()
	_, err := runMutation(t, newDexContext("closed"), client.ReceiveMessages(), sqs.ReceiveMessagesInput{WaitTimeSeconds: seconds(0)})
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestReceiveMessagesFromAMissingQueueIsRejectedNotEmpty(t *testing.T) {
	queues := newFakeQueues(t)
	result, err := runMutation(t, newDexContext("missing"), newFakeClient(t, queues).ReceiveMessages(), sqs.ReceiveMessagesInput{
		QueueURL: queues.QueueURL("missing-queue"), WaitTimeSeconds: seconds(0),
	})
	require.NoError(t, err)
	require.Equal(t, sqs.ReceiveMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, "SQS rejected the receive with HTTP 400 QueueDoesNotExist because the queue does not exist; check queueUrl and region", result.Failure.Message)
	requireNoSecrets(t, result)
}
