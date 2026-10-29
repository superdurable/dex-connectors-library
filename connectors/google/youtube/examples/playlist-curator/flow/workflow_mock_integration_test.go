//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlistcurator

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/youtube"
	"github.com/superdurable/dex-connectors-library/connectors/google/youtube/youtubemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPlaylistCuratorUsesYouTubeMocksWithRealDex(t *testing.T) {
	readMock := youtubemock.New(t, ReadConnectionName)
	manageMock := youtubemock.New(t, ManageConnectionName)
	flow := NewFlow(readMock.Connection(), manageMock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness := &curatorHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, harness.client.Close())
		require.NoError(t, cache.Close())
	})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "youtube-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result := harness.runFlow(t, ctx, flow, flowID, CurationRequest{Channel: "@CompilerLab", Query: "compiler", PlaylistID: "PLcurated123"})
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome CurationOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, StatusAdded, outcome.Status)
	require.Equal(t, "PLcurated123", outcome.PlaylistID)
	require.True(t, outcome.IsConfirmed)
	require.False(t, outcome.WasAlreadyPresent)

	channelCalls := readMock.GetChannel().Calls()
	require.Len(t, channelCalls, 1)
	require.Equal(t, youtube.GetChannelInput{Handle: "@CompilerLab"}, channelCalls[0].Input)
	searchCalls := readMock.SearchVideos().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, youtube.SearchVideosInput{Query: "compiler", ChannelID: "UC1234567890123456789012", Order: "date", MaxResults: 1}, searchCalls[0].Input)
	addCalls := manageMock.AddPlaylistItem().Calls()
	require.Len(t, addCalls, 1)
	require.Equal(t, youtube.AddPlaylistItemInput{PlaylistID: "PLcurated123", VideoID: "AbCdEfGhI12"}, addCalls[0].Input)
	listCalls := readMock.ListPlaylistItems().Calls()
	require.Len(t, listCalls, 1)
	require.Equal(t, youtube.ListPlaylistItemsInput{PlaylistID: "PLcurated123", VideoID: "AbCdEfGhI12", MaxResults: 1}, listCalls[0].Input)
}
