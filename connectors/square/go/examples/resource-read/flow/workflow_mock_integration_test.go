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
	"github.com/superdurable/dex-connectors-library/connectors/square/go"
	"github.com/superdurable/dex-connectors-library/connectors/square/go/squaremock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResourceReadUsesManifestMockWithRealDex(t *testing.T) {
	mock := squaremock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
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
	flowID := "square-resource-read-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Request{Cursor: ""}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.NotEmpty(t, outcome.Value.Payments)
	calls := mock.ListPayments().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, square.ListPaymentsInput{Cursor: ""}, calls[0].Input)
	require.Equal(t, square.ListPaymentsBranchListed, calls[0].Branch)
	cashFlowID := "square-cash-payment-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	cashInput := Request{RecordCashPayment: true, AmountMoney: square.Money{Amount: 2900, Currency: "USD"}}
	_, err = client.StartFlow(ctx, flow, cashFlowID, cashInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	cashResult, err := client.WaitForFlow(ctx, cashFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, cashResult.Status, cashResult.ErrorMessage)
	var cashOutcome Outcome
	require.NoError(t, cashResult.DecodeSingleOutput(&cashOutcome))
	require.Equal(t, "pay_102", cashOutcome.Payment.ID)
	cashCalls := mock.CreatePayment().Calls()
	require.Len(t, cashCalls, 1)
	require.Equal(t, cashFlowID, cashCalls[0].FlowID)
	require.Equal(t, square.CreatePaymentInput{SourceID: "CASH", AmountMoney: cashInput.AmountMoney}, cashCalls[0].Input)
	require.Equal(t, square.CreatePaymentBranchCreated, cashCalls[0].Branch)
	refundFlowID := "square-refund-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	refundInput := Request{RefundPaymentID: "pay_101", AmountMoney: square.Money{Amount: 1000, Currency: "USD"}}
	_, err = client.StartFlow(ctx, flow, refundFlowID, refundInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	refundResult, err := client.WaitForFlow(ctx, refundFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, refundResult.Status, refundResult.ErrorMessage)
	var refundOutcome Outcome
	require.NoError(t, refundResult.DecodeSingleOutput(&refundOutcome))
	require.Equal(t, "refund_101", refundOutcome.Refund.ID)
	refundCalls := mock.RefundPayment().Calls()
	require.Len(t, refundCalls, 1)
	require.Equal(t, refundFlowID, refundCalls[0].FlowID)
	require.Equal(t, square.RefundPaymentInput{PaymentID: "pay_101", AmountMoney: refundInput.AmountMoney}, refundCalls[0].Input)
	require.Equal(t, square.RefundPaymentBranchRefunded, refundCalls[0].Branch)
	customerFlowID := "square-customer-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	customerInput := Request{CreateCustomer: true, CustomerGivenName: "Ada", CustomerEmailAddress: "ada@example.com"}
	_, err = client.StartFlow(ctx, flow, customerFlowID, customerInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	customerResult, err := client.WaitForFlow(ctx, customerFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, customerResult.Status, customerResult.ErrorMessage)
	var customerOutcome Outcome
	require.NoError(t, customerResult.DecodeSingleOutput(&customerOutcome))
	require.Equal(t, "customer_101", customerOutcome.Customer.ID)
	customerCalls := mock.CreateCustomer().Calls()
	require.Len(t, customerCalls, 1)
	require.Equal(t, customerFlowID, customerCalls[0].FlowID)
	require.Equal(t, square.CreateCustomerInput{GivenName: "Ada", EmailAddress: "ada@example.com"}, customerCalls[0].Input)
	require.Equal(t, square.CreateCustomerBranchCreated, customerCalls[0].Branch)
	orderFlowID := "square-order-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	orderInput := Request{OrderLocationID: "loc_101", OrderLineItemName: "Coffee", OrderQuantity: "2", AmountMoney: square.Money{Amount: 500, Currency: "USD"}}
	_, err = client.StartFlow(ctx, flow, orderFlowID, orderInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	orderResult, err := client.WaitForFlow(ctx, orderFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, orderResult.Status, orderResult.ErrorMessage)
	var orderOutcome Outcome
	require.NoError(t, orderResult.DecodeSingleOutput(&orderOutcome))
	require.Equal(t, "order_101", orderOutcome.Order.ID)
	orderCalls := mock.CreateOrder().Calls()
	require.Len(t, orderCalls, 1)
	require.Equal(t, orderFlowID, orderCalls[0].FlowID)
	require.Equal(t, square.CreateOrderInput{LocationID: "loc_101", LineItemName: "Coffee", Quantity: "2", BasePriceMoney: orderInput.AmountMoney}, orderCalls[0].Input)
	require.Equal(t, square.CreateOrderBranchCreated, orderCalls[0].Branch)
	catalogFlowID := "square-catalog-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	catalogInput := Request{CatalogItemName: "Coffee"}
	_, err = client.StartFlow(ctx, flow, catalogFlowID, catalogInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	catalogResult, err := client.WaitForFlow(ctx, catalogFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, catalogResult.Status, catalogResult.ErrorMessage)
	var catalogOutcome Outcome
	require.NoError(t, catalogResult.DecodeSingleOutput(&catalogOutcome))
	require.Equal(t, "item_101", catalogOutcome.CatalogObject.Object["id"])
	catalogCalls := mock.UpsertCatalogObject().Calls()
	require.Len(t, catalogCalls, 1)
	require.Equal(t, catalogFlowID, catalogCalls[0].FlowID)
	require.Equal(t, MapToUpsertCatalogObjectInput(catalogInput), catalogCalls[0].Input)
	require.Equal(t, square.UpsertCatalogObjectBranchUpserted, catalogCalls[0].Branch)
	inventoryFlowID := "square-inventory-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	inventoryInput := Request{InventoryVariationID: "variation_101", InventoryLocationID: "loc_101", InventoryQuantity: "5", InventoryOccurredAt: "2026-11-17T12:00:00Z"}
	_, err = client.StartFlow(ctx, flow, inventoryFlowID, inventoryInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	inventoryResult, err := client.WaitForFlow(ctx, inventoryFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, inventoryResult.Status, inventoryResult.ErrorMessage)
	var inventoryOutcome Outcome
	require.NoError(t, inventoryResult.DecodeSingleOutput(&inventoryOutcome))
	require.Equal(t, "5", inventoryOutcome.InventoryCounts.Counts[0].Quantity)
	inventoryCalls := mock.SetInventoryPhysicalCount().Calls()
	require.Len(t, inventoryCalls, 1)
	require.Equal(t, inventoryFlowID, inventoryCalls[0].FlowID)
	require.Equal(t, MapToSetInventoryPhysicalCountInput(inventoryInput), inventoryCalls[0].Input)
	require.Equal(t, square.SetInventoryPhysicalCountBranchReconciled, inventoryCalls[0].Branch)
	invoiceFlowID := "square-invoice-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	invoiceInput := Request{InvoiceOrderID: "order_101", InvoiceLocationID: "loc_101", InvoiceCustomerID: "customer_101", InvoiceDueDate: "2030-01-24"}
	_, err = client.StartFlow(ctx, flow, invoiceFlowID, invoiceInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	invoiceResult, err := client.WaitForFlow(ctx, invoiceFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, invoiceResult.Status, invoiceResult.ErrorMessage)
	var invoiceOutcome Outcome
	require.NoError(t, invoiceResult.DecodeSingleOutput(&invoiceOutcome))
	require.Equal(t, "invoice_101", invoiceOutcome.Invoice.Invoice["id"])
	invoiceCalls := mock.CreateInvoice().Calls()
	require.Len(t, invoiceCalls, 1)
	require.Equal(t, invoiceFlowID, invoiceCalls[0].FlowID)
	require.Equal(t, MapToCreateInvoiceInput(invoiceInput), invoiceCalls[0].Input)
	require.Equal(t, square.CreateInvoiceBranchCreated, invoiceCalls[0].Branch)
	locationsFlowID := "square-locations-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	locationsInput := Request{ListLocations: true}
	_, err = client.StartFlow(ctx, flow, locationsFlowID, locationsInput, dex.StartFlowOptions{})
	require.NoError(t, err)
	locationsResult, err := client.WaitForFlow(ctx, locationsFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, locationsResult.Status, locationsResult.ErrorMessage)
	var locationsOutcome Outcome
	require.NoError(t, locationsResult.DecodeSingleOutput(&locationsOutcome))
	require.Equal(t, "loc_101", locationsOutcome.Locations.Locations[0]["id"])
	locationsCalls := mock.ListLocations().Calls()
	require.Len(t, locationsCalls, 1)
	require.Equal(t, locationsFlowID, locationsCalls[0].FlowID)
	require.Equal(t, square.ListLocationsInput{}, locationsCalls[0].Input)
	require.Equal(t, square.ListLocationsBranchListed, locationsCalls[0].Branch)
}
