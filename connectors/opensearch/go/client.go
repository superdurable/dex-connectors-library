// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package opensearch searches index documents through typed Dex Query Steps.
package opensearch

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable cluster settings and resolves credentials for each call.
type Client struct {
	clusterURL       string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SearchDocumentsInput selects an index, Lucene query, and bounded offset page.
type SearchDocumentsInput struct {
	// Index is one concrete lowercase OpenSearch index name.
	Index string `json:"index"`
	// Query is a Lucene query string, such as title:Meridian.
	Query string `json:"query"`
	// Size is 1 through 100; zero uses ten results.
	Size int `json:"size,omitempty"`
	// From is a zero-based offset; from plus size may not exceed 10000.
	From int `json:"from,omitempty"`
}

// SearchHit identifies one matching document and its untrusted source JSON.
type SearchHit struct {
	// Index is the index that produced this hit.
	Index string `json:"index"`
	// ID is the provider document ID.
	ID string `json:"id"`
	// Score is OpenSearch's relevance score, if available.
	Score *float64 `json:"score,omitempty"`
	// Source is provider-owned document JSON and may contain sensitive user data.
	Source json.RawMessage `json:"source"`
}

// SearchPage is one bounded search result page.
type SearchPage struct {
	// Total is the provider's matching hit count or lower bound.
	Total int64 `json:"total"`
	// Relation is eq for an exact count or gte for a lower bound.
	Relation string `json:"relation"`
	// Hits contains this page's matching documents.
	Hits []SearchHit `json:"hits"`
}

// SearchDocumentsOperation searches one index.
type SearchDocumentsOperation struct{ client *Client }

// New validates cluster settings without contacting OpenSearch. Credentials resolve for each call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("OpenSearch credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	clusterURL, err := providerhttp.ValidateBaseURL(config.ClusterURL)
	if err != nil {
		return nil, fmt.Errorf("OpenSearch cluster URL: %w", err)
	}
	client := &Client{clusterURL: clusterURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("OpenSearch client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SearchDocuments returns the typed search Query bound to this client.
func (client *Client) SearchDocuments() SearchDocumentsOperation {
	return SearchDocumentsOperation{client}
}

// Definition returns the manifest-generated search contract.
func (SearchDocumentsOperation) Definition() sdkgo.QueryDefinition { return SearchDocumentsDefinition }

var indexNamePattern = regexp.MustCompile(`^[a-z0-9.][a-z0-9._-]*$`)

// Invoke runs a read-only search and rejects partial or timed-out responses.
func (operation SearchDocumentsOperation) Invoke(call sdkgo.Call, input SearchDocumentsInput) sdkgo.QueryAttempt[SearchPage] {
	if !validIndexName(input.Index) || strings.TrimSpace(input.Query) == "" || !utf8.ValidString(input.Query) || len(input.Query) > 4096 {
		return searchFailure(SearchDocumentsBranchDefect, sdkgo.FailureValidation, "index or query is invalid")
	}
	size := input.Size
	if size == 0 {
		size = 10
	}
	if size < 1 || size > 100 || input.From < 0 || input.From+size > 10000 {
		return searchFailure(SearchDocumentsBranchDefect, sdkgo.FailureValidation, "size and from must fit a 10000-result window")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(credentials.Password.Reveal()) {
		return searchFailure(SearchDocumentsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"q": {input.Query}, "size": {strconv.Itoa(size)}, "from": {strconv.Itoa(input.From)}, "allow_partial_search_results": {"false"}}
	endpoint := operation.client.clusterURL + "/" + input.Index + "/_search?" + query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return searchFailure(SearchDocumentsBranchDefect, sdkgo.FailureLocalDefect, "search request could not be built")
	}
	request.SetBasicAuth(credentials.Username, credentials.Password.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "OpenSearch request failed", 0)
	}
	// Closing a read-only response cannot change the query outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return searchRetry(sdkgo.FailureRateLimit, "OpenSearch rate limited the search", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= 500:
		return searchRetry(sdkgo.FailureAvailability, "OpenSearch is temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return searchFailure(SearchDocumentsBranchIndexNotFound, sdkgo.FailureNotFound, "OpenSearch index was not found")
	case response.StatusCode == http.StatusUnauthorized:
		return searchFailure(SearchDocumentsBranchProviderRejected, sdkgo.FailureAuthentication, "OpenSearch rejected the credentials")
	case response.StatusCode == http.StatusForbidden:
		return searchFailure(SearchDocumentsBranchProviderRejected, sdkgo.FailureAuthorization, "OpenSearch denied index access")
	case response.StatusCode != http.StatusOK:
		return searchFailure(SearchDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "OpenSearch rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return searchFailure(SearchDocumentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OpenSearch response exceeded the configured limit")
	}
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "OpenSearch response was interrupted", 0)
	}
	page, shouldRetry, err := decodeSearchPage(body)
	if shouldRetry {
		return searchRetry(sdkgo.FailureAvailability, "OpenSearch search was incomplete", 0)
	}
	if err != nil {
		return searchFailure(SearchDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "OpenSearch returned unusable search data")
	}
	return sdkgo.NewQueryBranch(SearchDocumentsBranchFound, page, nil, sdkgo.Receipt{})
}

func decodeSearchPage(body []byte) (SearchPage, bool, error) {
	var wire struct {
		TimedOut *bool `json:"timed_out"`
		Shards   *struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value    int64  `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
			Hits []struct {
				Index  string          `json:"_index"`
				ID     string          `json:"_id"`
				Score  *float64        `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SearchPage{}, false, err
	}
	if wire.TimedOut == nil || wire.Shards == nil {
		return SearchPage{}, false, fmt.Errorf("missing search completion metadata")
	}
	if *wire.TimedOut || wire.Shards.Failed > 0 {
		return SearchPage{}, true, nil
	}
	if wire.Hits.Total.Value < 0 || (wire.Hits.Total.Relation != "eq" && wire.Hits.Total.Relation != "gte") || wire.Hits.Hits == nil {
		return SearchPage{}, false, fmt.Errorf("missing search hits or total")
	}
	page := SearchPage{Total: wire.Hits.Total.Value, Relation: wire.Hits.Total.Relation, Hits: make([]SearchHit, 0, len(wire.Hits.Hits))}
	for _, hit := range wire.Hits.Hits {
		if hit.Index == "" || hit.ID == "" || !json.Valid(hit.Source) {
			return SearchPage{}, false, fmt.Errorf("invalid search hit")
		}
		page.Hits = append(page.Hits, SearchHit{Index: hit.Index, ID: hit.ID, Score: hit.Score, Source: hit.Source})
	}
	return page, false, nil
}

func validIndexName(index string) bool {
	return len(index) <= 255 && indexNamePattern.MatchString(index) && index != "." && index != ".."
}
func validUsername(username string) bool {
	return username != "" && utf8.ValidString(username) && !strings.ContainsAny(username, ":\r\n\x00")
}

func searchFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SearchPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "opensearch", Operation: "searchDocuments", Message: message}
	return sdkgo.NewQueryBranch(branch, SearchPage{}, &failure, sdkgo.Receipt{})
}

func searchRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[SearchPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "opensearch", Operation: "searchDocuments", Message: message}
	return sdkgo.NewQueryRetry[SearchPage](failure, retryAfter)
}
