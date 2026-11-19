//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go/pubsubmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicsUsesDefaultMockWithRealDex(t *testing.T) {
	mock := pubsubmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	detailFlow := NewTopicDetailFlow(mock.Connection())
	subscriptionFlow := NewSubscriptionDetailFlow(mock.Connection())
	publishFlow := NewPublishFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, detailFlow, subscriptionFlow, publishFlow})
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
	flowID := "google-pubsub-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, pubsub.ListTopicsInput{ProjectID: "sample-project"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output pubsub.TopicsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "projects/sample-project/topics/events", output.Topics[0].Name)
	require.Len(t, mock.ListTopics().Calls(), 1)
	detailFlowID := "google-pubsub-detail-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, detailFlow, detailFlowID, pubsub.GetTopicInput{Name: "projects/sample-project/topics/events"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	detailResult, err := client.WaitForFlow(ctx, detailFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, detailResult.Status, detailResult.ErrorMessage)
	var detail pubsub.Topic
	require.NoError(t, detailResult.DecodeSingleOutput(&detail))
	require.Equal(t, "projects/sample-project/topics/events", detail.Name)
	require.Len(t, mock.GetTopic().Calls(), 1)
	subscriptionFlowID := "google-pubsub-subscription-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, subscriptionFlow, subscriptionFlowID, pubsub.GetSubscriptionInput{Name: "projects/sample-project/subscriptions/events-reader"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	subscriptionResult, err := client.WaitForFlow(ctx, subscriptionFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, subscriptionResult.Status, subscriptionResult.ErrorMessage)
	var subscription pubsub.Subscription
	require.NoError(t, subscriptionResult.DecodeSingleOutput(&subscription))
	require.Equal(t, "projects/sample-project/subscriptions/events-reader", subscription.Name)
	require.Len(t, mock.GetSubscription().Calls(), 1)
	publishFlowID := "google-pubsub-publish-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, publishFlow, publishFlowID, pubsub.PublishMessageInput{Topic: "projects/sample-project/topics/events", Text: "event"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	publishResult, err := client.WaitForFlow(ctx, publishFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, publishResult.Status, publishResult.ErrorMessage)
	var published pubsub.PublishedMessage
	require.NoError(t, publishResult.DecodeSingleOutput(&published))
	require.Equal(t, "1234567890", published.MessageID)
	require.Len(t, mock.PublishMessage().Calls(), 1)
}
