//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go/activecampaignmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUpsertEcommerceOrderUsesDefaultMockWithRealDex(t *testing.T) {
	mock := activecampaignmock.New(t, ConnectionName)
	flow := NewUpsertEcommerceOrderFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
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
	flowID := "activecampaign-upsertorder-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := activecampaign.UpsertEcommerceOrderInput{ConnectionID: "3", StoreOrderID: "store-order-1", StoreCustomerID: "store-customer-1", Email: "person@example.com", CreatedAt: "2026-11-16T10:00:00Z", Currency: "USD", FinalAmount: "91.11", CreationSource: "HISTORICAL", Status: "COMPLETED", LineItems: []activecampaign.EcommerceOrderLineItemInput{{StoreLineItemID: "line-1", ProductStorePrimaryID: "product-1", Name: "Spatula", Quantity: 2, PriceAmount: "45.55"}}}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output activecampaign.EcommerceOrderUpsertMetadata
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "store-order-1", output.StoreOrderID)
	require.Len(t, mock.UpsertEcommerceOrder().Calls(), 1)
}
