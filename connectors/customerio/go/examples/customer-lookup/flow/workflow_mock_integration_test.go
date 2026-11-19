//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/connectors/customerio/go/customeriomock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerLookupUsesDefaultMockWithRealDex(t *testing.T) {
	mock := customeriomock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	campaignFlow := NewCampaignInspectionFlow(mock.Connection())
	campaignsFlow := NewCampaignsSearchFlow(mock.Connection())
	metricsFlow := NewCampaignMetricsFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, campaignFlow, campaignsFlow, metricsFlow})
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
	flowID := "customerio-customerlookup-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, customerio.GetCustomersByEmailInput{Email: "person@example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output customerio.CustomerLookupPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "person@example.com", output.Email)
	require.Equal(t, []string{"a3000001"}, output.CustomerIDs)
	require.Len(t, mock.GetCustomersByEmail().Calls(), 1)
	campaignFlowID := "customerio-campaign-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, campaignFlow, campaignFlowID, customerio.GetCampaignInput{CampaignID: 5}, dex.StartFlowOptions{})
	require.NoError(t, err)
	campaignResult, err := client.WaitForFlow(ctx, campaignFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, campaignResult.Status, campaignResult.ErrorMessage)
	var campaign customerio.CampaignSummary
	require.NoError(t, campaignResult.DecodeSingleOutput(&campaign))
	require.Equal(t, int64(5), campaign.ID)
	require.Len(t, mock.GetCampaign().Calls(), 1)
	campaignsFlowID := "customerio-campaigns-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, campaignsFlow, campaignsFlowID, customerio.ListCampaignsInput{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	campaignsResult, err := client.WaitForFlow(ctx, campaignsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, campaignsResult.Status, campaignsResult.ErrorMessage)
	var campaigns customerio.CampaignsPage
	require.NoError(t, campaignsResult.DecodeSingleOutput(&campaigns))
	require.Len(t, campaigns.Campaigns, 1)
	require.Len(t, mock.ListCampaigns().Calls(), 1)
	metricsFlowID := "customerio-campaign-metrics-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, metricsFlow, metricsFlowID, customerio.GetCampaignMetricsInput{CampaignID: 5, StartUnix: 1735718400, EndUnix: 1735804800, Resolution: "days"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	metricsResult, err := client.WaitForFlow(ctx, metricsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, metricsResult.Status, metricsResult.ErrorMessage)
	var metrics customerio.CampaignMetrics
	require.NoError(t, metricsResult.DecodeSingleOutput(&metrics))
	require.Equal(t, []int64{1}, metrics.Series["sent"])
	require.Len(t, mock.GetCampaignMetrics().Calls(), 1)
}
