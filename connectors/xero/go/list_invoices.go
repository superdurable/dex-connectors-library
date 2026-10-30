// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listInvoicesOperationID = "listInvoices"
	// MaxReferenceCharacters is Xero's invoice Reference limit.
	MaxReferenceCharacters = 255
)

// ListInvoicesInput filters one page of invoices. Every filter is optional, and filters combine.
type ListInvoicesInput struct {
	// Statuses keeps invoices in any of these Xero statuses; empty keeps every status.
	Statuses []InvoiceStatus `json:"statuses,omitempty"`
	// ContactIDs keeps invoices for any of at most 40 contacts, by Xero UUID.
	ContactIDs []string `json:"contactIds,omitempty"`
	// InvoiceNumbers keeps invoices with any of at most 40 document numbers.
	InvoiceNumbers []string `json:"invoiceNumbers,omitempty"`
	// Type keeps only ACCREC sales invoices or ACCPAY bills; blank keeps both.
	Type InvoiceType `json:"type,omitempty"`
	// Reference keeps invoices whose Reference equals this value, through Xero's optimised filter.
	Reference string `json:"reference,omitempty"`
	// ModifiedSince sends If-Modified-Since, so only invoices created or changed at or after this
	// instant, to the second, are returned. Xero does not count every change, such as a due date
	// changed on a partly paid invoice.
	ModifiedSince *time.Time `json:"modifiedSince,omitempty"`
	// Page is the 1-based page to read; zero reads the first page.
	Page int `json:"page,omitempty"`
	// PageSize is 1 to 100 invoices; zero uses DefaultPageSize.
	PageSize int `json:"pageSize,omitempty"`
}

// ListInvoicesOperation implements the listInvoices Query.
type ListInvoicesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke reads one page of GET /Invoices with line items, in Xero's default order.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	client := operation.client
	request, bounds, err := buildListInvoicesRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListInvoicesBranchDefect, InvoicePage{}, xeroFailurePointer(sdkgo.FailureValidation, listInvoicesOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, listInvoicesOperationID, request)
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, InvoicePage{}, receipt, queryBranches{
		providerRejected: ListInvoicesBranchProviderRejected, dailyLimitReached: ListInvoicesBranchDailyLimitReached,
		invalidResponse: ListInvoicesBranchInvalidResponse, defect: ListInvoicesBranchDefect,
	}); isTerminal {
		return attempt
	}
	page, err := decodeInvoicePage(result.response.body, bounds)
	if err != nil {
		return sdkgo.NewQueryBranch(ListInvoicesBranchInvalidResponse, InvoicePage{},
			xeroFailurePointer(sdkgo.FailureProtocol, listInvoicesOperationID, "Xero returned an invalid invoice page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListInvoicesBranchListed, page, nil, receipt)
}

// BuildInvoiceWhereFilter returns the where filter listInvoices sends for the type and reference,
// such as Type=="ACCREC" AND Reference=="ORDER-1042", or "" when neither is set, so an
// application can review it. It does not validate the input.
func BuildInvoiceWhereFilter(input ListInvoicesInput) string {
	var conditions []string
	if input.Type != "" {
		conditions = append(conditions, `Type=="`+string(input.Type)+`"`)
	}
	if reference := strings.TrimSpace(input.Reference); reference != "" {
		conditions = append(conditions, `Reference=="`+reference+`"`)
	}
	return strings.Join(conditions, " AND ")
}

func buildListInvoicesRequest(input ListInvoicesInput) (xeroRequest, pageBounds, error) {
	bounds, err := resolvePageBounds(input.Page, input.PageSize)
	if err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	if err := validateInvoiceStatuses("statuses", input.Statuses); err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	if err := validateIDList("contactIds", input.ContactIDs); err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	if len(input.InvoiceNumbers) > MaxFilterValues {
		return xeroRequest{}, pageBounds{}, fmt.Errorf("invoiceNumbers can list at most %d numbers", MaxFilterValues)
	}
	for _, invoiceNumber := range input.InvoiceNumbers {
		if invoiceNumber == "" || strings.Contains(invoiceNumber, ",") {
			return xeroRequest{}, pageBounds{}, errors.New("invoiceNumbers cannot contain a blank number or a comma")
		}
		if err := validateInvoiceNumber("invoiceNumbers", invoiceNumber); err != nil {
			return xeroRequest{}, pageBounds{}, err
		}
	}
	if input.Type != "" {
		if err := validateInvoiceType(input.Type); err != nil {
			return xeroRequest{}, pageBounds{}, err
		}
	}
	if err := validateWhereLiteral("reference", strings.TrimSpace(input.Reference), MaxReferenceCharacters); err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	query := url.Values{
		"page": {strconv.Itoa(bounds.page)}, "pageSize": {strconv.Itoa(bounds.pageSize)}, "unitdp": {unitAmountDecimalPlaces},
	}
	if len(input.Statuses) != 0 {
		statuses := make([]string, 0, len(input.Statuses))
		for _, status := range input.Statuses {
			statuses = append(statuses, string(status))
		}
		query.Set("Statuses", strings.Join(statuses, ","))
	}
	if len(input.ContactIDs) != 0 {
		query.Set("ContactIDs", strings.Join(input.ContactIDs, ","))
	}
	if len(input.InvoiceNumbers) != 0 {
		query.Set("InvoiceNumbers", strings.Join(input.InvoiceNumbers, ","))
	}
	if where := BuildInvoiceWhereFilter(input); where != "" {
		query.Set("where", where)
	}
	return xeroRequest{method: http.MethodGet, path: "/Invoices", query: query, modifiedSince: input.ModifiedSince}, bounds, nil
}

func decodeInvoicePage(body []byte, bounds pageBounds) (InvoicePage, error) {
	var wire invoicePageWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return InvoicePage{}, errResponseNotJSONObject
	}
	if wire.Invoices == nil {
		return InvoicePage{}, errors.New("response has no Invoices list")
	}
	invoices := make([]Invoice, 0, len(*wire.Invoices))
	for _, element := range *wire.Invoices {
		invoice, err := decodeInvoice(element)
		if err != nil {
			return InvoicePage{}, err
		}
		invoices = append(invoices, invoice)
	}
	page := InvoicePage{
		Invoices: invoices, Page: bounds.page, PageSize: bounds.pageSize,
		HasMorePages: bounds.hasMorePages(wire.Pagination, len(invoices)),
	}
	if wire.Pagination != nil {
		page.PageCount, page.ItemCount = wire.Pagination.PageCount, wire.Pagination.ItemCount
	}
	return page, nil
}

func validateInvoiceStatuses(fieldName string, statuses []InvoiceStatus) error {
	for _, status := range statuses {
		switch status {
		case InvoiceStatusDraft, InvoiceStatusSubmitted, InvoiceStatusDeleted, InvoiceStatusAuthorised, InvoiceStatusPaid, InvoiceStatusVoided:
		default:
			return fmt.Errorf("%s must contain Xero statuses: DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED", fieldName)
		}
	}
	return nil
}

func validateInvoiceType(invoiceType InvoiceType) error {
	switch invoiceType {
	case InvoiceTypeAccountsReceivable, InvoiceTypeAccountsPayable:
		return nil
	default:
		return errors.New("type must be ACCREC for a sales invoice or ACCPAY for a bill")
	}
}
