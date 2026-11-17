//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go/microsoftadvertisingmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignDiscoveryUsesDefaultMockWithRealDex(t *testing.T) {
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	inspectionFlow := NewCampaignInspectionFlow(mock.Connection())
	adGroupFlow := NewAdGroupDiscoveryFlow(mock.Connection())
	keywordFlow := NewKeywordDiscoveryFlow(mock.Connection())
	adFlow := NewAdDiscoveryFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, inspectionFlow, adGroupFlow, keywordFlow, adFlow})
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
	flowID := "microsoftadvertising-campaigndiscovery-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, microsoftadvertising.ListCampaignsInput{AccountID: "12345"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output microsoftadvertising.CampaignsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "123456789", output.Campaigns[0].ID)
	require.Equal(t, "Sample Search", output.Campaigns[0].Name)
	require.Len(t, mock.ListCampaigns().Calls(), 1)
	inspectionID := "microsoftadvertising-campaigninspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, inspectionFlow, inspectionID, microsoftadvertising.GetCampaignInput{AccountID: "12345", CampaignID: "98765"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	inspection, err := client.WaitForFlow(ctx, inspectionID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, inspection.Status, inspection.ErrorMessage)
	var campaign microsoftadvertising.CampaignSummary
	require.NoError(t, inspection.DecodeSingleOutput(&campaign))
	require.Equal(t, "98765", campaign.ID)
	require.Len(t, mock.GetCampaign().Calls(), 1)
	adGroupID := "microsoftadvertising-adgroups-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, adGroupFlow, adGroupID, microsoftadvertising.ListAdGroupsInput{AccountID: "12345", CampaignID: "98765"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adGroupResult, err := client.WaitForFlow(ctx, adGroupID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adGroupResult.Status, adGroupResult.ErrorMessage)
	var adGroups microsoftadvertising.AdGroupsPage
	require.NoError(t, adGroupResult.DecodeSingleOutput(&adGroups))
	require.Equal(t, "33", adGroups.AdGroups[0].ID)
	require.Len(t, mock.ListAdGroups().Calls(), 1)
	keywordID := "microsoftadvertising-keywords-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, keywordFlow, keywordID, microsoftadvertising.ListKeywordsInput{AccountID: "12345", AdGroupID: "33"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	keywordResult, err := client.WaitForFlow(ctx, keywordID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, keywordResult.Status, keywordResult.ErrorMessage)
	var keywords microsoftadvertising.KeywordsPage
	require.NoError(t, keywordResult.DecodeSingleOutput(&keywords))
	require.Equal(t, "44", keywords.Keywords[0].ID)
	require.Len(t, mock.ListKeywords().Calls(), 1)
	adID := "microsoftadvertising-ads-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, adFlow, adID, microsoftadvertising.ListAdsInput{AccountID: "12345", AdGroupID: "33"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adResult, err := client.WaitForFlow(ctx, adID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adResult.Status, adResult.ErrorMessage)
	var ads microsoftadvertising.AdsPage
	require.NoError(t, adResult.DecodeSingleOutput(&ads))
	require.Equal(t, "55", ads.Ads[0].ID)
	require.Len(t, mock.ListAds().Calls(), 1)
}
