//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channellookup

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

func TestLookupUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := slackmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
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
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(client.Close(), worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	contextWithDeadline, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := LookupRequest{NameFilter: "#general", MemberOnly: true}
	flowID := "slack-channel-lookup-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(contextWithDeadline, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(contextWithDeadline, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output LookupOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "Acme Corp", output.Workspace.TeamName)
	require.Equal(t, "C0345678901", output.Channels.Channels[0].ChannelID)
	require.Equal(t, "cursor-page-2", output.Channels.NextCursor)

	identityCalls := mock.GetWorkspaceIdentity().Calls()
	require.Len(t, identityCalls, 1)
	require.Equal(t, slack.GetWorkspaceIdentityBranchRead, identityCalls[0].Branch)
	require.Equal(t, flowID, identityCalls[0].FlowID)
	channelCalls := mock.ListChannels().Calls()
	require.Len(t, channelCalls, 1)
	require.Equal(t, request, channelCalls[0].Input)
	require.Equal(t, slack.ListChannelsBranchListed, channelCalls[0].Branch)
	require.Equal(t, flowID, channelCalls[0].FlowID)
}
