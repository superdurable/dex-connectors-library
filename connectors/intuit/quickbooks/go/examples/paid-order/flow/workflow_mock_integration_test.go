//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paidorder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/quickbooksmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestPaidOrderUsesQuickBooksMocksAndRetriesPaymentWithRealDex(t *testing.T) {
	mock := quickbooksmock.New(t, ConnectionName)
	mock.RecordPayment().Respond(
		quickbooksmock.RecordPaymentRetry(sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "quickbooks", Operation: "recordPayment", Message: "temporary outage"}, 0),
		quickbooksmock.RecordPaymentDefault(),
	)
	harness := newOrderHarnessForFlow(t, NewFlow(mock.Connection()), nil)
	input := integrationOrder()
	outcome := harness.runOrder(t, "mock-retry", input)

	require.Equal(t, OrderExistingInvoicePaid, outcome.Action)
	require.Equal(t, input.OrderID, outcome.OrderID)
	require.Equal(t, "58", outcome.CustomerID)
	require.Equal(t, "130", outcome.InvoiceID)
	require.Equal(t, quickbooks.Decimal("112.40"), outcome.TotalAmount)
	require.Equal(t, quickbooks.Decimal("0"), outcome.Balance)
	require.True(t, outcome.IsFullyPaid)
	require.Equal(t, quickbooks.EmailStatusEmailSent, outcome.EmailStatus)
	require.Equal(t, "186", outcome.PaymentID)

	finds := mock.FindCustomer().Calls()
	require.Len(t, finds, 1)
	require.Equal(t, quickbooks.FindCustomerInput{EmailAddress: "accounts@cityagency.example.com"}, finds[0].Input)
	lists := mock.ListInvoices().Calls()
	require.Len(t, lists, 1)
	require.Equal(t, quickbooks.ListInvoicesInput{CustomerID: "58", DocNumber: "ORDER-1042", MaxResults: 10}, lists[0].Input)
	payments := mock.RecordPayment().Calls()
	require.Len(t, payments, 2)
	expectedPayment := quickbooks.RecordPaymentInput{CustomerID: "58", InvoiceID: "130", Amount: "112.40", TxnDate: "2026-10-01", DepositAccountID: "35", PaymentReferenceNumber: "ch_3QxF2a"}
	require.Equal(t, expectedPayment, payments[0].Input)
	require.Equal(t, expectedPayment, payments[1].Input)
	require.Equal(t, payments[0].CallID, payments[1].CallID)
	sends := mock.SendInvoice().Calls()
	require.Len(t, sends, 1)
	require.Equal(t, quickbooks.SendInvoiceInput{InvoiceID: "130", SendTo: "accounts@cityagency.example.com"}, sends[0].Input)
	reads := mock.GetInvoice().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, quickbooks.GetInvoiceInput{InvoiceID: "130"}, reads[0].Input)
	require.Empty(t, mock.CreateCustomer().Calls())
	require.Empty(t, mock.CreateInvoice().Calls())
}
