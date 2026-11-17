//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topicposts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go/redditmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicPostsUsesDefaultMocksWithRealDex(t *testing.T) {
	mock := redditmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	client := startTopicPostsWorker(t, flow)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "reddit-topic-posts-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := client.StartFlow(ctx, flow, flowID, TopicPostsRequest{Subreddit: "golang", Topic: "generics", ReadCommunity: true}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output TopicPostsOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	// The newest page's pinned thread and the post repeated by the search appear at most once and never pinned.
	require.Equal(t, []string{"t3_1a0004", "t3_1a0003"}, postIDs(output.Posts))
	require.Equal(t, "https://example.com/benchmarks/generics", output.Posts[0].LinkURL)
	require.Equal(t, "t3_1a0001", output.SearchAfter)
	require.NotNil(t, output.Community)
	require.Equal(t, "golang", output.Community.Name)
	require.Equal(t, "golang", mock.GetSubreddit().Calls()[0].Input.Subreddit)
	listings := mock.ListSubredditPosts().Calls()
	require.Len(t, listings, 1)
	require.Equal(t, reddit.ListSubredditPostsInput{Subreddit: "golang", Sort: reddit.SubredditPostSortNew}, listings[0].Input)
	require.Equal(t, reddit.ListSubredditPostsBranchListed, listings[0].Branch)
	searches := mock.SearchPosts().Calls()
	require.Len(t, searches, 1)
	require.Equal(t, reddit.SearchPostsInput{Query: "generics", Subreddit: "golang", Sort: reddit.SearchPostSortNew, TimeWindow: reddit.PostTimeWindowWeek}, searches[0].Input)
	var stored []reddit.ListedPost
	read, err := client.GetAttributes(ctx, flowID, dex.ReadAttribute(recentMentionsAttribute, &stored))
	require.NoError(t, err)
	require.True(t, read.IsSet(recentMentionsAttribute))
	require.Equal(t, []string{"t3_1a0003"}, postIDs(stored))
}

func TestTopicPostsFailsWhenTheSubredditIsUnavailable(t *testing.T) {
	mock := redditmock.New(t, ConnectionName)
	mock.ListSubredditPosts().Respond(redditmock.ListSubredditPostsSubredditMissing())
	flow := NewFlow(mock.Connection())
	client := startTopicPostsWorker(t, flow)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "reddit-topic-posts-missing-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := client.StartFlow(ctx, flow, flowID, TopicPostsRequest{Subreddit: "nosuchcommunity", Topic: "generics"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	// The unwired optional branch fails the Flow, so a misspelled subreddit stays visible instead of reading as no posts.
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Len(t, mock.ListSubredditPosts().Calls(), 1)
	require.Equal(t, reddit.ListSubredditPostsBranchSubredditUnavailable, mock.ListSubredditPosts().Calls()[0].Branch)
	require.Empty(t, mock.SearchPosts().Calls())
}

// startTopicPostsWorker runs flow on the Dex server at DEX_FLOW_SERVICE_ADDRESS and returns a Client for it.
func startTopicPostsWorker(t *testing.T, flow *Flow) *dex.Client {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
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
	return client
}
