// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listInboxesOperation = "listInboxes"

	// MinimumPageSize is the smallest page size Hiver accepts for a list.
	MinimumPageSize = 10
	// MaximumPageSize is the largest page size Hiver accepts for a list.
	MaximumPageSize = 100
	// DefaultPageSize is the page size the connector requests when an input leaves it zero.
	DefaultPageSize = 50
)

// ListInboxesInput selects one page of the account's shared inboxes.
type ListInboxesInput struct {
	// PageToken is NextPageToken from the previous page, or empty for the first page.
	PageToken string `json:"pageToken,omitempty"`
	// PageSize is the number of inboxes per page, from 10 to 100; zero uses DefaultPageSize.
	PageSize int `json:"pageSize,omitempty"`
}

// ListInboxesOutput is one page of shared inboxes.
type ListInboxesOutput struct {
	// Inboxes are the shared inboxes of this page in Hiver's order.
	Inboxes []Inbox `json:"inboxes"`
	// NextPageToken reads the next page, or is empty after the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListInboxesOperation is the listInboxes Query.
type ListInboxesOperation struct {
	client *Client
}

// queryBranches names an operation's branches for the shared Query outcome mapping.
type queryBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// Definition returns the immutable connector operation definition.
func (ListInboxesOperation) Definition() sdkgo.QueryDefinition { return ListInboxesDefinition }

// Invoke sends GET /v1/inboxes with limit and next_page.
func (operation ListInboxesOperation) Invoke(call sdkgo.Call, input ListInboxesInput) sdkgo.QueryAttempt[ListInboxesOutput] {
	if err := validatePageInput(input.PageToken, input.PageSize); err != nil {
		return sdkgo.NewQueryBranch(ListInboxesBranchDefect, ListInboxesOutput{}, hiverFailurePointer(sdkgo.FailureValidation, listInboxesOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, listInboxesOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListInboxesBranchDefect, ListInboxesOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, listInboxesOperation, hiverRequest{
		method: http.MethodGet, path: "/inboxes", query: pageQuery(input.PageToken, input.PageSize),
	})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := queryAttemptForExchange[ListInboxesOutput](result, receipt, queryBranches{
		notFound: ListInboxesBranchProviderRejected, providerRejected: ListInboxesBranchProviderRejected,
		invalidResponse: ListInboxesBranchInvalidResponse, defect: ListInboxesBranchDefect,
	}); isTerminal {
		return attempt
	}
	results, nextPageToken, err := decodeListPage(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListInboxesBranchInvalidResponse, ListInboxesOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, listInboxesOperation, "Hiver returned an invalid inbox page: "+err.Error()), receipt)
	}
	output := ListInboxesOutput{Inboxes: make([]Inbox, 0, len(results)), NextPageToken: nextPageToken}
	for index, raw := range results {
		inbox, err := decodeInbox(raw)
		if err != nil {
			return sdkgo.NewQueryBranch(ListInboxesBranchInvalidResponse, ListInboxesOutput{}, hiverFailurePointer(sdkgo.FailureProtocol, listInboxesOperation,
				fmt.Sprintf("Hiver returned an invalid inbox at index %d: %s", index, err.Error())), receipt)
		}
		output.Inboxes = append(output.Inboxes, inbox)
	}
	return sdkgo.NewQueryBranch(ListInboxesBranchListed, output, nil, receipt)
}

// queryAttemptForExchange returns the terminal attempt for every outcome except success; a read is always safe to retry.
func queryAttemptForExchange[OUT any](result hiverExchange, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(branches.notFound, zero, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, zero, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}

func validatePageInput(pageToken string, pageSize int) error {
	if pageSize != 0 && (pageSize < MinimumPageSize || pageSize > MaximumPageSize) {
		return errors.New("pageSize must be zero or from 10 to 100")
	}
	return validatePageToken("pageToken", pageToken)
}

// pageQuery sends Hiver's documented limit and next_page list parameters.
func pageQuery(pageToken string, pageSize int) url.Values {
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	query := url.Values{"limit": {strconv.Itoa(pageSize)}}
	if pageToken != "" {
		query.Set("next_page", pageToken)
	}
	return query
}
