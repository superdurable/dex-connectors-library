// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getInvoiceOperationID = "getInvoice"

// GetInvoiceInput identifies one invoice.
type GetInvoiceInput struct {
	// InvoiceID is the invoice's QuickBooks Id, such as 130.
	InvoiceID string `json:"invoiceId"`
}

// GetInvoiceOperation implements the getInvoice Query.
type GetInvoiceOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads GET /invoice/{invoiceId}; an answer about another invoice selects invalidResponse.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[Invoice] {
	client := operation.client
	invoiceID := strings.TrimSpace(input.InvoiceID)
	requested := Invoice{InvoiceID: invoiceID}
	if err := validateEntityID("invoiceId", invoiceID); err != nil {
		return sdkgo.NewQueryBranch(GetInvoiceBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, getInvoiceOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, getInvoiceOperationID, quickbooksRequest{method: http.MethodGet, path: "/invoice/" + invoiceID})
	receipt := client.receipt(call, result, invoiceID)
	if attempt, isTerminal := queryAttemptForExchange(result, requested, receipt, queryBranches{
		notFound: GetInvoiceBranchNotFound, providerRejected: GetInvoiceBranchProviderRejected,
		invalidResponse: GetInvoiceBranchInvalidResponse, defect: GetInvoiceBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeEntity[invoiceWire](result.response.body, "Invoice")
	var invoice Invoice
	if err == nil {
		invoice, err = decodeInvoice(wire)
	}
	if err == nil && invoice.InvoiceID != invoiceID {
		return sdkgo.NewQueryBranch(GetInvoiceBranchInvalidResponse, requested,
			quickbooksFailurePointer(sdkgo.FailureProtocol, getInvoiceOperationID, "QuickBooks returned another invoice"), receipt)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(GetInvoiceBranchInvalidResponse, requested,
			quickbooksFailurePointer(sdkgo.FailureProtocol, getInvoiceOperationID, "QuickBooks returned an invalid invoice: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetInvoiceBranchFound, invoice, nil, receipt)
}
