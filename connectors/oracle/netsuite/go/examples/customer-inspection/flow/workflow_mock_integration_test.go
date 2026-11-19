//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go"
	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go/oraclenetsuitemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := oraclenetsuitemock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	listFlow := NewListFlow(mock.Connection())
	vendorListFlow := NewVendorListFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, listFlow, vendorListFlow})
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
	flowID := "oracle-netsuite-customer-inspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, oraclenetsuite.GetCustomerInput{CustomerID: "107"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output oraclenetsuite.CustomerSummary
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "107", output.ID)
	require.Equal(t, "CUST107", output.EntityID)
	require.Len(t, mock.GetCustomer().Calls(), 1)
	listFlowID := "oracle-netsuite-customer-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, listFlow, listFlowID, oraclenetsuite.ListCustomersInput{Limit: 2, Offset: 0}, dex.StartFlowOptions{})
	require.NoError(t, err)
	listResult, err := client.WaitForFlow(ctx, listFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, listResult.Status, listResult.ErrorMessage)
	var page oraclenetsuite.CustomerList
	require.NoError(t, listResult.DecodeSingleOutput(&page))
	require.Equal(t, "107", page.Items[0].ID)
	require.Len(t, mock.ListCustomers().Calls(), 1)
	vendorFlowID := "oracle-netsuite-vendor-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, vendorListFlow, vendorFlowID, oraclenetsuite.ListVendorsInput{Limit: 2, Offset: 0}, dex.StartFlowOptions{})
	require.NoError(t, err)
	vendorResult, err := client.WaitForFlow(ctx, vendorFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, vendorResult.Status, vendorResult.ErrorMessage)
	var vendorPage oraclenetsuite.VendorList
	require.NoError(t, vendorResult.DecodeSingleOutput(&vendorPage))
	require.Equal(t, "107", vendorPage.Items[0].ID)
	require.Len(t, mock.ListVendors().Calls(), 1)
}
