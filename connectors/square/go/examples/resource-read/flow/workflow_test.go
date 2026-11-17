// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/square/go"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{Cursor: ""}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, square.ListPaymentsInput{Cursor: ""}, MapToOperationInput(prepared))
	require.Equal(t, square.CreatePaymentInput{SourceID: "CASH", AmountMoney: square.Money{Amount: 2900, Currency: "USD"}}, MapToCreatePaymentInput(Request{RecordCashPayment: true, AmountMoney: square.Money{Amount: 2900, Currency: "USD"}}))
	require.Equal(t, square.RefundPaymentInput{PaymentID: "pay_101", AmountMoney: square.Money{Amount: 1000, Currency: "USD"}}, MapToRefundPaymentInput(Request{RefundPaymentID: "pay_101", AmountMoney: square.Money{Amount: 1000, Currency: "USD"}}))
	require.Equal(t, square.CreateCustomerInput{GivenName: "Ada", EmailAddress: "ada@example.com"}, MapToCreateCustomerInput(Request{CreateCustomer: true, CustomerGivenName: "Ada", CustomerEmailAddress: "ada@example.com"}))
	require.Equal(t, square.CreateOrderInput{LocationID: "loc_101", LineItemName: "Coffee", Quantity: "2", BasePriceMoney: square.Money{Amount: 500, Currency: "USD"}}, MapToCreateOrderInput(Request{OrderLocationID: "loc_101", OrderLineItemName: "Coffee", OrderQuantity: "2", AmountMoney: square.Money{Amount: 500, Currency: "USD"}}))
	require.Equal(t, "Coffee", MapToUpsertCatalogObjectInput(Request{CatalogItemName: "Coffee"}).Object["item_data"].(map[string]any)["name"])
	require.Equal(t, square.SetInventoryPhysicalCountInput{CatalogObjectID: "variation_101", LocationID: "loc_101", Quantity: "5", OccurredAt: "2026-11-17T12:00:00Z"}, MapToSetInventoryPhysicalCountInput(Request{InventoryVariationID: "variation_101", InventoryLocationID: "loc_101", InventoryQuantity: "5", InventoryOccurredAt: "2026-11-17T12:00:00Z"}))
	require.Equal(t, square.CreateInvoiceInput{OrderID: "order_101", LocationID: "loc_101", CustomerID: "customer_101", DueDate: "2030-01-24"}, MapToCreateInvoiceInput(Request{InvoiceOrderID: "order_101", InvoiceLocationID: "loc_101", InvoiceCustomerID: "customer_101", InvoiceDueDate: "2030-01-24"}))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(square.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
