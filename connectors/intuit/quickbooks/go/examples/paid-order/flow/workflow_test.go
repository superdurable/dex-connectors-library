// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paidorder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validOrder() Input {
	return Input{
		OrderID: " ORDER-1042 ", CustomerEmail: " accounts@cityagency.example.com ", CustomerDisplayName: " City Agency ",
		InvoiceDate: "2026-10-01", DueDate: "2026-10-15", PaidOn: "2026-10-01",
		Lines: []OrderLine{{ItemID: " 1 ", Description: " Spring bouquet ", Quantity: "2", UnitPrice: "49.95"}},
	}
}

func TestBuildPaidOrderTrimsAndValidatesStartFlowInput(t *testing.T) {
	order, err := BuildPaidOrder(validOrder())
	require.NoError(t, err)
	require.Equal(t, "ORDER-1042", order.OrderID)
	require.Equal(t, "accounts@cityagency.example.com", order.CustomerEmail)
	require.Equal(t, "City Agency", order.CustomerDisplayName)
	require.Equal(t, "1", order.Lines[0].ItemID)
	require.Equal(t, "Spring bouquet", order.Lines[0].Description)

	for name, change := range map[string]func(*Input){
		"display-name email":     func(input *Input) { input.CustomerEmail = "City Agency <accounts@cityagency.example.com>" },
		"order ID with space":    func(input *Input) { input.OrderID = "ORDER 1042" },
		"order ID too long":      func(input *Input) { input.OrderID = "ORDER-1042-AND-MORE-TEXT" },
		"missing display name":   func(input *Input) { input.CustomerDisplayName = "" },
		"colon in display name":  func(input *Input) { input.CustomerDisplayName = "Parent:Child" },
		"unparseable date":       func(input *Input) { input.PaidOn = "01/10/2026" },
		"no lines":               func(input *Input) { input.Lines = nil },
		"line without item":      func(input *Input) { input.Lines[0].ItemID = "" },
		"line without unitPrice": func(input *Input) { input.Lines[0].UnitPrice = "" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validOrder()
			input.Lines = append([]OrderLine(nil), input.Lines...)
			change(&input)
			_, err := BuildPaidOrder(input)
			require.Error(t, err)
		})
	}
}

func TestMappersCarryExactAmountsAndTheOrderNumber(t *testing.T) {
	order, err := BuildPaidOrder(validOrder())
	require.NoError(t, err)
	require.Equal(t, quickbooks.FindCustomerInput{EmailAddress: "accounts@cityagency.example.com"}, MapToFindCustomerInput(order))
	invoice := MapToCreateInvoiceInput(OrderInvoiceRequest{CustomerID: "58", Order: order})
	require.Equal(t, "ORDER-1042", invoice.DocNumber, "the order ID becomes the invoice number the lookup searches")
	require.Equal(t, "accounts@cityagency.example.com", invoice.BillEmail)
	require.Equal(t, quickbooks.Decimal("49.95"), invoice.Lines[0].UnitPrice)
	require.Empty(t, invoice.Lines[0].Amount, "the connector computes the line amount exactly")

	lookup := MapToListInvoicesInput(OrderInvoiceLookup{CustomerID: "58", OrderID: order.OrderID})
	require.Equal(t, quickbooks.ListInvoicesInput{CustomerID: "58", DocNumber: "ORDER-1042", MaxResults: 10}, lookup)

	payment := MapToRecordPaymentInput(OrderPayment{CustomerID: "58", InvoiceID: "130", Amount: "112.40", PaidOn: "2026-10-01", Reference: "ch_1"})
	require.Equal(t, quickbooks.Decimal("112.40"), payment.Amount)
	require.Equal(t, "ch_1", payment.PaymentReferenceNumber)
	require.Empty(t, payment.DepositAccountID, "blank deposits to Undeposited Funds")

	require.Equal(t, quickbooks.SendInvoiceInput{InvoiceID: "130", SendTo: "a@example.com"}, MapToSendInvoiceInput(InvoiceDelivery{InvoiceID: "130", SendTo: "a@example.com"}))
	readBack := MapToGetInvoiceInput(sdkgo.MutationResult[quickbooks.Invoice]{Value: quickbooks.Invoice{InvoiceID: "130"}})
	require.Equal(t, quickbooks.GetInvoiceInput{InvoiceID: "130"}, readBack)
}

func TestChooseOrderInvoicePrefersAPaidExactNumber(t *testing.T) {
	invoices := []quickbooks.Invoice{
		{InvoiceID: "lookalike", DocNumber: "ORDER-10420", Balance: "0"},
		{InvoiceID: "open", DocNumber: "ORDER-1042", Balance: "75.40"},
		{InvoiceID: "paid", DocNumber: "order-1042", Balance: "0"},
	}
	chosen, isFound := ChooseOrderInvoice(invoices, "ORDER-1042")
	require.True(t, isFound)
	require.Equal(t, "paid", chosen.InvoiceID, "QuickBooks compares invoice numbers ignoring case")
	chosen, isFound = ChooseOrderInvoice(invoices[:2], "ORDER-1042")
	require.True(t, isFound)
	require.Equal(t, "open", chosen.InvoiceID)
	_, isFound = ChooseOrderInvoice(invoices[:1], "ORDER-1042")
	require.False(t, isFound, "a look-alike number is not the order's invoice")
}
