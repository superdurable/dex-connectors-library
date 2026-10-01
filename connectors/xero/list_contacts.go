// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listContactsOperationID = "listContacts"
	// MaxSearchTermCharacters bounds a listContacts search term.
	MaxSearchTermCharacters = 100
)

// ListContactsInput filters one page of contacts. Every filter is optional, and filters combine.
type ListContactsInput struct {
	// SearchTerm is a case-insensitive text search across Name, FirstName, LastName,
	// ContactNumber, CompanyNumber, and EmailAddress; blank lists every contact.
	SearchTerm string `json:"searchTerm,omitempty"`
	// ContactIDs limits the page to at most 40 contacts by their Xero UUIDs.
	ContactIDs []string `json:"contactIds,omitempty"`
	// IncludesArchived also returns ARCHIVED contacts, which Xero leaves out by default.
	IncludesArchived bool `json:"includesArchived,omitempty"`
	// ModifiedSince sends If-Modified-Since, so only contacts created or changed at or after this
	// instant, to the second, are returned. Xero does not count balance or IsCustomer changes.
	ModifiedSince *time.Time `json:"modifiedSince,omitempty"`
	// Page is the 1-based page to read; zero reads the first page.
	Page int `json:"page,omitempty"`
	// PageSize is 1 to 100 contacts; zero uses DefaultPageSize.
	PageSize int `json:"pageSize,omitempty"`
}

// ListContactsOperation implements the listContacts Query.
type ListContactsOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// Invoke reads one page of GET /Contacts in Xero's default order, last modification then ContactID.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactPage] {
	client := operation.client
	request, bounds, err := buildListContactsRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListContactsBranchDefect, ContactPage{}, xeroFailurePointer(sdkgo.FailureValidation, listContactsOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, listContactsOperationID, request)
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, ContactPage{}, receipt, queryBranches{
		providerRejected: ListContactsBranchProviderRejected, dailyLimitReached: ListContactsBranchDailyLimitReached,
		invalidResponse: ListContactsBranchInvalidResponse, defect: ListContactsBranchDefect,
	}); isTerminal {
		return attempt
	}
	page, err := decodeContactPage(result.response.body, bounds)
	if err != nil {
		return sdkgo.NewQueryBranch(ListContactsBranchInvalidResponse, ContactPage{},
			xeroFailurePointer(sdkgo.FailureProtocol, listContactsOperationID, "Xero returned an invalid contact page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListContactsBranchListed, page, nil, receipt)
}

func buildListContactsRequest(input ListContactsInput) (xeroRequest, pageBounds, error) {
	bounds, err := resolvePageBounds(input.Page, input.PageSize)
	if err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	searchTerm := strings.TrimSpace(input.SearchTerm)
	if err := validateSingleLineText("searchTerm", searchTerm, MaxSearchTermCharacters); err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	if err := validateIDList("contactIds", input.ContactIDs); err != nil {
		return xeroRequest{}, pageBounds{}, err
	}
	query := url.Values{"page": {strconv.Itoa(bounds.page)}, "pageSize": {strconv.Itoa(bounds.pageSize)}}
	if searchTerm != "" {
		query.Set("searchTerm", searchTerm)
	}
	if len(input.ContactIDs) != 0 {
		query.Set("IDs", strings.Join(input.ContactIDs, ","))
	}
	if input.IncludesArchived {
		query.Set("includeArchived", "true")
	}
	return xeroRequest{method: http.MethodGet, path: "/Contacts", query: query, modifiedSince: input.ModifiedSince}, bounds, nil
}

func decodeContactPage(body []byte, bounds pageBounds) (ContactPage, error) {
	var wire contactPageWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactPage{}, errResponseNotJSONObject
	}
	if wire.Contacts == nil {
		return ContactPage{}, errors.New("response has no Contacts list")
	}
	contacts, err := decodeContacts(*wire.Contacts)
	if err != nil {
		return ContactPage{}, err
	}
	page := ContactPage{
		Contacts: contacts, Page: bounds.page, PageSize: bounds.pageSize,
		HasMorePages: bounds.hasMorePages(wire.Pagination, len(contacts)),
	}
	if wire.Pagination != nil {
		page.PageCount, page.ItemCount = wire.Pagination.PageCount, wire.Pagination.ItemCount
	}
	return page, nil
}
