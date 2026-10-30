//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go/internal/sqsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAccessKeyID     = "queue-consumer-access-key"
	integrationSecretAccessKey = "queue-consumer-integration-secret"
	integrationRegion          = "eu-west-1"
	integrationQueue           = "orders"
	// slowResponseDelay exercises a healthy long poll.
	slowResponseDelay = 9 * time.Second
)

func TestQueueConsumerProcessesAndAcknowledgesAValidOrderEventWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	messageID := queues.Send(integrationQueue, sqsfake.SentMessage{Body: `{"orderId":"1042","event":"paid"}`})
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))
	ctx := integrationContext(t)

	result := runQueueConsumer(t, ctx, harness.client, flow, "processed-"+uniqueSuffix(), Input{})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusProcessed, result.outcome.Status)
	require.Equal(t, &OrderEvent{OrderID: "1042", Event: "paid"}, result.outcome.OrderEvent)
	require.Equal(t, messageID, result.outcome.Message.MessageID)
	require.Equal(t, 1, result.outcome.Message.ReceiveCount)
	require.True(t, result.outcome.IsAcknowledgementConfirmed)
	state, _ := queues.Message(integrationQueue, messageID)
	require.True(t, state.IsDeleted, "the acknowledged message is gone from the queue")
	require.Equal(t, 1, queues.RequestCount("ReceiveMessage"))
	require.Equal(t, 1, queues.RequestCount("DeleteMessage"))
	require.Zero(t, queues.SignatureErrorCount())
}

func TestQueueConsumerReleasesAnInvalidMessageForRedeliveryWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	messageID := queues.Send(integrationQueue, sqsfake.SentMessage{Body: "not an order event"})
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))
	ctx := integrationContext(t)

	result := runQueueConsumer(t, ctx, harness.client, flow, "rejected-"+uniqueSuffix(), Input{})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusRejected, result.outcome.Status)
	require.Equal(t, "the message body is not a JSON order event", result.outcome.RejectionReason)
	require.True(t, result.outcome.IsAcknowledgementConfirmed)
	state, _ := queues.Message(integrationQueue, messageID)
	require.False(t, state.IsDeleted)
	require.False(t, time.Now().Before(state.VisibleAt), "the released message is visible again at once")
	require.Zero(t, queues.RequestCount("DeleteMessage"), "a rejected message is never acknowledged")

	again := runQueueConsumer(t, ctx, harness.client, flow, "rejected-again-"+uniqueSuffix(), Input{})
	require.Equal(t, StatusRejected, again.outcome.Status)
	require.Equal(t, 2, again.outcome.Message.ReceiveCount, "the redrive policy counts each delivery")
}

func TestQueueConsumerCompletesWithNoMessageWhenTheQueueIsEmptyWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))
	result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "empty-"+uniqueSuffix(), Input{})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusNoMessage, result.outcome.Status)
	require.Nil(t, result.outcome.Message)
}

func TestQueueConsumerInvalidReceiptCompletesWithoutConfirmationWithRealDex(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		body           string
		action         string
		expectedStatus Status
	}{
		{name: "delete", body: `{"orderId":"1042","event":"paid"}`, action: "DeleteMessage", expectedStatus: StatusProcessed},
		{name: "release", body: "not an order event", action: "ChangeMessageVisibility", expectedStatus: StatusRejected},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			queues := newIntegrationQueues(t)
			queues.Intercept(func(request sqsfake.Request) *sqsfake.Response {
				if request.Action == scenario.action {
					return &sqsfake.Response{StatusCode: http.StatusBadRequest, Code: "MessageNotInflight"}
				}
				return nil
			})
			queues.Send(integrationQueue, sqsfake.SentMessage{Body: scenario.body})
			flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))

			result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "invalid-"+scenario.name+"-"+uniqueSuffix(), Input{})

			require.Equal(t, dex.FlowCompleted, result.status)
			require.Equal(t, scenario.expectedStatus, result.outcome.Status)
			require.False(t, result.outcome.IsAcknowledgementConfirmed)
			require.Equal(t, 1, queues.RequestCount(scenario.action))
		})
	}
}

// TestQueueConsumerLongPollPastTheAsyncPhaseReceivesOnceWithRealDex: sync durability keeps one receive in flight.
func TestQueueConsumerLongPollPastTheAsyncPhaseReceivesOnceWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 20))
	time.AfterFunc(slowResponseDelay, func() {
		queues.Send(integrationQueue, sqsfake.SentMessage{Body: `{"orderId":"2001","event":"created"}`})
	})

	result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "long-poll-"+uniqueSuffix(), Input{})

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusProcessed, result.outcome.Status)
	require.Equal(t, "2001", result.outcome.OrderEvent.OrderID)
	require.Equal(t, 1, queues.RequestCount("ReceiveMessage"), "the long poll was not dispatched a second time")
	require.Equal(t, 1, result.outcome.Message.ReceiveCount)
}

// TestQueueConsumerDuplicateAcknowledgementDispatchConvergesWithRealDex repeats an accepted delete after Worker loss.
func TestQueueConsumerDuplicateAcknowledgementDispatchConvergesWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	queues.HoldNextResponse("DeleteMessage", make(chan struct{}))
	messageID := queues.Send(integrationQueue, sqsfake.SentMessage{Body: `{"orderId":"1042","event":"shipped"}`})
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))

	ctx := integrationContext(t)
	flowID := "duplicate-ack-" + uniqueSuffix()
	_, err := harness.client.StartFlow(ctx, flow, flowID, Input{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		state, found := queues.Message(integrationQueue, messageID)
		return found && state.IsDeleted
	}, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, queues.RequestCount("DeleteMessage"))
	harness.replaceWorker(t)
	flowResult, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	result := queueConsumerRun{status: flowResult.Status}
	if flowResult.Status == dex.FlowCompleted {
		require.NoError(t, flowResult.DecodeSingleOutput(&result.outcome))
	}

	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusProcessed, result.outcome.Status)
	require.True(t, result.outcome.IsAcknowledgementConfirmed)
	deletes := deleteRequests(queues)
	require.Len(t, deletes, 2, "the replacement Worker repeats the acknowledgement once")
	require.Equal(t, deletes[0].Body, deletes[1].Body, "both attempts acknowledge the same delivery")
	state, _ := queues.Message(integrationQueue, messageID)
	require.True(t, state.IsDeleted)
	require.Equal(t, 1, queues.RequestCount("ReceiveMessage"))
}

func TestQueueConsumerUnwiredRejectionFailsTheFlowWithoutAcknowledgingWithRealDex(t *testing.T) {
	queues := newIntegrationQueues(t)
	queues.Intercept(func(request sqsfake.Request) *sqsfake.Response {
		if request.Action == "ReceiveMessage" {
			return &sqsfake.Response{StatusCode: http.StatusForbidden, Code: "AccessDenied"}
		}
		return nil
	})
	queues.Send(integrationQueue, sqsfake.SentMessage{Body: `{"orderId":"1042","event":"paid"}`})
	flow, harness := newQueueConsumerHarness(t, newFakeQueueClient(t, queues, 1))
	result := runQueueConsumer(t, integrationContext(t), harness.client, flow, "denied-"+uniqueSuffix(), Input{})
	require.Equal(t, dex.FlowFailed, result.status)
	require.Equal(t, 1, queues.RequestCount("ReceiveMessage"), "a conclusive rejection is not retried")
	require.Zero(t, queues.RequestCount("DeleteMessage"))
	require.Equal(t, 1, queues.RemainingMessageCount(integrationQueue))
}

func deleteRequests(queues *sqsfake.Server) []sqsfake.Request {
	var deletes []sqsfake.Request
	for _, request := range queues.Requests() {
		if request.Action == "DeleteMessage" {
			deletes = append(deletes, request)
		}
	}
	return deletes
}

func uniqueSuffix() string { return strconv.FormatInt(time.Now().UnixNano(), 10) }

func newIntegrationQueues(t *testing.T) *sqsfake.Server {
	t.Helper()
	return sqsfake.NewServer(t, sqsfake.Config{
		AccessKeyID: integrationAccessKeyID, SecretAccessKey: integrationSecretAccessKey, Region: integrationRegion,
		Queues: []string{integrationQueue},
	})
}

// newFakeQueueClient addresses the fake with the example's static connection, default queue, and receive wait.
func newFakeQueueClient(t *testing.T, queues *sqsfake.Server, receiveWaitSeconds int64) *sqs.Client {
	t.Helper()
	client, err := sqs.New(sqs.Config{
		Endpoint: queues.URL, Region: integrationRegion, DefaultQueueURL: queues.QueueURL(integrationQueue), ReceiveWaitSeconds: receiveWaitSeconds,
	}, sdkgo.StaticCredentialProvider[sqs.Credentials]{exampleConnection: {
		AccessKeyID: sdkgo.NewSecretString(integrationAccessKeyID), SecretAccessKey: sdkgo.NewSecretString(integrationSecretAccessKey),
	}})
	require.NoError(t, err)
	return client
}
