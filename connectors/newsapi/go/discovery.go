// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newsapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// HeadlineCategories lists NewsAPI's supported category filters.
var HeadlineCategories = []string{"business", "entertainment", "general", "health", "science", "sports", "technology"}

// GetTopHeadlinesInput filters one page of breaking headlines.
type GetTopHeadlinesInput struct {
	// Country is a two-letter ISO 3166-1 code; it cannot be combined with Sources.
	Country string `json:"country,omitempty"`
	// Category is one of HeadlineCategories; it cannot be combined with Sources.
	Category string `json:"category,omitempty"`
	// Sources lists provider source IDs; it cannot be combined with Country or Category.
	Sources []string `json:"sources,omitempty"`
	// Query is a keyword or phrase; blank uses the selected country, category, or sources.
	Query string `json:"query,omitempty"`
	// PageSize is 1 to 100; zero uses NewsAPI's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Page is 1-based; zero selects the first page.
	Page int `json:"page,omitempty"`
}

// GetTopHeadlinesOperation reads one page of top headlines.
type GetTopHeadlinesOperation struct{ client *Client }

// GetTopHeadlines binds the headline query to this client.
func (client *Client) GetTopHeadlines() GetTopHeadlinesOperation {
	return GetTopHeadlinesOperation{client}
}

// Definition returns the generated headline query contract.
func (GetTopHeadlinesOperation) Definition() sdkgo.QueryDefinition { return GetTopHeadlinesDefinition }

// Invoke validates the headline filters and calls NewsAPI.
func (operation GetTopHeadlinesOperation) Invoke(call sdkgo.Call, input GetTopHeadlinesInput) sdkgo.QueryAttempt[ArticlePage] {
	query, err := encodeHeadlineQuery(input)
	if err != nil {
		return discoveryBranch[ArticlePage](GetTopHeadlinesBranchDefect, "getTopHeadlines", sdkgo.FailureValidation, err.Error())
	}
	return runDiscoveryQuery(operation.client, call, "getTopHeadlines", "/top-headlines", query,
		GetTopHeadlinesBranchFound, GetTopHeadlinesBranchProviderRejected, GetTopHeadlinesBranchInvalidResponse, GetTopHeadlinesBranchDefect,
		func(body []byte) (ArticlePage, error) { return parseHeadlinePage(body, input) })
}

func encodeHeadlineQuery(input GetTopHeadlinesInput) (url.Values, error) {
	query := url.Values{}
	if input.Country != "" {
		if len(input.Country) != 2 || strings.ToLower(input.Country) != input.Country {
			return nil, fmt.Errorf("country must be a lowercase two-letter code")
		}
		query.Set("country", input.Country)
	}
	if input.Category != "" {
		if !slices.Contains(HeadlineCategories, input.Category) {
			return nil, fmt.Errorf("category must be a NewsAPI headline category")
		}
		query.Set("category", input.Category)
	}
	if len(input.Sources) > 0 {
		if input.Country != "" || input.Category != "" || len(input.Sources) > maxSources {
			return nil, fmt.Errorf("sources cannot accompany country or category and accepts at most 20 IDs")
		}
		for _, source := range input.Sources {
			if !sourceIDPattern.MatchString(source) {
				return nil, fmt.Errorf("sources contains an invalid ID")
			}
		}
		query.Set("sources", strings.Join(input.Sources, ","))
	}
	if len(input.Query) > maxQueryCharacters {
		return nil, fmt.Errorf("query exceeds 500 characters")
	}
	if input.Query != "" {
		query.Set("q", input.Query)
	}
	if input.Country == "" && input.Category == "" && len(input.Sources) == 0 && strings.TrimSpace(input.Query) == "" {
		return nil, fmt.Errorf("country, category, sources, or query is required")
	}
	if input.PageSize < 0 || input.PageSize > maxPageSize || input.Page < 0 {
		return nil, fmt.Errorf("pageSize must be 1 to 100 and page must be positive")
	}
	if input.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	return query, nil
}

func parseHeadlinePage(body []byte, input GetTopHeadlinesInput) (ArticlePage, error) {
	var wirePage wireArticlePage
	if err := json.Unmarshal(body, &wirePage); err != nil {
		return ArticlePage{}, err
	}
	if wirePage.Status != "ok" || wirePage.TotalResults == nil || *wirePage.TotalResults < 0 || wirePage.Articles == nil {
		return ArticlePage{}, fmt.Errorf("incomplete headline page")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 20
	}
	if len(wirePage.Articles) > pageSize {
		return ArticlePage{}, fmt.Errorf("headline page exceeded requested size")
	}
	for index := range wirePage.Articles {
		wirePage.Articles[index].PublishedAt = wirePage.Articles[index].PublishedAt.UTC()
	}
	page := max(input.Page, 1)
	return ArticlePage{TotalResults: *wirePage.TotalResults, Articles: wirePage.Articles, Page: page, HasMore: page*pageSize < *wirePage.TotalResults}, nil
}

// ListSourcesInput filters NewsAPI publishers; blank fields return all sources.
type ListSourcesInput struct {
	// Category is one of HeadlineCategories; blank includes every category.
	Category string `json:"category,omitempty"`
	// Language is a two-letter code from Languages; blank includes every language.
	Language string `json:"language,omitempty"`
	// Country is a lowercase two-letter ISO 3166-1 code; blank includes every country.
	Country string `json:"country,omitempty"`
}

// NewsSource is one NewsAPI publisher.
type NewsSource struct {
	// ID is the source ID accepted by headline and article queries.
	ID string `json:"id"`
	// Name is the publisher's display name.
	Name string `json:"name"`
	// Description is the publisher's summary.
	Description string `json:"description"`
	// URL is the publisher's home page.
	URL string `json:"url"`
	// Category describes the publisher's news topic.
	Category string `json:"category"`
	// Language is its publication language code.
	Language string `json:"language"`
	// Country is its country code.
	Country string `json:"country"`
}

// SourcesPage contains the available NewsAPI publishers.
type SourcesPage struct {
	// Sources contains the matching publishers, possibly empty.
	Sources []NewsSource `json:"sources"`
}

// ListSourcesOperation reads available publishers.
type ListSourcesOperation struct{ client *Client }

// ListSources binds the source query to this client.
func (client *Client) ListSources() ListSourcesOperation { return ListSourcesOperation{client} }

// Definition returns the generated source query contract.
func (ListSourcesOperation) Definition() sdkgo.QueryDefinition { return ListSourcesDefinition }

// Invoke validates filters and calls NewsAPI.
func (operation ListSourcesOperation) Invoke(call sdkgo.Call, input ListSourcesInput) sdkgo.QueryAttempt[SourcesPage] {
	query := url.Values{}
	if input.Category != "" {
		if !slices.Contains(HeadlineCategories, input.Category) {
			return discoveryBranch[SourcesPage](ListSourcesBranchDefect, "listSources", sdkgo.FailureValidation, "invalid category")
		}
		query.Set("category", input.Category)
	}
	if input.Language != "" {
		if !slices.Contains(Languages, input.Language) {
			return discoveryBranch[SourcesPage](ListSourcesBranchDefect, "listSources", sdkgo.FailureValidation, "invalid language")
		}
		query.Set("language", input.Language)
	}
	if input.Country != "" {
		if len(input.Country) != 2 || strings.ToLower(input.Country) != input.Country {
			return discoveryBranch[SourcesPage](ListSourcesBranchDefect, "listSources", sdkgo.FailureValidation, "invalid country")
		}
		query.Set("country", input.Country)
	}
	return runDiscoveryQuery(operation.client, call, "listSources", "/top-headlines/sources", query,
		ListSourcesBranchListed, ListSourcesBranchProviderRejected, ListSourcesBranchInvalidResponse, ListSourcesBranchDefect, parseSourcesPage)
}

func parseSourcesPage(body []byte) (SourcesPage, error) {
	var wirePage struct {
		Status  string       `json:"status"`
		Sources []NewsSource `json:"sources"`
	}
	if err := json.Unmarshal(body, &wirePage); err != nil {
		return SourcesPage{}, err
	}
	if wirePage.Status != "ok" || wirePage.Sources == nil {
		return SourcesPage{}, fmt.Errorf("incomplete sources page")
	}
	return SourcesPage{Sources: wirePage.Sources}, nil
}

func runDiscoveryQuery[T any](client *Client, call sdkgo.Call, operation, path string, query url.Values,
	happy, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	request, localFailure := client.newProviderRequest(call, path, query)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewQueryBranch(defect, *new(T), localFailure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: providerName, Operation: operation, Message: "NewsAPI request failed"}, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return classifyDiscoveryError[T](response, operation, rejected)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return discoveryBranch[T](invalid, operation, sdkgo.FailureResponseTooLarge, "NewsAPI response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: providerName, Operation: operation, Message: "NewsAPI response was interrupted"}, 0)
	}
	value, err := decode(body)
	if err != nil {
		return discoveryBranch[T](invalid, operation, sdkgo.FailureProtocol, "NewsAPI response was malformed")
	}
	return sdkgo.NewQueryBranch(happy, value, nil, sdkgo.Receipt{})
}

func classifyDiscoveryError[T any](response *http.Response, operation string, rejected sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	body, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
	code := ""
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/code"}); len(tokens) == 1 {
		code = tokens[0]
	}
	switch {
	case code == "apiKeyExhausted":
		return discoveryBranch[T](rejected, operation, sdkgo.FailureQuotaExhausted, "NewsAPI key allowance is exhausted")
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Provider: providerName, Operation: operation, Message: "NewsAPI rate limited the request"}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError:
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: providerName, Operation: operation, Message: "NewsAPI is temporarily unavailable"}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusUnauthorized || slices.Contains(authenticationErrorCodes, code):
		return discoveryBranch[T](rejected, operation, sdkgo.FailureAuthentication, "NewsAPI rejected the API key")
	case response.StatusCode == http.StatusUpgradeRequired:
		return discoveryBranch[T](rejected, operation, sdkgo.FailureAuthorization, "NewsAPI plan does not permit this request")
	case slices.Contains(parameterErrorCodes, code):
		return discoveryBranch[T](rejected, operation, sdkgo.FailureValidation, "NewsAPI rejected the request parameters")
	default:
		return discoveryBranch[T](rejected, operation, sdkgo.FailureProviderRejection, "NewsAPI rejected the request")
	}
}

func discoveryBranch[T any](branch sdkgo.BranchID, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, *new(T), &sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}, sdkgo.Receipt{})
}
