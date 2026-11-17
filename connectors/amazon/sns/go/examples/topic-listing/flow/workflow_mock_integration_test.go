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
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicListingUsesDefaultMockWithRealDex(t *testing.T) {
	mock := snsmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	publishFlow := NewPublishFlow(mock.Connection())
	subscriptionFlow := NewSubscriptionListingFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, publishFlow, subscriptionFlow})
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
}
