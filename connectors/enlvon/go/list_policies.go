// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package enlvon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchPoliciesInput contains a natural-language electricity policy query and bounded result count.
type SearchPoliciesInput struct {
	// Query is a nonempty natural-language electricity policy search.
	Query string `json:"query"`
	// TopK is the number of cards from 1 to 50; zero uses the provider's documented default of 10.
	TopK int `json:"topK,omitempty"`
}

// PolicyCard contains source metadata without generated analysis or full document text.
type PolicyCard struct {
	// Title is the policy title.
	Title string `json:"title"`
	// Date is the provider's publication date string.
	Date string `json:"date"`
	// Source is the provider's source identifier.
	Source string `json:"source"`
	// OriginalURL is the provider's link to the original government document.
	OriginalURL string `json:"originalURL"`
}

// SearchPoliciesOutput contains policy cards that match the query.
type SearchPoliciesOutput struct {
	// Policies are the returned cards, possibly empty.
	Policies []PolicyCard `json:"policies"`
}

// SearchPoliciesOperation performs an authenticated, read-only Enlvon search.
type SearchPoliciesOperation struct{ client *Client }

// Definition returns the generated policy-search Query contract.
func (SearchPoliciesOperation) Definition() sdkgo.QueryDefinition { return SearchPoliciesDefinition }

// Invoke searches Enlvon and discards full text, analysis, and relevance scores.
func (operation SearchPoliciesOperation) Invoke(call sdkgo.Call, input SearchPoliciesInput) sdkgo.QueryAttempt[SearchPoliciesOutput] {
	if input.Query == "" || len(input.Query) > 1024 || !utf8.ValidString(input.Query) || input.TopK < 0 || input.TopK > 50 {
		return policyBranch(SearchPoliciesBranchDefect, sdkgo.FailureValidation, "Enlvon search query or topK is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return policyBranch(SearchPoliciesBranchDefect, sdkgo.FailureAuthentication, "Enlvon API key is unavailable or invalid")
	}
	values := url.Values{}
	values.Set("q", input.Query)
	if input.TopK > 0 {
		values.Set("top_k", strconv.Itoa(input.TopK))
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/search?"+values.Encode(), nil)
	if err != nil {
		return policyBranch(SearchPoliciesBranchDefect, sdkgo.FailureValidation, "Enlvon search request is invalid")
	}
	request.Header.Set("X-API-Key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return policyRetry(sdkgo.FailureAvailability, "Enlvon search transport failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		return policyRetry(sdkgo.FailureRateLimit, "Enlvon search quota or rate limit reached", retryAfter)
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		return policyRetry(sdkgo.FailureAvailability, "Enlvon search temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return policyBranch(SearchPoliciesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Enlvon search response exceeded the configured limit")
	}
	if err != nil {
		return policyRetry(sdkgo.FailureAvailability, "Enlvon search response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		return policyBranch(SearchPoliciesBranchProviderRejected, kind, "Enlvon rejected search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Count   *int `json:"count"`
		Results []struct {
			Title       string `json:"标题"`
			Date        string `json:"日期"`
			Source      string `json:"来源"`
			OriginalURL string `json:"原文链接"`
		} `json:"results"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&wire) != nil || wire.Count == nil || wire.Results == nil {
		return policyBranch(SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, "Enlvon returned invalid search JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return policyBranch(SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, "Enlvon returned trailing search JSON")
	}
	if *wire.Count != len(wire.Results) || len(wire.Results) > 50 {
		return policyBranch(SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, "Enlvon returned an inconsistent result count")
	}
	policies := make([]PolicyCard, 0, len(wire.Results))
	for _, item := range wire.Results {
		if item.Title == "" || len(item.Title) > 2048 || len(item.Date) > 128 || len(item.Source) > 128 || len(item.OriginalURL) > 4096 {
			return policyBranch(SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, "Enlvon returned invalid policy metadata")
		}
		policies = append(policies, PolicyCard{Title: item.Title, Date: item.Date, Source: item.Source, OriginalURL: item.OriginalURL})
	}
	return sdkgo.NewQueryBranch(SearchPoliciesBranchFound, SearchPoliciesOutput{Policies: policies}, nil, sdkgo.Receipt{})
}
func isValidAPIKey(value string) bool {
	if !strings.HasPrefix(value, "enl_") || len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func policyBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SearchPoliciesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "enlvon", Operation: "searchPolicies", Message: message}
	return sdkgo.NewQueryBranch(branch, SearchPoliciesOutput{}, &failure, sdkgo.Receipt{})
}
func policyRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SearchPoliciesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "enlvon", Operation: "searchPolicies", Message: message}
	return sdkgo.NewQueryRetry[SearchPoliciesOutput](failure, delay)
}
