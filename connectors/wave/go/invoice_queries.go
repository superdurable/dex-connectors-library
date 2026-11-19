// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listInvoicesGraphQL = "query ListInvoices($businessId: ID!, $page: Int!, $pageSize: Int!) { business(id: $businessId) { invoices(page: $page, pageSize: $pageSize) { pageInfo { currentPage totalPages totalCount } edges { node { id status title invoiceNumber customer { id } } } } } }"
const getInvoiceGraphQL = "query GetInvoice($businessId: ID!, $invoiceId: ID!) { business(id: $businessId) { invoice(id: $invoiceId) { id status title invoiceNumber customer { id } } } }"

// InvoiceProfile contains public Wave invoice metadata.
type InvoiceProfile struct {
	// BusinessID identifies the queried business.
	BusinessID string `json:"businessId"`
	// ID is the Wave invoice ID.
	ID string `json:"id"`
	// CustomerID identifies the invoice customer.
	CustomerID string `json:"customerId"`
	// Status is the Wave invoice status.
	Status string `json:"status"`
	// Title is the invoice title.
	Title string `json:"title"`
	// InvoiceNumber is the Wave invoice number.
	InvoiceNumber string `json:"invoiceNumber"`
}

// InvoicePage contains one page of invoices.
type InvoicePage struct {
	// BusinessID identifies the queried business.
	BusinessID string `json:"businessId"`
	// Invoices contains the returned invoices.
	Invoices []InvoiceProfile `json:"invoices"`
	// CurrentPage is the returned 1-based page.
	CurrentPage int `json:"currentPage"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
	// TotalCount is the number of matching invoices.
	TotalCount int `json:"totalCount"`
}

// ListInvoicesInput selects a business and one invoice page.
type ListInvoicesInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// Page is the 1-based page number.
	Page int `json:"page"`
	// PageSize is the number of invoices, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// GetInvoiceInput selects one invoice within a business.
type GetInvoiceInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// InvoiceID is the exact Wave invoice ID.
	InvoiceID string `json:"invoiceId"`
}

type invoiceWire struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Title         string `json:"title"`
	InvoiceNumber string `json:"invoiceNumber"`
	Customer      *struct {
		ID string `json:"id"`
	} `json:"customer"`
}

// ListInvoicesOperation reads one invoice page.
type ListInvoicesOperation struct{ client *Client }

// ListInvoices binds invoice listing to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// Definition returns the generated Query contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke reads the requested invoice page.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	if !validGraphQLID(input.BusinessID) || input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return waveQueryBranch[InvoicePage]("listInvoices", "defect", sdkgo.FailureValidation, "Wave invoice page input is invalid")
	}
	variables := struct {
		BusinessID string `json:"businessId"`
		Page       int    `json:"page"`
		PageSize   int    `json:"pageSize"`
	}{input.BusinessID, input.Page, input.PageSize}
	response := operation.client.sendGraphQL(call, call.Context, listInvoicesGraphQL, variables, false)
	if retry, hasRetry := waveQueryRetry[InvoicePage]("listInvoices", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveQueryBranch[InvoicePage]("listInvoices", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveQueryBranch[InvoicePage]("listInvoices", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice listing")
	}
	if response.err != nil {
		return waveQueryBranch[InvoicePage]("listInvoices", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave invoice response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Business *struct {
				Invoices *struct {
					PageInfo struct {
						CurrentPage int `json:"currentPage"`
						TotalPages  int `json:"totalPages"`
						TotalCount  int `json:"totalCount"`
					} `json:"pageInfo"`
					Edges []struct {
						Node invoiceWire `json:"node"`
					} `json:"edges"`
				} `json:"invoices"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return waveQueryBranch[InvoicePage]("listInvoices", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid invoice JSON")
	}
	if wire.Data.Business == nil {
		return waveQueryBranch[InvoicePage]("listInvoices", "notFound", sdkgo.FailureNotFound, "Wave did not find the business")
	}
	if wire.Data.Business.Invoices == nil || wire.Data.Business.Invoices.Edges == nil {
		return waveQueryBranch[InvoicePage]("listInvoices", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete invoice data")
	}
	page := InvoicePage{BusinessID: input.BusinessID, Invoices: []InvoiceProfile{}, CurrentPage: wire.Data.Business.Invoices.PageInfo.CurrentPage, TotalPages: wire.Data.Business.Invoices.PageInfo.TotalPages, TotalCount: wire.Data.Business.Invoices.PageInfo.TotalCount}
	for _, edge := range wire.Data.Business.Invoices.Edges {
		profile, isValid := edge.Node.profile(input.BusinessID)
		if !isValid {
			return waveQueryBranch[InvoicePage]("listInvoices", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete invoice data")
		}
		page.Invoices = append(page.Invoices, profile)
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), page, nil, sdkgo.Receipt{})
}

// GetInvoiceOperation reads one invoice.
type GetInvoiceOperation struct{ client *Client }

// GetInvoice binds an invoice lookup to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// Definition returns the generated Query contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads one invoice by business and invoice IDs.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[InvoiceProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.InvoiceID) {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "defect", sdkgo.FailureValidation, "Wave invoice IDs are invalid")
	}
	variables := struct {
		BusinessID string `json:"businessId"`
		InvoiceID  string `json:"invoiceId"`
	}{input.BusinessID, input.InvoiceID}
	response := operation.client.sendGraphQL(call, call.Context, getInvoiceGraphQL, variables, false)
	if retry, hasRetry := waveQueryRetry[InvoiceProfile]("getInvoice", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice lookup")
	}
	if response.err != nil {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave invoice response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Business *struct {
				Invoice *invoiceWire `json:"invoice"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid invoice JSON")
	}
	if wire.Data.Business == nil || wire.Data.Business.Invoice == nil {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "notFound", sdkgo.FailureNotFound, "Wave did not find the invoice")
	}
	profile, isValid := wire.Data.Business.Invoice.profile(input.BusinessID)
	if !isValid || profile.ID != input.InvoiceID {
		return waveQueryBranch[InvoiceProfile]("getInvoice", "invalidResponse", sdkgo.FailureProtocol, "Wave returned inconsistent invoice data")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), profile, nil, sdkgo.Receipt{})
}

func (wire invoiceWire) profile(businessID string) (InvoiceProfile, bool) {
	if wire.ID == "" || wire.Status == "" || wire.Customer == nil || wire.Customer.ID == "" {
		return InvoiceProfile{}, false
	}
	return InvoiceProfile{BusinessID: businessID, ID: wire.ID, CustomerID: wire.Customer.ID, Status: wire.Status, Title: wire.Title, InvoiceNumber: wire.InvoiceNumber}, true
}
