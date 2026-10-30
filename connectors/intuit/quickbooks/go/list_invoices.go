// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listInvoicesOperationID = "listInvoices"

// ListInvoicesInput filters one page of invoices. Every filter is optional, and filters combine with AND.
type ListInvoicesInput struct {
	// CustomerID keeps invoices of one customer, by Id.
	CustomerID string `json:"customerId,omitempty"`
	// DocNumber keeps invoices whose invoice number equals this value.
	DocNumber string `json:"docNumber,omitempty"`
	// IsOpenOnly keeps invoices whose Balance is above zero, that is, not fully paid.
	IsOpenOnly bool `json:"isOpenOnly,omitempty"`
	// TxnDateFrom keeps invoices dated on or after this date, YYYY-MM-DD.
	TxnDateFrom string `json:"txnDateFrom,omitempty"`
	// TxnDateTo keeps invoices dated on or before this date, YYYY-MM-DD.
	TxnDateTo string `json:"txnDateTo,omitempty"`
	// UpdatedSince keeps invoices created or changed at or after this instant, to the second.
	UpdatedSince *time.Time `json:"updatedSince,omitempty"`
	// StartPosition is the 1-based position of the first invoice to read, such as the previous
	// page's NextStartPosition; zero reads from the first invoice.
	StartPosition int `json:"startPosition,omitempty"`
	// MaxResults is 1 to 100 invoices; zero uses DefaultInvoicesPerPage.
	MaxResults int `json:"maxResults,omitempty"`
}

// InvoicePage is one page of invoices in ascending creation order.
type InvoicePage struct {
	// Invoices are the page's invoices; the list can be empty.
	Invoices []Invoice `json:"invoices"`
	// StartPosition is the 1-based position of the first invoice on the page.
	StartPosition int `json:"startPosition"`
	// MaxResults is the requested page size.
	MaxResults int `json:"maxResults"`
	// HasMorePages reports a full page, after which more invoices can follow.
	HasMorePages bool `json:"hasMorePages"`
	// NextStartPosition is the StartPosition of the next page when HasMorePages is true, and zero otherwise.
	NextStartPosition int `json:"nextStartPosition,omitempty"`
}

// ListInvoicesOperation implements the listInvoices Query.
type ListInvoicesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke runs one QuickBooks query on Invoice and returns its page.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	client := operation.client
	statement, startPosition, maxResults, err := buildListInvoicesQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListInvoicesBranchDefect, InvoicePage{},
			quickbooksFailurePointer(sdkgo.FailureValidation, listInvoicesOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, listInvoicesOperationID, quickbooksRequest{method: http.MethodGet, path: "/query", query: url.Values{"query": {statement}}})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, InvoicePage{}, receipt, queryBranches{
		providerRejected: ListInvoicesBranchProviderRejected, invalidResponse: ListInvoicesBranchInvalidResponse, defect: ListInvoicesBranchDefect,
	}); isTerminal {
		return attempt
	}
	page, err := decodeInvoicePage(result.response.body, startPosition, maxResults)
	if err != nil {
		return sdkgo.NewQueryBranch(ListInvoicesBranchInvalidResponse, InvoicePage{},
			quickbooksFailurePointer(sdkgo.FailureProtocol, listInvoicesOperationID, "QuickBooks returned an invalid invoice page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListInvoicesBranchListed, page, nil, receipt)
}

// BuildListInvoicesQuery returns the QuickBooks query listInvoices sends, such as
// select * from Invoice where CustomerRef = '58' AND Balance > '0' ORDERBY MetaData.CreateTime
// STARTPOSITION 1 MAXRESULTS 25, so an application can review it. It validates the input.
func BuildListInvoicesQuery(input ListInvoicesInput) (string, error) {
	statement, _, _, err := buildListInvoicesQuery(input)
	return statement, err
}

func buildListInvoicesQuery(input ListInvoicesInput) (string, int, int, error) {
	startPosition, maxResults := input.StartPosition, input.MaxResults
	if startPosition == 0 {
		startPosition = 1
	}
	if maxResults == 0 {
		maxResults = DefaultInvoicesPerPage
	}
	switch {
	case startPosition < 1:
		return "", 0, 0, errors.New("startPosition must be 1 or greater")
	case maxResults < 1 || maxResults > MaxInvoicesPerPage:
		return "", 0, 0, fmt.Errorf("maxResults must be between 1 and %d", MaxInvoicesPerPage)
	}
	var conditions []string
	if customerID := strings.TrimSpace(input.CustomerID); customerID != "" {
		if err := validateEntityID("customerId", customerID); err != nil {
			return "", 0, 0, err
		}
		conditions = append(conditions, "CustomerRef = "+quoteQueryLiteral(customerID))
	}
	if docNumber := strings.TrimSpace(input.DocNumber); docNumber != "" {
		if err := validateQueryLiteral("docNumber", docNumber, MaxDocNumberCharacters); err != nil {
			return "", 0, 0, err
		}
		conditions = append(conditions, "DocNumber = "+quoteQueryLiteral(docNumber))
	}
	if input.IsOpenOnly {
		conditions = append(conditions, "Balance > '0'")
	}
	for _, date := range []struct{ name, value, operator string }{{"txnDateFrom", input.TxnDateFrom, ">="}, {"txnDateTo", input.TxnDateTo, "<="}} {
		value := strings.TrimSpace(date.value)
		if value == "" {
			continue
		}
		if err := validateCalendarDate(date.name, value); err != nil {
			return "", 0, 0, err
		}
		conditions = append(conditions, "TxnDate "+date.operator+" "+quoteQueryLiteral(value))
	}
	if input.UpdatedSince != nil {
		conditions = append(conditions, "MetaData.LastUpdatedTime >= "+quoteQueryLiteral(input.UpdatedSince.UTC().Format(time.RFC3339)))
	}
	statement := "select * from Invoice"
	if len(conditions) != 0 {
		statement += " where " + strings.Join(conditions, " AND ")
	}
	statement += " ORDERBY MetaData.CreateTime STARTPOSITION " + strconv.Itoa(startPosition) + " MAXRESULTS " + strconv.Itoa(maxResults)
	return statement, startPosition, maxResults, nil
}

func decodeInvoicePage(body []byte, startPosition int, maxResults int) (InvoicePage, error) {
	elements, err := decodeQueryEntities[invoiceWire](body, "Invoice")
	if err != nil {
		return InvoicePage{}, err
	}
	if len(elements) > maxResults {
		return InvoicePage{}, errors.New("response has more invoices than requested")
	}
	page := InvoicePage{Invoices: make([]Invoice, 0, len(elements)), StartPosition: startPosition, MaxResults: maxResults}
	for _, element := range elements {
		invoice, err := decodeInvoice(element)
		if err != nil {
			return InvoicePage{}, err
		}
		page.Invoices = append(page.Invoices, invoice)
	}
	if len(page.Invoices) == maxResults {
		page.HasMorePages, page.NextStartPosition = true, startPosition+maxResults
	}
	return page, nil
}
