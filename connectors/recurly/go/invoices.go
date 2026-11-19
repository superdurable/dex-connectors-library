// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// InvoiceSummary contains only the invoice ID and lifecycle state.
type InvoiceSummary struct {
	// ID is the Recurly invoice ID.
	ID string `json:"id"`
	// State is the provider lifecycle state, such as active or canceled.
	State string `json:"state"`
}

// InvoicePage contains a bounded list of invoice states.
type InvoicePage struct {
	// Invoices excludes account, billing, and payment details.
	Invoices []InvoiceSummary `json:"invoices"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListInvoicesInput selects a page of 1 to 200 invoices.
type ListInvoicesInput struct {
	// Limit is the requested page size from 1 to 200.
	Limit int `json:"limit"`
}

// GetInvoiceInput selects one invoice by ID.
type GetInvoiceInput struct {
	// InvoiceID is the Recurly invoice ID.
	InvoiceID string `json:"invoiceId"`
}

// ListInvoicesOperation lists invoice states.
type ListInvoicesOperation struct{ client *Client }

// ListInvoices binds the Query to this client.
func (client *Client) ListInvoices() ListInvoicesOperation {
	return ListInvoicesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition {
	return ListInvoicesDefinition
}

// Invoke lists invoices without customer or payment details.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	if input.Limit < 1 || input.Limit > 200 {
		return invoicePageBranch(ListInvoicesBranchDefect, sdkgo.FailureValidation, "Recurly invoice page size is invalid")
	}
	response := operation.client.get(call, "/invoices?limit="+strconv.Itoa(input.Limit))
	if errors.Is(response.err, errCredential) {
		return invoicePageBranch(ListInvoicesBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return invoicePageRetry(sdkgo.FailureTransport, "Recurly invoice list request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return invoicePageRetry(sdkgo.FailureRateLimit, "Recurly rate limited invoice listing", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return invoicePageRetry(sdkgo.FailureAvailability, "Recurly invoice listing is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return invoicePageBranch(ListInvoicesBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected invoice listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return invoicePageBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly invoice list exceeded the response limit")
	}
	if response.err != nil {
		return invoicePageRetry(sdkgo.FailureTransport, "Recurly invoice list was interrupted", 0)
	}
	var wire struct {
		Object  string            `json:"object"`
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return invoicePageBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly invoice list is invalid")
	}
	page := InvoicePage{Invoices: make([]InvoiceSummary, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, raw := range wire.Data {
		summary, valid := decodeInvoice(raw)
		if !valid {
			return invoicePageBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly invoice list item is invalid")
		}
		page.Invoices = append(page.Invoices, summary)
	}
	return sdkgo.NewQueryBranch(ListInvoicesBranchListed, page, nil, sdkgo.Receipt{})
}

// GetInvoiceOperation reads one invoice state.
type GetInvoiceOperation struct{ client *Client }

// GetInvoice binds the Query to this client.
func (client *Client) GetInvoice() GetInvoiceOperation {
	return GetInvoiceOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads one invoice by ID.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[InvoiceSummary] {
	if !validAccountID(input.InvoiceID) {
		return getInvoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureValidation, "Recurly invoice ID is invalid")
	}
	response := operation.client.get(call, "/invoices/"+url.PathEscape(input.InvoiceID))
	if errors.Is(response.err, errCredential) {
		return getInvoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return getInvoiceRetry(sdkgo.FailureTransport, "Recurly invoice request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return getInvoiceRetry(sdkgo.FailureRateLimit, "Recurly rate limited invoice lookup", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return getInvoiceRetry(sdkgo.FailureAvailability, "Recurly invoice lookup is unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getInvoiceBranch(GetInvoiceBranchNotFound, sdkgo.FailureNotFound, "Recurly invoice was not found")
	}
	if response.status != http.StatusOK {
		return getInvoiceBranch(GetInvoiceBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected invoice lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getInvoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly invoice exceeded the response limit")
	}
	if response.err != nil {
		return getInvoiceRetry(sdkgo.FailureTransport, "Recurly invoice response was interrupted", 0)
	}
	summary, valid := decodeInvoice(response.body)
	if !valid || summary.ID != input.InvoiceID {
		return getInvoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly invoice response is invalid")
	}
	return sdkgo.NewQueryBranch(GetInvoiceBranchFound, summary, nil, sdkgo.Receipt{})
}

func decodeInvoice(body []byte) (InvoiceSummary, bool) {
	var wire struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if json.Unmarshal(body, &wire) != nil || !validAccountID(wire.ID) || wire.State == "" {
		return InvoiceSummary{}, false
	}
	return InvoiceSummary{ID: wire.ID, State: wire.State}, true
}
func invoiceFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: operation, Message: message}
}
func invoicePageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InvoicePage] {
	failure := invoiceFailure("listInvoices", kind, message)
	return sdkgo.NewQueryBranch(branch, InvoicePage{}, &failure, sdkgo.Receipt{})
}
func invoicePageRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[InvoicePage] {
	return sdkgo.NewQueryRetry[InvoicePage](invoiceFailure("listInvoices", kind, message), after)
}
func getInvoiceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InvoiceSummary] {
	failure := invoiceFailure("getInvoice", kind, message)
	return sdkgo.NewQueryBranch(branch, InvoiceSummary{}, &failure, sdkgo.Receipt{})
}
func getInvoiceRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[InvoiceSummary] {
	return sdkgo.NewQueryRetry[InvoiceSummary](invoiceFailure("getInvoice", kind, message), after)
}
