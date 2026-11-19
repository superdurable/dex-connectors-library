//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go/adsmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountsUsesDefaultMockWithRealDex(t *testing.T) {
	mock := adsmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	groupsFlow := NewCampaignGroupsFlow(mock.Connection())
	accountLookupFlow := NewAdAccountLookupFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, groupsFlow, accountLookupFlow})
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
	flowID := "linkedin-ads-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, ads.ListAdAccountsInput{PageSize: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output ads.AdAccountsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "507404993", output.Accounts[0].ID)
	require.Len(t, mock.ListAdAccounts().Calls(), 1)
	groupsFlowID := "linkedin-ads-groups-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, groupsFlow, groupsFlowID, ads.ListCampaignGroupsInput{AccountID: "507404993", Status: "ACTIVE"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	groupsResult, err := client.WaitForFlow(ctx, groupsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, groupsResult.Status, groupsResult.ErrorMessage)
	var groupsOutput ads.CampaignGroupsPage
	require.NoError(t, groupsResult.DecodeSingleOutput(&groupsOutput))
	require.Equal(t, "603407684", groupsOutput.Groups[0].ID)
	require.Len(t, mock.ListCampaignGroups().Calls(), 1)
	accountFlowID := "linkedin-ads-account-lookup-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, accountLookupFlow, accountFlowID, ads.GetAdAccountInput{ID: "507404993"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	accountResult, err := client.WaitForFlow(ctx, accountFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, accountResult.Status, accountResult.ErrorMessage)
	var account ads.AdAccount
	require.NoError(t, accountResult.DecodeSingleOutput(&account))
	require.Equal(t, "507404993", account.ID)
	require.Len(t, mock.GetAdAccount().Calls(), 1)
}
