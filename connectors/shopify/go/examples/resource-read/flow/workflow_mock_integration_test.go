//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/shopify/go"
	"github.com/superdurable/dex-connectors-library/connectors/shopify/go/shopifymock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResourceReadUsesManifestMockWithRealDex(t *testing.T) {
	mock := shopifymock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	countFlow := NewCountFlow(mock.Connection())
	createOrderFlow := NewCreateOrderFlow(mock.Connection())
	createProductFlow := NewCreateProductFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, countFlow, createOrderFlow, createProductFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	workerOptions := dex.WorkerOptions{FlowServiceAddress: serverAddress}
	worker, err := dex.NewWorker(registry, cache, workerOptions)
	require.NoError(t, err)
	clientOptions := dex.ClientOptions{FlowServiceAddress: serverAddress}
	client, err := dex.NewClient(registry, cache, clientOptions)
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "shopify-resource-read-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Request{OrderID: 101}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.NotEmpty(t, outcome.Value.ID)
	calls := mock.GetOrder().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, shopify.GetOrderInput{OrderID: 101}, calls[0].Input)
	require.Equal(t, shopify.GetOrderBranchFound, calls[0].Branch)
	countFlowID := "shopify-order-count-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, countFlow, countFlowID, CountRequest{Status: "closed"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	countResult, err := client.WaitForFlow(ctx, countFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, countResult.Status, countResult.ErrorMessage)
	var countOutcome CountOutcome
	require.NoError(t, countResult.DecodeSingleOutput(&countOutcome))
	require.EqualValues(t, 1, countOutcome.Count)
	countCalls := mock.CountOrders().Calls()
	require.Len(t, countCalls, 1)
	require.Equal(t, countFlowID, countCalls[0].FlowID)
	require.Equal(t, shopify.CountOrdersInput{Status: "closed"}, countCalls[0].Input)
	require.Equal(t, shopify.CountOrdersBranchCounted, countCalls[0].Branch)
	createFlowID := "shopify-order-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	createInput := shopify.CreateOrderInput{LineItems: []shopify.OrderLineItem{{Title: "Sample item", Quantity: 1, Price: "29.00"}}}
	_, err = client.StartFlow(ctx, createOrderFlow, createFlowID, createInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	createResult, err := client.WaitForFlow(ctx, createFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createResult.Status, createResult.ErrorMessage)
	var createOutcome CreateOrderOutcome
	require.NoError(t, createResult.DecodeSingleOutput(&createOutcome))
	require.EqualValues(t, 102, createOutcome.Order.ID)
	createCalls := mock.CreateOrder().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, createFlowID, createCalls[0].FlowID)
	require.Equal(t, createInput, createCalls[0].Input)
	require.Equal(t, shopify.CreateOrderBranchCreated, createCalls[0].Branch)
	productFlowID := "shopify-product-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	productInput := shopify.CreateProductInput{Title: "New Product", Status: "draft"}
	_, err = client.StartFlow(ctx, createProductFlow, productFlowID, productInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	productResult, err := client.WaitForFlow(ctx, productFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, productResult.Status, productResult.ErrorMessage)
	var productOutcome CreateProductOutcome
	require.NoError(t, productResult.DecodeSingleOutput(&productOutcome))
	require.EqualValues(t, 202, productOutcome.Product.ID)
	productCalls := mock.CreateProduct().Calls()
	require.Len(t, productCalls, 1)
	require.Equal(t, productFlowID, productCalls[0].FlowID)
	require.Equal(t, productInput, productCalls[0].Input)
	require.Equal(t, shopify.CreateProductBranchCreated, productCalls[0].Branch)
}
