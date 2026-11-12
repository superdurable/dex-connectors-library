//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelmarkersearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/slackmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchFindsMarkerOnSecondMockPageWithRealDex(t *testing.T) {
	mock := slackmock.New(t, ConnectionName)
	flow, client := startMarkerSearchHarness(t, mock)
	request := SearchRequest{ChannelID: "C0123456789", Marker: "Order 1042 is ready for review.", OldestTimestamp: "1709999999.000000"}
	flowID := "slack-marker-search-found-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	output := runMarkerSearch(t, client, flow, flowID, request)
	require.True(t, output.IsFound)
	require.Equal(t, 2, output.PagesRead)
	require.Equal(t, "1710000010.000500", output.Message.Timestamp)
	require.Equal(t, "U0BOT123456", output.Message.UserID)

	identityCalls := mock.GetWorkspaceIdentity().Calls()
	require.Len(t, identityCalls, 1)
	require.Equal(t, slack.GetWorkspaceIdentityBranchRead, identityCalls[0].Branch)
	require.Equal(t, flowID, identityCalls[0].FlowID)
	calls := mock.ListChannelMessages().Calls()
	require.Len(t, calls, 2)
	require.Equal(t, slack.ListChannelMessagesInput{ChannelID: "C0123456789", OldestTimestamp: "1709999999.000000"}, calls[0].Input)
	require.Equal(t, slack.ListChannelMessagesInput{ChannelID: "C0123456789", OldestTimestamp: "1709999999.000000", Cursor: "cursor-page-2"}, calls[1].Input)
	for _, call := range calls {
		require.Equal(t, slack.ListChannelMessagesBranchRead, call.Branch)
		require.Equal(t, flowID, call.FlowID)
	}
}

func TestSearchIgnoresAPersonQuotingTheMarkerWithRealDex(t *testing.T) {
	mock := slackmock.New(t, ConnectionName)
	const marker = "ref:order-1042"
	mock.ListChannelMessages().Respond(
		slackmock.ListChannelMessagesRead(slack.ListChannelMessagesOutput{Messages: []slack.ChannelMessage{
			{ChannelID: "C0123456789", Timestamp: "1710000030.000900", UserID: "U0234567890", Text: "Did " + marker + " post?"},
		}, NextCursor: "cursor-page-2"}, nil),
		slackmock.ListChannelMessagesRead(slack.ListChannelMessagesOutput{Messages: []slack.ChannelMessage{
			{ChannelID: "C0123456789", Timestamp: "1710000010.000500", UserID: "U0BOT123456", BotID: "B0123456789", Text: "Order ready " + marker},
		}}, nil),
	)
	flow, client := startMarkerSearchHarness(t, mock)
	flowID := "slack-marker-search-quoted-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	output := runMarkerSearch(t, client, flow, flowID, SearchRequest{ChannelID: "C0123456789", Marker: marker})
	require.True(t, output.IsFound)
	require.Equal(t, 2, output.PagesRead)
	require.Equal(t, "1710000010.000500", output.Message.Timestamp)
	require.Len(t, mock.ListChannelMessages().Calls(), 2)
}

func TestSearchStopsAtThePageBoundWithRealDex(t *testing.T) {
	mock := slackmock.New(t, ConnectionName)
	flow, client := startMarkerSearchHarness(t, mock)
	request := SearchRequest{ChannelID: "C0123456789", Marker: "Order 1042 is ready for review.", MaximumPages: 1}
	flowID := "slack-marker-search-bounded-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	output := runMarkerSearch(t, client, flow, flowID, request)
	require.False(t, output.IsFound)
	require.Nil(t, output.Message)
	require.Equal(t, 1, output.PagesRead)
	require.Len(t, mock.ListChannelMessages().Calls(), 1)
}

func startMarkerSearchHarness(t *testing.T, mock *slackmock.Mock) (*Flow, *dex.Client) {
	t.Helper()
	flow := NewFlow(mock.Connection())
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
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(client.Close(), worker.Stop(stopContext), <-workerResult, cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	return flow, client
}

func runMarkerSearch(t *testing.T, client *dex.Client, flow *Flow, flowID string, request SearchRequest) SearchOutput {
	t.Helper()
	contextWithDeadline, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := client.StartFlow(contextWithDeadline, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(contextWithDeadline, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output SearchOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	return output
}
