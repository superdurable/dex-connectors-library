// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedorder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validOrder() Input {
	return Input{
		OrderID: " ORDER-1042 ", CustomerEmail: " accounts@cityagency.example.com ", CurrencyCode: "USD",
		InvoiceDate: "2026-10-01", DueDate: "2026-10-15", PaidOn: "2026-10-01", PaymentAccountCode: "090",
		Lines: []OrderLine{{Description: " Spring bouquet ", Quantity: "2", UnitAmount: "49.95", AccountCode: "200", TaxType: "OUTPUT"}},
	}
}

func TestBuildApprovedOrderTrimsAndValidatesStartFlowInput(t *testing.T) {
	order, err := BuildApprovedOrder(validOrder())
	require.NoError(t, err)
	require.Equal(t, "ORDER-1042", order.OrderID)
	require.Equal(t, "accounts@cityagency.example.com", order.CustomerEmail)
	require.Equal(t, "Spring bouquet", order.Lines[0].Description)

	for name, change := range map[string]func(*Input){
		"display-name email":  func(input *Input) { input.CustomerEmail = "City Agency <accounts@cityagency.example.com>" },
		"order ID with space": func(input *Input) { input.OrderID = "ORDER 1042" },
		"missing currency":    func(input *Input) { input.CurrencyCode = "" },
		"unparseable date":    func(input *Input) { input.PaidOn = "01/10/2026" },
		"no lines":            func(input *Input) { input.Lines = nil },
		"line without amount": func(input *Input) { input.Lines[0].UnitAmount = "" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validOrder()
			input.Lines = append([]OrderLine(nil), input.Lines...)
			change(&input)
			_, err := BuildApprovedOrder(input)
			require.Error(t, err)
		})
	}
}

func TestMappersCarryExactAmountsAndTheOrderReference(t *testing.T) {
	order, err := BuildApprovedOrder(validOrder())
	require.NoError(t, err)
	invoice := MapToCreateInvoiceInput(OrderInvoiceRequest{ContactID: "025867f1-d741-4d6b-b1af-9ac774b59ba7", Order: order})
	require.Equal(t, xero.InvoiceTypeAccountsReceivable, invoice.Type)
	require.Equal(t, xero.InvoiceStatusAuthorised, invoice.Status, "only an approved invoice can take the payment")
	require.Equal(t, "ORDER-1042", invoice.Reference)
	require.Empty(t, invoice.InvoiceNumber, "Xero numbers the invoice from the organisation's settings")
	require.Equal(t, xero.Decimal("49.95"), invoice.LineItems[0].UnitAmount)

	lookup := MapToListInvoicesInput(OrderInvoiceLookup{ContactID: invoice.ContactID, OrderID: order.OrderID})
	require.Equal(t, "ORDER-1042", lookup.Reference)
	require.Equal(t, []xero.InvoiceStatus{xero.InvoiceStatusAuthorised, xero.InvoiceStatusPaid}, lookup.Statuses)
	require.Equal(t, `Type=="ACCREC" AND Reference=="ORDER-1042"`, xero.BuildInvoiceWhereFilter(lookup))

	payment := MapToRecordPaymentInput(OrderPayment{InvoiceID: "inv", Amount: "122.39", PaidOn: "2026-10-01", AccountCode: "090", Reference: "ch_1"})
	require.Equal(t, xero.Decimal("122.39"), payment.Amount)
	require.Equal(t, "090", payment.AccountCode)
	require.Empty(t, payment.AccountID)

	readBack := MapToGetInvoiceInput(sdkgo.MutationResult[xero.RecordPaymentOutput]{Value: xero.RecordPaymentOutput{InvoiceID: "inv"}})
	require.Equal(t, xero.GetInvoiceInput{InvoiceID: "inv"}, readBack)
}

func TestChooseOrderInvoicePrefersAPaidExactReference(t *testing.T) {
	invoices := []xero.Invoice{
		{InvoiceID: "lookalike", Reference: "ORDER-10420", Status: xero.InvoiceStatusAuthorised},
		{InvoiceID: "approved", Reference: "ORDER-1042", Status: xero.InvoiceStatusAuthorised},
		{InvoiceID: "voided", Reference: "ORDER-1042", Status: xero.InvoiceStatusVoided},
		{InvoiceID: "paid", Reference: "ORDER-1042", Status: xero.InvoiceStatusPaid},
	}
	chosen, isFound := ChooseOrderInvoice(invoices, "ORDER-1042")
	require.True(t, isFound)
	require.Equal(t, "paid", chosen.InvoiceID)
	chosen, isFound = ChooseOrderInvoice(invoices[:3], "ORDER-1042")
	require.True(t, isFound)
	require.Equal(t, "approved", chosen.InvoiceID)
	_, isFound = ChooseOrderInvoice(invoices[:1], "ORDER-1042")
	require.False(t, isFound, "a look-alike reference is not the order's invoice")
}
