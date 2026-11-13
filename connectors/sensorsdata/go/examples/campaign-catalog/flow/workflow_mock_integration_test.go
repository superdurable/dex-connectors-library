//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigncatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sensorsdata/go"
	"github.com/superdurable/dex-connectors-library/connectors/sensorsdata/go/sensorsdatamock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignCatalogUsesDefaultMockWithRealDex(t *testing.T) {
	mock := sensorsdatamock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:10291"
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
	flowID := "sensorsdata-campaign-catalog-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, sensorsdata.ListCampaignsInput{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output sensorsdata.ListCampaignsOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, sensorsdata.ListCampaignsOutput{Campaigns: []sensorsdata.Campaign{{Name: "Example Campaign", LinkCount: 1, LatestUseTime: "2025-01-01 12:00:00"}}, TotalRows: 1, TotalPages: 1, PageNum: 0, PageSize: 20}, output)
	require.Len(t, mock.ListCampaigns().Calls(), 1)
}
