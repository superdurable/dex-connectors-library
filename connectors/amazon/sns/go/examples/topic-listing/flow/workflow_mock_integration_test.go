//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topiclisting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go/snsmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicListingUsesDefaultMockWithRealDex(t *testing.T) {
	mock := snsmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	publishFlow := NewPublishFlow(mock.Connection())
	subscriptionFlow := NewSubscriptionListingFlow(mock.Connection())
	notificationFlow := NewNotificationFlow()
	registry, err := dex.NewRegistry([]dex.Flow{flow, publishFlow, subscriptionFlow, notificationFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "sns-topic-listing-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, sns.ListTopicsInput{NextToken: ""}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output sns.TopicPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "arn:aws:sns:us-east-1:123456789012:example-topic", output.TopicARNs[0])
	require.Len(t, mock.ListTopics().Calls(), 1)
	publishID := "sns-message-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, publishFlow, publishID, sns.PublishMessageInput{TopicARN: "arn:aws:sns:us-east-1:123456789012:example-topic", Message: "Report ready", Subject: "Operations"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	publishResult, err := client.WaitForFlow(ctx, publishID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, publishResult.Status, publishResult.ErrorMessage)
	var published sns.PublishedMessage
	require.NoError(t, publishResult.DecodeSingleOutput(&published))
	require.Equal(t, "example-message-id", published.MessageID)
	require.Len(t, mock.PublishMessage().Calls(), 1)
	subscriptionID := "sns-subscription-listing-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, subscriptionFlow, subscriptionID, sns.ListSubscriptionsByTopicInput{
		TopicARN: "arn:aws:sns:us-east-1:123456789012:example-topic",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	subscriptionResult, err := client.WaitForFlow(ctx, subscriptionID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, subscriptionResult.Status, subscriptionResult.ErrorMessage)
	var subscriptions sns.SubscriptionPage
	require.NoError(t, subscriptionResult.DecodeSingleOutput(&subscriptions))
	require.Len(t, subscriptions.Subscriptions, 1)
	require.Len(t, mock.ListSubscriptionsByTopic().Calls(), 1)
	notificationEvent := sdkgo.TriggerEvent[sns.SNSNotification]{
		ID: "11111111-1111-4111-8111-111111111111", OccurredAt: time.Now().UTC(),
		Payload: sns.SNSNotification{
			MessageID: "11111111-1111-4111-8111-111111111111",
			TopicARN:  "arn:aws:sns:us-east-1:123456789012:example-topic",
			Message:   "Report ready", PublishedAt: time.Now().UTC(),
		},
	}
	target := sdkgo.NewDexFlowTriggerTarget(client, notificationFlow, AcceptEveryNotification, ResolveNotificationFlowID, MapNotificationToFlowInput)
	require.NoError(t, sdkgo.DeliverTrigger(ctx, target, notificationEvent))
	notificationResult, err := client.WaitForFlow(ctx, ResolveNotificationFlowID(notificationEvent), dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, notificationResult.Status, notificationResult.ErrorMessage)
	var recorded sns.SNSNotification
	require.NoError(t, notificationResult.DecodeSingleOutput(&recorded))
	require.Equal(t, notificationEvent.Payload.MessageID, recorded.MessageID)
}
