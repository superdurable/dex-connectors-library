// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bocha

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchWebInput supplies a public web search query.
type SearchWebInput struct {
	// Query is a nonblank search phrase of at most 256 Unicode characters.
	Query string `json:"query"`
}

// WebResult contains a title, HTTPS URL, and short search excerpt.
type WebResult struct {
	// Title is the provider's page title.
	Title string `json:"title"`
	// URL is the page's public HTTPS URL.
	URL string `json:"url"`
	// Snippet is the provider's short search excerpt.
	Snippet string `json:"snippet"`
}

// SearchWebOutput contains up to five public web results.
type SearchWebOutput struct {
	// Results are the public pages returned in provider ranking order.
	Results []WebResult `json:"results"`
}

// SearchWebOperation performs a read-only, potentially billed public web search.
type SearchWebOperation struct{ client *Client }

// Definition returns the generated search contract.
func (SearchWebOperation) Definition() sdkgo.QueryDefinition { return SearchWebDefinition }

// Invoke searches the public web while filtering provider metadata and errors.
func (operation SearchWebOperation) Invoke(call sdkgo.Call, input SearchWebInput) sdkgo.QueryAttempt[SearchWebOutput] {
	if !utf8.ValidString(input.Query) || strings.TrimSpace(input.Query) == "" || utf8.RuneCountInString(input.Query) > 256 {
		return searchBranch(SearchWebBranchDefect, sdkgo.FailureValidation, "Bocha query must contain 1 to 256 Unicode characters")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return searchBranch(SearchWebBranchDefect, sdkgo.FailureAuthentication, "Bocha API Key is unavailable or invalid")
	}
	payload, err := json.Marshal(struct {
		Query   string `json:"query"`
		Count   int    `json:"count"`
		Summary bool   `json:"summary"`
	}{Query: input.Query, Count: 5, Summary: false})
	if err != nil {
		return searchBranch(SearchWebBranchDefect, sdkgo.FailureValidation, "Bocha query could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, webSearchEndpoint, bytes.NewReader(payload))
	if err != nil {
		return searchBranch(SearchWebBranchDefect, sdkgo.FailureValidation, "Bocha search request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "Bocha search request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return searchRetry(sdkgo.FailureRateLimit, "Bocha rate limited the search", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return searchRetry(sdkgo.FailureAvailability, "Bocha search service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifySearchHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return searchBranch(SearchWebBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Bocha response exceeded the configured limit")
	}
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "Bocha response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			WebPages struct {
				Value []struct {
					Name    string `json:"name"`
					URL     string `json:"url"`
					Snippet string `json:"snippet"`
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return searchBranch(SearchWebBranchInvalidResponse, sdkgo.FailureProtocol, "Bocha returned invalid search data")
	}
	if *wire.Code != 200 {
		return searchBranch(SearchWebBranchProviderRejected, sdkgo.FailureProviderRejection, "Bocha rejected search with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data.WebPages.Value == nil || len(wire.Data.WebPages.Value) > 50 {
		return searchBranch(SearchWebBranchInvalidResponse, sdkgo.FailureProtocol, "Bocha returned invalid search results")
	}
	results := make([]WebResult, 0, min(len(wire.Data.WebPages.Value), 5))
	for _, page := range wire.Data.WebPages.Value {
		if len(results) == 5 {
			break
		}
		parsedURL, err := url.Parse(page.URL)
		if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil || page.Name == "" || len(page.Name) > 500 || len(page.Snippet) > 2000 {
			return searchBranch(SearchWebBranchInvalidResponse, sdkgo.FailureProtocol, "Bocha returned invalid search result")
		}
		results = append(results, WebResult{Title: page.Name, URL: page.URL, Snippet: page.Snippet})
	}
	return sdkgo.NewQueryBranch(SearchWebBranchFound, SearchWebOutput{Results: results}, nil, sdkgo.Receipt{})
}
func classifySearchHTTPStatus(status int) sdkgo.QueryAttempt[SearchWebOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return searchBranch(SearchWebBranchProviderRejected, kind, "Bocha rejected search with HTTP "+strconv.Itoa(status))
}
func searchBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SearchWebOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "bocha", Operation: "searchWeb", Message: message}
	return sdkgo.NewQueryBranch(branch, SearchWebOutput{}, &failure, sdkgo.Receipt{})
}
func searchRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SearchWebOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "bocha", Operation: "searchWeb", Message: message}
	return sdkgo.NewQueryRetry[SearchWebOutput](failure, delay)
}
