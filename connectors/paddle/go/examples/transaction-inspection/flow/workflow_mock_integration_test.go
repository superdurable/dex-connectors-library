//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/connectors/paddle/go/paddlemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTransactionInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := paddlemock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	customerFlow := NewCustomerFlow(mock.Connection())
	discountFlow := NewDiscountFlow(mock.Connection())
	productFlow := NewProductFlow(mock.Connection())
	priceFlow := NewPriceFlow(mock.Connection())
	transactionFlow := NewTransactionFlow(mock.Connection())
	subscriptionFlow := NewSubscriptionFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, customerFlow, discountFlow, productFlow, priceFlow, transactionFlow, subscriptionFlow})
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
	flowID := "paddle-transactioninspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, paddle.GetTransactionInput{TransactionID: "txn_01hv8wptq8987qeep44cyrewp9"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output paddle.TransactionStatus
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "txn_01hv8wptq8987qeep44cyrewp9", output.ID)
	require.Equal(t, "completed", output.Status)
	require.Len(t, mock.GetTransaction().Calls(), 1)
	customerFlowID := "paddle-customer-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, customerFlow, customerFlowID, paddle.ListCustomersInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	customerResult, err := client.WaitForFlow(ctx, customerFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, customerResult.Status, customerResult.ErrorMessage)
	var page paddle.CustomerPage
	require.NoError(t, customerResult.DecodeSingleOutput(&page))
	require.Equal(t, "ctm_01hv8wptq8987qeep44cyrewp9", page.Items[0].ID)
	require.Len(t, mock.ListCustomers().Calls(), 1)
	discountFlowID := "paddle-discount-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, discountFlow, discountFlowID, paddle.ListDiscountsInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	discountResult, err := client.WaitForFlow(ctx, discountFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, discountResult.Status, discountResult.ErrorMessage)
	var discountPage paddle.DiscountPage
	require.NoError(t, discountResult.DecodeSingleOutput(&discountPage))
	require.Equal(t, "dsc_01gtgztp8fpchantd5g1wrksa3", discountPage.Items[0].ID)
	require.Len(t, mock.ListDiscounts().Calls(), 1)
	productFlowID := "paddle-product-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, productFlow, productFlowID, paddle.ListProductsInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	productResult, err := client.WaitForFlow(ctx, productFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, productResult.Status, productResult.ErrorMessage)
	var productPage paddle.ProductPage
	require.NoError(t, productResult.DecodeSingleOutput(&productPage))
	require.Equal(t, "pro_01htz88xpr0mm7b3ta2pjkr7w2", productPage.Items[0].ID)
	require.Len(t, mock.ListProducts().Calls(), 1)
	priceFlowID := "paddle-price-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, priceFlow, priceFlowID, paddle.ListPricesInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	priceResult, err := client.WaitForFlow(ctx, priceFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, priceResult.Status, priceResult.ErrorMessage)
	var pricePage paddle.PricePage
	require.NoError(t, priceResult.DecodeSingleOutput(&pricePage))
	require.Equal(t, "pri_01hv0vax6rv18t4tamj848ne4d", pricePage.Items[0].ID)
	require.Len(t, mock.ListPrices().Calls(), 1)
	transactionFlowID := "paddle-transaction-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, transactionFlow, transactionFlowID, paddle.ListTransactionsInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	transactionResult, err := client.WaitForFlow(ctx, transactionFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, transactionResult.Status, transactionResult.ErrorMessage)
	var transactionPage paddle.TransactionPage
	require.NoError(t, transactionResult.DecodeSingleOutput(&transactionPage))
	require.Equal(t, "txn_01hv8wptq8987qeep44cyrewp9", transactionPage.Items[0].ID)
	require.Len(t, mock.ListTransactions().Calls(), 1)
	subscriptionFlowID := "paddle-subscription-list-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, subscriptionFlow, subscriptionFlowID, paddle.ListSubscriptionsInput{PerPage: 2}, dex.StartFlowOptions{})
	require.NoError(t, err)
	subscriptionResult, err := client.WaitForFlow(ctx, subscriptionFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, subscriptionResult.Status, subscriptionResult.ErrorMessage)
	var subscriptionPage paddle.SubscriptionPage
	require.NoError(t, subscriptionResult.DecodeSingleOutput(&subscriptionPage))
	require.Equal(t, "sub_01hv8y5ehszzq0yv20ttx3166y", subscriptionPage.Items[0].ID)
	require.Len(t, mock.ListSubscriptions().Calls(), 1)
}
