// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// myCustomerAlias is Google's alias for the administrator's own customer account.
	myCustomerAlias         = "my_customer"
	maxUserQueryLength      = 2048
	maxPageTokenLength      = 4096
	defaultListUsersOrderBy = "email"
)

var (
	customerIDPattern = regexp.MustCompile(`^C[0-9A-Za-z]{1,32}$`)
	domainNamePattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)
	pageTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)
	// listUsersOrderByFields are the sort fields Google's users.list guide documents.
	listUsersOrderByFields = map[string]bool{"email": true, "givenName": true, "familyName": true}
)

// ListUsersInput selects one page of accounts. Leave both Domain and Customer
// blank to list every account in the administrator's own customer account.
type ListUsersInput struct {
	// Domain limits the page to one of the account's domains, such as example.com; it cannot be combined with Customer.
	Domain string `json:"domain,omitempty"`
	// Customer is a customer ID such as C03az79cb, for a reseller administrator; blank uses the administrator's own account.
	Customer string `json:"customer,omitempty"`
	// Query is Google's user search syntax, such as orgUnitPath='/Sales' isSuspended=false; blank lists every account.
	// Google documents that new data can take up to 36 hours to appear in search results.
	Query string `json:"query,omitempty"`
	// OrderBy is email, givenName, or familyName; blank sorts by primary address.
	OrderBy string `json:"orderBy,omitempty"`
	// PageSize is 1 to 500 accounts; zero uses the connection's listUsersPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page; Google keeps it valid for three days.
	PageToken string `json:"pageToken,omitempty"`
}

// ListUsersOutput is one page of accounts.
type ListUsersOutput struct {
	// Users lists the page's accounts in the requested order; it may be empty.
	Users []User `json:"users"`
	// NextPageToken selects the following page; empty means this is the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListUsersOperation implements the listUsers connector Query.
type ListUsersOperation struct{ client *Client }

type directoryUserPage struct {
	Users         []directoryUser `json:"users"`
	NextPageToken string          `json:"nextPageToken"`
}

var listUsersFailureBranches = failureBranches{
	notFound: ListUsersBranchProviderRejected, conflict: ListUsersBranchProviderRejected, rejected: ListUsersBranchProviderRejected,
	invalidResponse: ListUsersBranchInvalidResponse, defect: ListUsersBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke requests one page of accounts and returns it with the next page token.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[ListUsersOutput] {
	const operationID = "listUsers"
	query, err := operation.buildListUsersQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, failure, sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, directoryRequest{method: http.MethodGet, path: "/users", query: query})
	if exchange.outcome != exchangeSucceeded {
		return queryAttemptFromExchange[ListUsersOutput](operation.client, call, exchange, listUsersFailureBranches)
	}
	receipt := operation.client.receipt(call, exchange.response, "")
	output, err := decodeUserPage(exchange.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchInvalidResponse, ListUsersOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListUsersBranchListed, output, nil, receipt)
}

func (operation ListUsersOperation) buildListUsersQuery(input ListUsersInput) (url.Values, error) {
	domain, customer := strings.TrimSpace(input.Domain), strings.TrimSpace(input.Customer)
	query := url.Values{}
	switch {
	case domain != "" && customer != "":
		return nil, errors.New("set domain or customer, not both")
	case domain != "":
		if len(domain) > 253 || !domainNamePattern.MatchString(domain) {
			return nil, errors.New("domain must be a domain name such as example.com")
		}
		query.Set("domain", domain)
	case customer != "":
		if customer != myCustomerAlias && !customerIDPattern.MatchString(customer) {
			return nil, errors.New("customer must be a customer ID such as C03az79cb or my_customer")
		}
		query.Set("customer", customer)
	default:
		query.Set("customer", myCustomerAlias)
	}
	if search := strings.TrimSpace(input.Query); search != "" {
		if len(search) > maxUserQueryLength || strings.ContainsFunc(search, isControlCharacter) {
			return nil, fmt.Errorf("query must be at most %d bytes without control characters", maxUserQueryLength)
		}
		query.Set("query", search)
	}
	orderBy := strings.TrimSpace(input.OrderBy)
	if orderBy == "" {
		orderBy = defaultListUsersOrderBy
	}
	if !listUsersOrderByFields[orderBy] {
		return nil, errors.New("orderBy must be email, givenName, or familyName")
	}
	query.Set("orderBy", orderBy)
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = operation.client.listUsersPageSize
	}
	if pageSize < 1 || pageSize > maxListUsersPageSize {
		return nil, fmt.Errorf("pageSize must be from 1 to %d", maxListUsersPageSize)
	}
	query.Set("maxResults", strconv.Itoa(pageSize))
	if pageToken := strings.TrimSpace(input.PageToken); pageToken != "" {
		if len(pageToken) > maxPageTokenLength || !pageTokenPattern.MatchString(pageToken) {
			return nil, errors.New("pageToken must be the NextPageToken of a previous page")
		}
		query.Set("pageToken", pageToken)
	}
	return query, nil
}

func decodeUserPage(body []byte) (ListUsersOutput, error) {
	var page directoryUserPage
	if err := json.Unmarshal(body, &page); err != nil {
		return ListUsersOutput{}, errors.New("page is not valid JSON")
	}
	output := ListUsersOutput{Users: make([]User, 0, len(page.Users)), NextPageToken: page.NextPageToken}
	for index, resource := range page.Users {
		user, err := convertUser(resource)
		if err != nil {
			return ListUsersOutput{}, fmt.Errorf("account %d: %w", index, err)
		}
		output.Users = append(output.Users, user)
	}
	return output, nil
}

func isControlCharacter(character rune) bool { return character < 0x20 || character == 0x7f }
