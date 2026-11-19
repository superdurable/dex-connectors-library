// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshbooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListInvoicesInput selects a one-based invoice page; zero selects page one.
type ListInvoicesInput struct {
	AccountID string `json:"accountId"`
	Page      int    `json:"page,omitempty"`
}

// InvoicePage contains one page of invoices without client contact details.
type InvoicePage struct {
	AccountID string    `json:"accountId"`
	Invoices  []Invoice `json:"invoices"`
	Page      int       `json:"page"`
	PageCount int       `json:"pageCount"`
	Total     int       `json:"total"`
}

// ListInvoicesOperation reads one invoice page with the existing invoices:read grant.
type ListInvoicesOperation struct{ client *Client }

// ListInvoices binds the Query to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// Definition returns the generated invoice list Query contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke reads one bounded invoice page and classifies provider failures.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	if input.Page == 0 {
		input.Page = 1
	}
	if !accountIDPattern.MatchString(input.AccountID) || input.Page < 1 || input.Page > 10000 {
		return invoicePageBranch(ListInvoicesBranchDefect, sdkgo.FailureValidation, "FreshBooks account ID or page is invalid")
	}
	path := "/accounting/account/" + input.AccountID + "/invoices/invoices?page=" + strconv.Itoa(input.Page) + "&per_page=100"
	response := operation.client.get(call, path)
	switch {
	case errors.Is(response.err, errCredential):
		return invoicePageBranch(ListInvoicesBranchDefect, sdkgo.FailureAuthentication, "FreshBooks OAuth credentials are unavailable or invalid")
	case errors.Is(response.err, errRequest):
		return invoicePageRetry(sdkgo.FailureTransport, "FreshBooks request failed", 0)
	case response.status == http.StatusTooManyRequests:
		return invoicePageRetry(sdkgo.FailureRateLimit, "FreshBooks rate limited the request", response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		return invoicePageRetry(sdkgo.FailureAvailability, "FreshBooks is temporarily unavailable", response.retryAfter)
	case errors.Is(response.err, providerhttp.ErrBodyTooLarge):
		return invoicePageBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FreshBooks response exceeded the configured limit")
	case response.err != nil:
		return invoicePageRetry(sdkgo.FailureTransport, "FreshBooks response was interrupted", 0)
	case response.status == http.StatusNotFound:
		return invoicePageBranch(ListInvoicesBranchMissing, sdkgo.FailureNotFound, "FreshBooks did not find the account")
	case response.status != http.StatusOK:
		return invoicePageBranch(ListInvoicesBranchProviderRejected, sdkgo.FailureProviderRejection, "FreshBooks rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeInvoicePage(response.body, input)
	if err != nil {
		return invoicePageBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureProtocol, "FreshBooks returned invalid invoice page JSON")
	}
	return sdkgo.NewQueryBranch(ListInvoicesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeInvoicePage(body []byte, input ListInvoicesInput) (InvoicePage, error) {
	var envelope struct {
		Response struct {
			Result struct {
				Invoices []json.RawMessage `json:"invoices"`
				Page     int               `json:"page"`
				Pages    int               `json:"pages"`
				Total    int               `json:"total"`
			} `json:"result"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return InvoicePage{}, err
	}
	result := envelope.Response.Result
	if result.Invoices == nil || result.Page != input.Page || result.Pages < 0 || result.Total < 0 {
		return InvoicePage{}, fmt.Errorf("FreshBooks invoice page metadata is invalid")
	}
	page := InvoicePage{AccountID: input.AccountID, Invoices: make([]Invoice, 0, len(result.Invoices)), Page: result.Page, PageCount: result.Pages, Total: result.Total}
	for _, raw := range result.Invoices {
		var identity struct {
			InvoiceID int64 `json:"invoiceid"`
		}
		if json.Unmarshal(raw, &identity) != nil || identity.InvoiceID <= 0 {
			return InvoicePage{}, fmt.Errorf("FreshBooks invoice identity is invalid")
		}
		wrapped, err := json.Marshal(map[string]any{"response": map[string]any{"result": map[string]any{"invoice": json.RawMessage(raw)}}})
		if err != nil {
			return InvoicePage{}, err
		}
		invoice, err := decodeInvoice(wrapped, GetInvoiceInput{AccountID: input.AccountID, InvoiceID: identity.InvoiceID})
		if err != nil {
			return InvoicePage{}, err
		}
		page.Invoices = append(page.Invoices, invoice)
	}
	return page, nil
}

func invoicePageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "freshbooks", Operation: "listInvoices", Message: message}
}
func invoicePageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InvoicePage] {
	failure := invoicePageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, InvoicePage{}, &failure, sdkgo.Receipt{})
}
func invoicePageRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[InvoicePage] {
	return sdkgo.NewQueryRetry[InvoicePage](invoicePageFailure(kind, message), delay)
}
