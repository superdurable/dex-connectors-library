// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const searchTransactionsGraphQL = "query SearchTransactions($input: TransactionSearchInput!, $first: Int!, $after: String) { transactions(input: $input, first: $first, after: $after) { edges { node { id status } } pageInfo { hasNextPage endCursor } } }"

// SearchTransactionsInput filters by exact GraphQL transaction or order ID.
type SearchTransactionsInput struct {
	// TransactionID is an optional exact GraphQL ID.
	TransactionID string `json:"transactionId,omitempty"`
	// OrderID is an optional exact merchant order ID.
	OrderID string `json:"orderId,omitempty"`
	// First is the page size from 1 through 100; zero uses 50.
	First int `json:"first,omitempty"`
	// After is the previous page's end cursor; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// TransactionsPage contains one Braintree search result page.
type TransactionsPage struct {
	// Transactions contains the matching transaction profiles.
	Transactions []TransactionProfile `json:"transactions"`
	// HasNextPage indicates another provider page.
	HasNextPage bool `json:"hasNextPage"`
	// EndCursor is passed as After for the next page.
	EndCursor string `json:"endCursor,omitempty"`
}

// SearchTransactionsOperation reads one transaction search page.
type SearchTransactionsOperation struct{ client *Client }

// SearchTransactions binds the search Query.
func (client *Client) SearchTransactions() SearchTransactionsOperation {
	return SearchTransactionsOperation{client}
}

// Definition returns the generated Query contract.
func (SearchTransactionsOperation) Definition() sdkgo.QueryDefinition {
	return SearchTransactionsDefinition
}

// Invoke searches by exact ID or order ID.
func (operation SearchTransactionsOperation) Invoke(call sdkgo.Call, input SearchTransactionsInput) sdkgo.QueryAttempt[TransactionsPage] {
	if (input.TransactionID == "" && input.OrderID == "") || input.TransactionID != "" && !validGraphQLID(input.TransactionID) || !validOrderID(input.OrderID) || input.First < 0 || input.First > 100 || len(input.After) > 500 {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchDefect, sdkgo.FailureValidation, "transaction search input is invalid")
	}
	first := input.First
	if first == 0 {
		first = 50
	}
	filter := map[string]any{}
	if input.TransactionID != "" {
		filter["id"] = map[string]any{"is": input.TransactionID}
	}
	if input.OrderID != "" {
		filter["orderId"] = map[string]any{"is": input.OrderID}
	}
	variables := map[string]any{"input": filter, "first": first}
	if input.After != "" {
		variables["after"] = input.After
	}
	response := operation.client.graphQL(call, call.Context, searchTransactionsGraphQL, variables)
	if errors.Is(response.err, errCredential) {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchDefect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return nodeRetry[TransactionsPage]("searchTransactions", sdkgo.FailureTransport, "Braintree search request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return nodeRetry[TransactionsPage]("searchTransactions", sdkgo.FailureRateLimit, "Braintree rate limited the search", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return nodeRetry[TransactionsPage]("searchTransactions", sdkgo.FailureAvailability, "Braintree search is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Braintree search response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Transactions *struct {
				Edges []struct {
					Node TransactionProfile `json:"node"`
				} `json:"edges"`
				PageInfo *struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"transactions"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree returned invalid search JSON")
	}
	if len(wire.Errors) > 0 {
		switch classifyGraphQLErrors(wire.Errors) {
		case graphQLErrorRateLimited:
			return nodeRetry[TransactionsPage]("searchTransactions", sdkgo.FailureRateLimit, "Braintree reported a resource limit", response.retryAfter)
		case graphQLErrorUnavailable:
			return nodeRetry[TransactionsPage]("searchTransactions", sdkgo.FailureAvailability, "Braintree reported a temporary failure", response.retryAfter)
		case graphQLErrorNotFound:
			return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchNotFound, sdkgo.FailureNotFound, "Braintree did not find the search resource")
		default:
			return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected the search")
		}
	}
	if wire.Data == nil || wire.Data.Transactions == nil || wire.Data.Transactions.PageInfo == nil {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree search page is missing")
	}
	page := TransactionsPage{Transactions: make([]TransactionProfile, 0, len(wire.Data.Transactions.Edges)), HasNextPage: wire.Data.Transactions.PageInfo.HasNextPage, EndCursor: wire.Data.Transactions.PageInfo.EndCursor}
	for _, edge := range wire.Data.Transactions.Edges {
		if !validGraphQLID(edge.Node.ID) || edge.Node.Status == "" {
			return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree search result is invalid")
		}
		page.Transactions = append(page.Transactions, edge.Node)
	}
	if page.HasNextPage && page.EndCursor == "" {
		return nodeBranch[TransactionsPage]("searchTransactions", SearchTransactionsBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree search cursor is missing")
	}
	return sdkgo.NewQueryBranch(SearchTransactionsBranchSearched, page, nil, sdkgo.Receipt{})
}
