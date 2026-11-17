// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package x

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchRecentPostsOperation reads one page of matching public Posts.
type SearchRecentPostsOperation struct{ client *Client }

// SearchRecentPosts binds the Query to this client.
func (client *Client) SearchRecentPosts() SearchRecentPostsOperation {
	return SearchRecentPostsOperation{client: client}
}

// Definition returns the manifest-generated Query contract.
func (SearchRecentPostsOperation) Definition() sdkgo.QueryDefinition {
	return SearchRecentPostsDefinition
}

// Invoke searches public Posts from the last seven days.
func (operation SearchRecentPostsOperation) Invoke(call sdkgo.Call, input SearchRecentPostsInput) sdkgo.QueryAttempt[PostPage] {
	if len(strings.TrimSpace(input.Query)) == 0 || len(input.Query) > 4096 || input.MaxResults != 0 && (input.MaxResults < 10 || input.MaxResults > 100) || len(input.NextToken) > 2048 {
		return postPageBranch(SearchRecentPostsBranchDefect, sdkgo.FailureValidation, "search query or pagination is invalid")
	}
	parameters := url.Values{"query": {input.Query}}
	if input.MaxResults > 0 {
		parameters.Set("max_results", strconv.Itoa(input.MaxResults))
	}
	if input.NextToken != "" {
		parameters.Set("next_token", input.NextToken)
	}
	response := operation.client.get(call, "/tweets/search/recent?"+parameters.Encode())
	if errors.Is(response.err, errCredential) {
		return postPageBranch(SearchRecentPostsBranchDefect, sdkgo.FailureAuthentication, "X credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return postPageRetry(sdkgo.FailureTransport, "X search request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return postPageRetry(sdkgo.FailureRateLimit, "X rate limited the search", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return postPageRetry(sdkgo.FailureAvailability, "X search is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return postPageBranch(SearchRecentPostsBranchProviderRejected, sdkgo.FailureProviderRejection, "X rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return postPageBranch(SearchRecentPostsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "X search response exceeded the configured limit")
	}
	if response.err != nil {
		return postPageRetry(sdkgo.FailureTransport, "X search response was interrupted", 0)
	}
	var wire struct {
		Data []PostMetadata `json:"data"`
		Meta *struct {
			ResultCount int    `json:"result_count"`
			NextToken   string `json:"next_token"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Meta == nil || wire.Meta.ResultCount < 0 || wire.Meta.ResultCount != len(wire.Data) {
		return postPageBranch(SearchRecentPostsBranchInvalidResponse, sdkgo.FailureProtocol, "X returned invalid search JSON")
	}
	for _, post := range wire.Data {
		if !validPostID(post.ID) || strings.TrimSpace(post.Text) == "" {
			return postPageBranch(SearchRecentPostsBranchInvalidResponse, sdkgo.FailureProtocol, "X returned an incomplete Post")
		}
	}
	return sdkgo.NewQueryBranch(SearchRecentPostsBranchFound, PostPage{Data: wire.Data, NextToken: wire.Meta.NextToken}, nil, sdkgo.Receipt{})
}

func postPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "x-twitter", Operation: "searchRecentPosts", Message: message}
}
func postPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PostPage] {
	failure := postPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PostPage{}, &failure, sdkgo.Receipt{})
}
func postPageRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PostPage] {
	return sdkgo.NewQueryRetry[PostPage](postPageFailure(kind, message), delay)
}
