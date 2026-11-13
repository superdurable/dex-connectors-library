//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package parceltracking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidibird/go"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidibird/go/kuaidibirdmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTrackingUsesDefaultMockWithRealDex(t *testing.T) {
	mock := kuaidibirdmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:9191"
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
	flowID := "kuaidibird-parcel-tracking-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, kuaidibird.QueryTrackingInput{CarrierCode: "YTO", TrackingNumber: "YT1234567890123"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output kuaidibird.QueryTrackingOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "YT1234567890123", output.TrackingNumber)
	require.Equal(t, "2", output.State)
	require.Equal(t, []kuaidibird.TrackingEvent{
		{Time: "2026-11-12 15:30:00", Content: "Parcel was collected by the carrier."},
		{Time: "2026-11-13 10:00:00", Content: "Parcel reached the destination sorting center."},
	}, output.Events)
	require.Len(t, mock.QueryTracking().Calls(), 1)
}
