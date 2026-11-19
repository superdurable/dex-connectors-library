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
	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go"
	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go/tiktokmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignDiscoveryUsesDefaultMockWithRealDex(t *testing.T) {
	mock := tiktokmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	adgroupsFlow := NewAdgroupsFlow(mock.Connection())
	adsFlow := NewAdsFlow(mock.Connection())
	campaignStatusFlow := NewCampaignStatusFlow(mock.Connection())
	adgroupStatusFlow := NewAdgroupStatusFlow(mock.Connection())
	adStatusFlow := NewAdStatusFlow(mock.Connection())
	createCampaignFlow := NewCreateCampaignFlow(mock.Connection())
	updateCampaignFlow := NewUpdateCampaignFlow(mock.Connection())
	createAdgroupFlow := NewCreateAdgroupFlow(mock.Connection())
	updateAdgroupFlow := NewUpdateAdgroupFlow(mock.Connection())
	createAdFlow := NewCreateAdFlow(mock.Connection())
	updateAdFlow := NewUpdateAdFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, adgroupsFlow, adsFlow, campaignStatusFlow, adgroupStatusFlow, adStatusFlow, createCampaignFlow, updateCampaignFlow, createAdgroupFlow, updateAdgroupFlow, createAdFlow, updateAdFlow})
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
	flowID := "tiktok-campaigndiscovery-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, tiktok.ListCampaignsInput{AdvertiserID: "1845123456789012344"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output tiktok.CampaignsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "1845123456789012345", output.Campaigns[0].ID)
	require.Len(t, mock.ListCampaigns().Calls(), 1)
	adgroupsFlowID := flowID + "-adgroups"
	_, err = client.StartFlow(ctx, adgroupsFlow, adgroupsFlowID, tiktok.ListAdgroupsInput{AdvertiserID: "1845123456789012344"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adgroupsResult, err := client.WaitForFlow(ctx, adgroupsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adgroupsResult.Status, adgroupsResult.ErrorMessage)
	var adgroupsOutput tiktok.AdgroupsPage
	require.NoError(t, adgroupsResult.DecodeSingleOutput(&adgroupsOutput))
	require.Equal(t, "1845123456789012346", adgroupsOutput.Adgroups[0].ID)
	require.Len(t, mock.ListAdgroups().Calls(), 1)
	adsFlowID := flowID + "-ads"
	_, err = client.StartFlow(ctx, adsFlow, adsFlowID, tiktok.ListAdsInput{AdvertiserID: "1845123456789012344"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adsResult, err := client.WaitForFlow(ctx, adsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adsResult.Status, adsResult.ErrorMessage)
	var adsOutput tiktok.AdsPage
	require.NoError(t, adsResult.DecodeSingleOutput(&adsOutput))
	require.Equal(t, "1845123456789012347", adsOutput.Ads[0].ID)
	require.Len(t, mock.ListAds().Calls(), 1)
	campaignStatusFlowID := flowID + "-campaign-status"
	_, err = client.StartFlow(ctx, campaignStatusFlow, campaignStatusFlowID, tiktok.UpdateCampaignStatusInput{AdvertiserID: "1845123456789012344", CampaignID: "1845123456789012345", OperationStatus: "DISABLE"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	campaignStatusResult, err := client.WaitForFlow(ctx, campaignStatusFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, campaignStatusResult.Status, campaignStatusResult.ErrorMessage)
	require.Len(t, mock.UpdateCampaignStatus().Calls(), 1)
	adgroupStatusFlowID := flowID + "-adgroup-status"
	_, err = client.StartFlow(ctx, adgroupStatusFlow, adgroupStatusFlowID, tiktok.UpdateAdgroupStatusInput{AdvertiserID: "1845123456789012344", AdgroupID: "1845123456789012345", OperationStatus: "DISABLE"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adgroupStatusResult, err := client.WaitForFlow(ctx, adgroupStatusFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adgroupStatusResult.Status, adgroupStatusResult.ErrorMessage)
	require.Len(t, mock.UpdateAdgroupStatus().Calls(), 1)
	adStatusFlowID := flowID + "-ad-status"
	_, err = client.StartFlow(ctx, adStatusFlow, adStatusFlowID, tiktok.UpdateAdStatusInput{AdvertiserID: "1845123456789012344", AdID: "1845123456789012345", OperationStatus: "DISABLE"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adStatusResult, err := client.WaitForFlow(ctx, adStatusFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adStatusResult.Status, adStatusResult.ErrorMessage)
	require.Len(t, mock.UpdateAdStatus().Calls(), 1)
	createCampaignFlowID := flowID + "-create-campaign"
	_, err = client.StartFlow(ctx, createCampaignFlow, createCampaignFlowID, tiktok.CreateCampaignInput{AdvertiserID: "1845123456789012344", Name: "Example Campaign", ObjectiveType: "LEAD_GENERATION", BudgetMode: "BUDGET_MODE_TOTAL", Budget: 50}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createCampaignResult, err := client.WaitForFlow(ctx, createCampaignFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createCampaignResult.Status, createCampaignResult.ErrorMessage)
	require.Len(t, mock.CreateCampaign().Calls(), 1)
	updateCampaignFlowID := flowID + "-update-campaign"
	updatedCampaignName := "Updated Campaign"
	_, err = client.StartFlow(ctx, updateCampaignFlow, updateCampaignFlowID, tiktok.UpdateCampaignInput{AdvertiserID: "1845123456789012344", CampaignID: "1845123456789012345", Name: &updatedCampaignName}, dex.StartFlowOptions{})
	require.NoError(t, err)
	updateCampaignResult, err := client.WaitForFlow(ctx, updateCampaignFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, updateCampaignResult.Status, updateCampaignResult.ErrorMessage)
	require.Len(t, mock.UpdateCampaign().Calls(), 1)
	createAdgroupFlowID := flowID + "-create-adgroup"
	_, err = client.StartFlow(ctx, createAdgroupFlow, createAdgroupFlowID, tiktok.CreateAdgroupInput{AdvertiserID: "1845123456789012344", CampaignID: "1845123456789012345", Name: "Example Adgroup", BillingEvent: "OCPM", Budget: 50, BudgetMode: "BUDGET_MODE_TOTAL", OptimizationGoal: "CONVERT", Pacing: "PACING_MODE_SMOOTH", ScheduleStartTime: "2026-11-20 00:00:00", ScheduleEndTime: "2026-12-20 00:00:00", ScheduleType: "SCHEDULE_START_END", LocationIDs: []string{"1562822"}}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createAdgroupResult, err := client.WaitForFlow(ctx, createAdgroupFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createAdgroupResult.Status, createAdgroupResult.ErrorMessage)
	require.Len(t, mock.CreateAdgroup().Calls(), 1)
	updateAdgroupFlowID := flowID + "-update-adgroup"
	updatedAdgroupName := "Updated Adgroup"
	_, err = client.StartFlow(ctx, updateAdgroupFlow, updateAdgroupFlowID, tiktok.UpdateAdgroupInput{AdvertiserID: "1845123456789012344", AdgroupID: "1845123456789012346", Name: &updatedAdgroupName}, dex.StartFlowOptions{})
	require.NoError(t, err)
	updateAdgroupResult, err := client.WaitForFlow(ctx, updateAdgroupFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, updateAdgroupResult.Status, updateAdgroupResult.ErrorMessage)
	require.Len(t, mock.UpdateAdgroup().Calls(), 1)
	createAdFlowID := flowID + "-create-ad"
	_, err = client.StartFlow(ctx, createAdFlow, createAdFlowID, tiktok.CreateAdInput{AdvertiserID: "1845123456789012344", AdgroupID: "1845123456789012346", Name: "Example Ad", Text: "Example ad copy", VideoID: "video-1", ImageID: "image-1", IdentityID: "identity-1", IdentityType: "CUSTOMIZED_USER", CallToAction: "LEARN_MORE", LandingPageURL: "https://example.com/landing"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createAdResult, err := client.WaitForFlow(ctx, createAdFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createAdResult.Status, createAdResult.ErrorMessage)
	require.Len(t, mock.CreateAd().Calls(), 1)
	updateAdFlowID := flowID + "-update-ad"
	updatedAdName := "Updated Ad"
	_, err = client.StartFlow(ctx, updateAdFlow, updateAdFlowID, tiktok.UpdateAdInput{AdvertiserID: "1845123456789012344", AdgroupID: "1845123456789012346", AdID: "1845123456789012347", Name: &updatedAdName}, dex.StartFlowOptions{})
	require.NoError(t, err)
	updateAdResult, err := client.WaitForFlow(ctx, updateAdFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, updateAdResult.Status, updateAdResult.ErrorMessage)
	require.Len(t, mock.UpdateAd().Calls(), 1)
}
