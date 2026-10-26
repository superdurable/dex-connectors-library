//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedorder

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/connectors/xero/xeromock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestApprovedOrderExampleRunsOnXeroMockWithRealDex(t *testing.T) {
	mock := xeromock.New(t, ConnectionName)
	mock.ListInvoices().Default(xeromock.ListInvoicesOrderWithoutInvoice())
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "xero-approved-order-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	order := Input{
		OrderID: " ORDER-1042 ", CustomerEmail: " accounts@cityagency.example.com ", CurrencyCode: "USD",
		InvoiceDate: "2026-10-01", DueDate: "2026-10-15", PaidOn: "2026-10-01", PaymentAccountCode: "090",
		Lines: []OrderLine{{Description: " Spring bouquet ", Quantity: "1", UnitAmount: "49.95", AccountCode: "200", TaxType: "NONE"}},
	}
	_, err = client.StartFlow(ctx, flow, flowID, order, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome InvoicedOrderOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, OrderInvoicedAndPaid, outcome.Action)
	require.Equal(t, "ORDER-1042", outcome.OrderID)
	require.True(t, outcome.IsFullyPaid)
	require.Equal(t, xero.InvoiceStatusPaid, outcome.InvoiceStatus)
	require.Equal(t, xero.Decimal("0.00"), outcome.AmountDue)

	contactCalls := mock.FindContactByEmail().Calls()
	require.Len(t, contactCalls, 1)
	require.Equal(t, flowID, contactCalls[0].FlowID)
	require.Equal(t, xero.FindContactByEmailInput{EmailAddress: "accounts@cityagency.example.com"}, contactCalls[0].Input)
	listCalls := mock.ListInvoices().Calls()
	require.Len(t, listCalls, 1)
	require.Equal(t, xero.ListInvoicesInput{
		Statuses:   []xero.InvoiceStatus{xero.InvoiceStatusAuthorised, xero.InvoiceStatusPaid},
		ContactIDs: []string{"025867f1-d741-4d6b-b1af-9ac774b59ba7"}, Type: xero.InvoiceTypeAccountsReceivable,
		Reference: "ORDER-1042", PageSize: 10,
	}, listCalls[0].Input)
	createCalls := mock.CreateInvoice().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, xero.CreateInvoiceInput{
		Type: xero.InvoiceTypeAccountsReceivable, ContactID: "025867f1-d741-4d6b-b1af-9ac774b59ba7",
		LineItems: []xero.InvoiceLineItemInput{{Description: "Spring bouquet", Quantity: "1", UnitAmount: "49.95", AccountCode: "200", TaxType: "NONE"}},
		Date:      "2026-10-01", DueDate: "2026-10-15", LineAmountTypes: xero.LineAmountTypesExclusive,
		CurrencyCode: "USD", Reference: "ORDER-1042", Status: xero.InvoiceStatusAuthorised,
	}, createCalls[0].Input)
	paymentCalls := mock.RecordPayment().Calls()
	require.Len(t, paymentCalls, 1)
	require.Equal(t, xero.RecordPaymentInput{
		InvoiceID: "7b3db23e-1c22-43a1-b11e-dac2e5637d70", AccountCode: "090", Date: "2026-10-01", Amount: "49.95",
	}, paymentCalls[0].Input)
	readCalls := mock.GetInvoice().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, xero.GetInvoiceInput{InvoiceID: "7b3db23e-1c22-43a1-b11e-dac2e5637d70"}, readCalls[0].Input)
}
