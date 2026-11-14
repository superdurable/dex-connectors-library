//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package valuationread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhihuiya/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhihuiya/go/zhihuiyamock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGetPatentValuationUsesDefaultMockWithRealDex(t *testing.T) {
	mock := zhihuiyamock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:10061"
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
	flowID := "zhihuiya-get-patent-valuation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, zhihuiya.GetPatentValuationInput{PublicationNumber: "JP6012277B2"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output zhihuiya.GetPatentValuationOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, zhihuiya.GetPatentValuationOutput{PublicationNumber: "JP6012277B2", PatentID: "92ccf76a-a718-47e9-8cd6-6c5c9df19ae3", PatentValueUSD: 43000, LegalScore: 71, MarketScore: 18, EconomyScore: 23, StrategicScore: 71, TechnologyScore: 28}, output)

	require.Len(t, mock.GetPatentValuation().Calls(), 1)
}
