// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package newsapi searches NewsAPI articles through a typed Dex Query Step.
//
// A Flow opens the connection with NewProjectConnection and adds the generated
// NewSearchArticlesStep factory:
//
//	connection, err := newsapi.NewProjectConnection(project, "newsapi")
//	if err != nil {
//		return err
//	}
//	step := newsapi.NewSearchArticlesStep(newsapi.SearchArticlesStepConfig[CompanyNewsRequest]{
//		StepType: "SearchCompanyArticles", ConnectionName: "newsapi", Connection: connection,
//		MapToOperationInput: func(request CompanyNewsRequest) newsapi.SearchArticlesInput {
//			return newsapi.SearchArticlesInput{Query: request.Company, SearchIn: []string{"title"}}
//		},
//		Searched: sdkgo.GoTo(ArticlesFound{}),
//	})
//
// The runnable examples/company-news Flow shows the complete registration.
package newsapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	searchArticlesOperationID = "searchArticles"
	providerName              = "newsapi"
	// maxQueryCharacters, maxSources, and maxPageSize are NewsAPI's documented limits.
	maxQueryCharacters = 500
	maxSources         = 20
	maxPageSize        = 100
)

var (
	// SearchInFields lists the article fields SearchArticlesInput.SearchIn accepts.
	SearchInFields = []string{"title", "description", "content"}
	// Languages lists the two-letter codes NewsAPI accepts for SearchArticlesInput.Language.
	Languages = []string{"ar", "de", "en", "es", "fr", "he", "it", "nl", "no", "pt", "ru", "sv", "ud", "zh"}
	// SortOrders lists the values SearchArticlesInput.SortBy accepts.
	SortOrders = []string{"publishedAt", "relevancy", "popularity"}

	sourceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
	domainPattern   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	// timeLayouts are the ISO 8601 forms NewsAPI documents for from and to; a time without an offset is UTC.
	timeLayouts = []string{"2006-01-02", "2006-01-02T15:04:05", time.RFC3339Nano}

	authenticationErrorCodes = []string{"apiKeyDisabled", "apiKeyInvalid", "apiKeyMissing"}
	parameterErrorCodes      = []string{"parameterInvalid", "parametersMissing", "sourcesTooMany", "sourceDoesNotExist"}
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport. New copies the client, preserves a nonzero timeout, and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and the credential source. It is safe for concurrent calls.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SearchArticlesInput selects one page of NewsAPI's everything search.
// Query, Sources, or Domains must be set. Empty fields are not sent, so NewsAPI applies its own defaults.
type SearchArticlesInput struct {
	// Query is NewsAPI's keyword expression, up to 500 characters. It supports quoted phrases,
	// + and - prefixes, AND, OR, NOT, and parentheses. The connector form-encodes it.
	Query string `json:"query,omitempty"`
	// SearchIn limits Query matching to title, description, or content. Empty searches all three.
	SearchIn []string `json:"searchIn,omitempty"`
	// Sources lists up to 20 NewsAPI source IDs, such as bbc-news.
	Sources []string `json:"sources,omitempty"`
	// Domains restricts results to these domains, such as techcrunch.com.
	Domains []string `json:"domains,omitempty"`
	// ExcludeDomains removes results from these domains.
	ExcludeDomains []string `json:"excludeDomains,omitempty"`
	// From is the oldest publication time, as a date such as 2026-01-05, a UTC time such as
	// 2026-01-05T07:00:00, or an RFC 3339 time. Empty uses the oldest article the plan allows.
	From string `json:"from,omitempty"`
	// To is the newest publication time in the same forms. Empty means now.
	To string `json:"to,omitempty"`
	// Language is one of Languages. Empty returns articles in every language.
	Language string `json:"language,omitempty"`
	// SortBy is publishedAt (newest first), relevancy, or popularity. Empty uses NewsAPI's default, publishedAt.
	SortBy string `json:"sortBy,omitempty"`
	// PageSize is the number of articles per page, from 1 through 100. Zero uses NewsAPI's default of 100.
	PageSize int `json:"pageSize,omitempty"`
	// Page is the 1-based page number. Zero selects page 1.
	Page int `json:"page,omitempty"`
}

// ArticlePage is one page of matching articles in NewsAPI's order.
type ArticlePage struct {
	// TotalResults is NewsAPI's count of all matching articles. The Developer plan returns only the first 100.
	TotalResults int `json:"totalResults"`
	// Articles holds this page, possibly empty.
	Articles []Article `json:"articles"`
	// Page is the 1-based page this result holds.
	Page int `json:"page"`
	// HasMore reports that TotalResults exceeds the articles up to and including this page.
	HasMore bool `json:"hasMore"`
}

// Article preserves NewsAPI's article fields. A pointer field is nil when NewsAPI sends null, so an
// application can tell a missing value from an empty one. NewsAPI keeps a removed article in results
// with the title [Removed]; the connector returns it unchanged.
// Title, Description, and Content are untrusted provider text: escape them before rendering HTML.
type Article struct {
	// Source identifies the publisher.
	Source ArticleSource `json:"source"`
	// Author is the byline, or nil when NewsAPI has none.
	Author *string `json:"author"`
	// Title is the headline.
	Title string `json:"title"`
	// Description is the article summary, or nil when NewsAPI has none.
	Description *string `json:"description"`
	// URL links to the article on the publisher's site.
	URL string `json:"url"`
	// URLToImage links to the article's lead image, or is nil.
	URLToImage *string `json:"urlToImage"`
	// PublishedAt is the publication time in UTC.
	PublishedAt time.Time `json:"publishedAt"`
	// Content is NewsAPI's excerpt of the body, truncated by NewsAPI to about 200 characters, or nil.
	Content *string `json:"content"`
}

// ArticleSource names an article's publisher.
type ArticleSource struct {
	// ID is NewsAPI's source ID, such as bbc-news, or nil for a publisher without one.
	ID *string `json:"id"`
	// Name is the publisher's display name.
	Name string `json:"name"`
}

// SearchArticlesOperation runs one everything search with one HTTP request.
type SearchArticlesOperation struct{ client *Client }

type wireArticlePage struct {
	Status       string    `json:"status"`
	TotalResults *int      `json:"totalResults"`
	Articles     []Article `json:"articles"`
}

// New validates config and returns a client without accessing the network. Zero fields use manifest
// defaults. Credentials are resolved during each call, so a replaced key applies without a restart.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("NewsAPI credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("NewsAPI base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SearchArticles returns the search query bound to this client.
func (client *Client) SearchArticles() SearchArticlesOperation { return SearchArticlesOperation{client} }

// Definition returns the manifest-generated search query contract.
func (SearchArticlesOperation) Definition() sdkgo.QueryDefinition { return SearchArticlesDefinition }

// Invoke validates input, sends one search request, and classifies the response. Invalid input or
// credentials select defect before any request. Provider message text never enters a Failure.
func (operation SearchArticlesOperation) Invoke(call sdkgo.Call, input SearchArticlesInput) sdkgo.QueryAttempt[ArticlePage] {
	query, err := encodeSearchQuery(input)
	if err != nil {
		return failedBranch(SearchArticlesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	request, failure := operation.client.newSearchRequest(call, query)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchArticlesBranchDefect, ArticlePage{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[ArticlePage](newFailure(sdkgo.FailureAvailability, "NewsAPI request failed"), 0)
	}
	// Closing a read-only response cannot change the query outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return classifyErrorResponse(response)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch(SearchArticlesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "NewsAPI response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ArticlePage](newFailure(sdkgo.FailureAvailability, "NewsAPI response was interrupted"), 0)
	}
	return decodeArticlePage(body, input)
}

// encodeSearchQuery validates input against NewsAPI's documented limits and returns the query string.
func encodeSearchQuery(input SearchArticlesInput) (url.Values, error) {
	query := url.Values{}
	if utf8.RuneCountInString(input.Query) > maxQueryCharacters {
		return nil, fmt.Errorf("query must be at most %d characters", maxQueryCharacters)
	}
	if strings.TrimSpace(input.Query) == "" && len(input.Sources) == 0 && len(input.Domains) == 0 {
		return nil, fmt.Errorf("query, sources, or domains must be set")
	}
	if strings.TrimSpace(input.Query) != "" {
		query.Set("q", input.Query)
	}
	for _, field := range input.SearchIn {
		if !slices.Contains(SearchInFields, field) {
			return nil, fmt.Errorf("searchIn accepts only title, description, and content")
		}
	}
	if len(input.SearchIn) > 0 {
		query.Set("searchIn", strings.Join(input.SearchIn, ","))
	}
	if len(input.Sources) > maxSources {
		return nil, fmt.Errorf("sources accepts at most %d source IDs", maxSources)
	}
	for _, list := range []struct {
		name    string
		values  []string
		pattern *regexp.Regexp
	}{
		{"sources", input.Sources, sourceIDPattern},
		{"domains", input.Domains, domainPattern},
		{"excludeDomains", input.ExcludeDomains, domainPattern},
	} {
		for _, value := range list.values {
			if !list.pattern.MatchString(value) {
				return nil, fmt.Errorf("%s contains an invalid entry", list.name)
			}
		}
		if len(list.values) > 0 {
			query.Set(list.name, strings.Join(list.values, ","))
		}
	}
	from, err := setTimeParameter(query, "from", input.From)
	if err != nil {
		return nil, err
	}
	to, err := setTimeParameter(query, "to", input.To)
	if err != nil {
		return nil, err
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, fmt.Errorf("from must not be after to")
	}
	if input.Language != "" {
		if !slices.Contains(Languages, input.Language) {
			return nil, fmt.Errorf("language must be one of %s", strings.Join(Languages, ", "))
		}
		query.Set("language", input.Language)
	}
	if input.SortBy != "" {
		if !slices.Contains(SortOrders, input.SortBy) {
			return nil, fmt.Errorf("sortBy must be publishedAt, relevancy, or popularity")
		}
		query.Set("sortBy", input.SortBy)
	}
	if input.PageSize < 0 || input.PageSize > maxPageSize {
		return nil, fmt.Errorf("pageSize must be from 1 through %d, or zero for NewsAPI's default", maxPageSize)
	}
	if input.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.Page < 0 {
		return nil, fmt.Errorf("page must be positive, or zero for the first page")
	}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	return query, nil
}

// setTimeParameter sends value unchanged after checking that it uses a documented ISO 8601 form.
func setTimeParameter(query url.Values, name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			query.Set(name, value)
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s must be a date such as 2026-01-05 or an ISO 8601 time", name)
}

func (client *Client) newSearchRequest(call sdkgo.Call, query url.Values) (*http.Request, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, failurePointer(sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, failurePointer(sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/everything?"+query.Encode(), nil)
	if err != nil {
		return nil, failurePointer(sdkgo.FailureLocalDefect, "NewsAPI request could not be built")
	}
	// The key travels only in this header, so it never appears in a URL or a proxy log.
	request.Header.Set("X-Api-Key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "superdurable-newsapi/0.21")
	return request, nil
}

// classifyErrorResponse maps a non-200 response to Retry or a branch using only NewsAPI's error code.
func classifyErrorResponse(response *http.Response) sdkgo.QueryAttempt[ArticlePage] {
	// A truncated or unreadable error body leaves the code empty; the status still classifies it.
	body, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
	code := ""
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/code"}); len(tokens) == 1 {
		code = tokens[0]
	}
	describedCode := describeErrorCode(code)
	switch {
	case code == "apiKeyExhausted":
		return failedBranch(SearchArticlesBranchProviderRejected, sdkgo.FailureQuotaExhausted, "NewsAPI reports the API key's request allowance is exhausted ("+describedCode+")")
	case response.StatusCode == http.StatusTooManyRequests:
		retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		return sdkgo.NewQueryRetry[ArticlePage](newFailure(sdkgo.FailureRateLimit, "NewsAPI rate limited the request ("+describedCode+")"), retryAfter)
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError:
		retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		return sdkgo.NewQueryRetry[ArticlePage](newFailure(sdkgo.FailureAvailability, "NewsAPI is temporarily unavailable ("+describedCode+")"), retryAfter)
	case response.StatusCode == http.StatusUnauthorized || slices.Contains(authenticationErrorCodes, code):
		return failedBranch(SearchArticlesBranchProviderRejected, sdkgo.FailureAuthentication, "NewsAPI rejected the API key ("+describedCode+")")
	case response.StatusCode == http.StatusUpgradeRequired:
		return failedBranch(SearchArticlesBranchProviderRejected, sdkgo.FailureAuthorization, "NewsAPI's plan does not permit this request ("+describedCode+")")
	case slices.Contains(parameterErrorCodes, code):
		return failedBranch(SearchArticlesBranchProviderRejected, sdkgo.FailureValidation, "NewsAPI rejected the search parameters ("+describedCode+")")
	default:
		return failedBranch(SearchArticlesBranchProviderRejected, sdkgo.FailureProviderRejection, "NewsAPI rejected the request with HTTP "+strconv.Itoa(response.StatusCode)+" ("+describedCode+")")
	}
}

// describeErrorCode returns a documented NewsAPI code, or a fixed label, so a Failure never echoes arbitrary text.
func describeErrorCode(code string) string {
	documented := append(append([]string{"apiKeyExhausted", "rateLimited", "unexpectedError", "maximumResultsReached"}, authenticationErrorCodes...), parameterErrorCodes...)
	if slices.Contains(documented, code) {
		return code
	}
	return "no documented error code"
}

func decodeArticlePage(body []byte, input SearchArticlesInput) sdkgo.QueryAttempt[ArticlePage] {
	var wirePage wireArticlePage
	if err := json.Unmarshal(body, &wirePage); err != nil {
		return failedBranch(SearchArticlesBranchInvalidResponse, sdkgo.FailureProtocol, "NewsAPI returned malformed JSON")
	}
	if wirePage.Status != "ok" || wirePage.TotalResults == nil || *wirePage.TotalResults < 0 || wirePage.Articles == nil {
		return failedBranch(SearchArticlesBranchInvalidResponse, sdkgo.FailureProtocol, "NewsAPI returned an incomplete article page")
	}
	page := max(input.Page, 1)
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = maxPageSize
	}
	if len(wirePage.Articles) > pageSize {
		return failedBranch(SearchArticlesBranchInvalidResponse, sdkgo.FailureProtocol, "NewsAPI returned more articles than the page size")
	}
	for index := range wirePage.Articles {
		wirePage.Articles[index].PublishedAt = wirePage.Articles[index].PublishedAt.UTC()
	}
	result := ArticlePage{
		TotalResults: *wirePage.TotalResults, Articles: wirePage.Articles, Page: page,
		HasMore: page*pageSize < *wirePage.TotalResults,
	}
	return sdkgo.NewQueryBranch(SearchArticlesBranchSearched, result, nil, sdkgo.Receipt{})
}

func failedBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ArticlePage] {
	return sdkgo.NewQueryBranch(branch, ArticlePage{}, failurePointer(kind, message), sdkgo.Receipt{})
}

func failurePointer(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(kind, message)
	return &failure
}

func newFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: searchArticlesOperationID, Message: message}
}
