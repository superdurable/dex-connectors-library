//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sites

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	searchconsole "github.com/superdurable/dex-connectors-library/connectors/google/search-console/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/search-console/go/searchconsolemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSitesUsesDefaultMockWithRealDex(t *testing.T) {
	mock := searchconsolemock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	analyticsFlow := NewAnalyticsFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, analyticsFlow})
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
	flowID := "google-search-console-sites-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, searchconsole.ListSitesInput{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output searchconsole.SitesPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "sc-domain:example.com", output.Sites[0].SiteURL)
	require.Equal(t, "siteOwner", output.Sites[0].PermissionLevel)
	require.Len(t, mock.ListSites().Calls(), 1)
	analyticsFlowID := "google-search-console-analytics-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, analyticsFlow, analyticsFlowID, searchconsole.QuerySearchAnalyticsInput{SiteURL: "sc-domain:example.com", StartDate: "2026-11-01", EndDate: "2026-11-07", Dimensions: []string{"query"}, RowLimit: 10}, dex.StartFlowOptions{})
	require.NoError(t, err)
	analyticsResult, err := client.WaitForFlow(ctx, analyticsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, analyticsResult.Status, analyticsResult.ErrorMessage)
	var analytics searchconsole.SearchAnalyticsResult
	require.NoError(t, analyticsResult.DecodeSingleOutput(&analytics))
	require.Equal(t, float64(5), analytics.Rows[0].Clicks)
	require.Len(t, mock.QuerySearchAnalytics().Calls(), 1)
}
