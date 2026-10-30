// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newsapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "0123456789abcdef0123456789abcdef"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "newsapi", Name: "newsapi"}

func newTestClient(t *testing.T, baseURL string, config Config) *Client {
	t.Helper()
	config.BaseURL = baseURL
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func searchArticles(t *testing.T, client *Client, input SearchArticlesInput) (SearchArticlesResult, error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, client.SearchArticles(), testConnection, input)
}

func TestSearchSendsTheKeyOnlyInTheHeaderAndEncodesTheQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/everything", r.URL.Path)
		require.Equal(t, testAPIKey, r.Header.Get("X-Api-Key"))
		require.NotContains(t, r.URL.RawQuery, testAPIKey, "the key never travels in the URL")
		require.Equal(t, url.Values{
			"q": {`"AT&T" #1 + co`}, "searchIn": {"title,description"}, "sources": {"bbc-news,the-verge"},
			"domains": {"techcrunch.com"}, "excludeDomains": {"example.com"}, "from": {"2026-09-25"},
			"to": {"2026-10-05T07:00:00Z"}, "language": {"en"}, "sortBy": {"publishedAt"}, "pageSize": {"2"}, "page": {"2"},
		}, r.URL.Query())
		_, err := fmt.Fprint(w, `{"status":"ok","totalResults":5,"articles":[
			{"source":{"id":null,"name":"Example Wire"},"author":null,"title":"Example & partners","description":null,
			 "url":"https://news.example/a","urlToImage":null,"publishedAt":"2026-10-04T12:30:00Z","content":null},
			{"source":{"id":"bbc-news","name":"BBC News"},"author":"Reporter","title":"Second","description":"",
			 "url":"https://news.example/b","urlToImage":"https://news.example/b.jpg","publishedAt":"2026-10-04T05:00:00-07:00","content":"Body"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := searchArticles(t, newTestClient(t, server.URL+"/v2", Config{}), SearchArticlesInput{
		Query: `"AT&T" #1 + co`, SearchIn: []string{"title", "description"}, Sources: []string{"bbc-news", "the-verge"},
		Domains: []string{"techcrunch.com"}, ExcludeDomains: []string{"example.com"}, From: "2026-09-25",
		To: "2026-10-05T07:00:00Z", Language: "en", SortBy: "publishedAt", PageSize: 2, Page: 2,
	})
	require.NoError(t, err)
	require.Equal(t, SearchArticlesBranchSearched, result.Branch)
	require.Nil(t, result.Failure)
	page := result.Value
	require.Equal(t, 5, page.TotalResults)
	require.Equal(t, 2, page.Page)
	require.True(t, page.HasMore, "pages one and two hold four of five results")
	require.Len(t, page.Articles, 2)

	first := page.Articles[0]
	require.Nil(t, first.Source.ID)
	require.Nil(t, first.Author)
	require.Nil(t, first.Description, "null stays distinguishable from an empty description")
	require.Nil(t, first.Content)
	require.Equal(t, "Example & partners", first.Title, "provider text is returned unescaped")
	second := page.Articles[1]
	require.Equal(t, "bbc-news", *second.Source.ID)
	require.Equal(t, "", *second.Description)
	require.Equal(t, time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), second.PublishedAt)
	require.Equal(t, time.UTC, second.PublishedAt.Location())
}

func TestEmptyInputFieldsAreNotSent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, url.Values{"q": {"acme"}}, r.URL.Query())
		_, err := fmt.Fprint(w, `{"status":"ok","totalResults":0,"articles":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := searchArticles(t, newTestClient(t, server.URL, Config{}), SearchArticlesInput{Query: "acme"})
	require.NoError(t, err)
	require.Equal(t, SearchArticlesBranchSearched, result.Branch)
	require.Equal(t, ArticlePage{TotalResults: 0, Articles: []Article{}, Page: 1}, result.Value)
}

func TestRemovedArticlesAreReturnedUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprint(w, `{"status":"ok","totalResults":101,"articles":[{"source":{"id":null,"name":"[Removed]"},"author":null,
			"title":"[Removed]","description":"[Removed]","url":"https://removed.com","urlToImage":null,"publishedAt":"1970-01-01T00:00:00Z","content":"[Removed]"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := searchArticles(t, newTestClient(t, server.URL, Config{}), SearchArticlesInput{Domains: []string{"example.com"}})
	require.NoError(t, err)
	require.Equal(t, SearchArticlesBranchSearched, result.Branch)
	require.Equal(t, "[Removed]", result.Value.Articles[0].Title)
	require.True(t, result.Value.HasMore, "the default page size is 100")
}

func TestHTTPFailureClassification(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		isRetry bool
	}{
		{"invalid key", 401, `{"status":"error","code":"apiKeyInvalid","message":"Your API key is invalid SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"missing key", 400, `{"status":"error","code":"apiKeyMissing","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"exhausted key", 429, `{"status":"error","code":"apiKeyExhausted","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureQuotaExhausted, false},
		{"bad parameter", 400, `{"status":"error","code":"parameterInvalid","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureValidation, false},
		{"unknown source", 400, `{"status":"error","code":"sourceDoesNotExist","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureValidation, false},
		{"plan limit", 426, `{"status":"error","code":"maximumResultsReached","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"redirect", 302, ``, SearchArticlesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"undocumented code", 418, `{"status":"error","code":"SENTINEL","message":"SENTINEL"}`, SearchArticlesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `{"status":"error","code":"rateLimited","message":"SENTINEL"}`, "", sdkgo.FailureRateLimit, true},
		{"server error", 500, `{"status":"error","code":"unexpectedError","message":"SENTINEL"}`, "", sdkgo.FailureAvailability, true},
		{"unavailable", 503, `<html>SENTINEL</html>`, "", sdkgo.FailureAvailability, true},
		{"timeout", 408, ``, "", sdkgo.FailureAvailability, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Location", "/elsewhere")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := searchArticles(t, newTestClient(t, server.URL, Config{}), SearchArticlesInput{Query: "acme"})
			if test.isRetry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), "SENTINEL")
				require.Contains(t, err.Error(), string(test.kind))
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL", "provider text never enters a Failure")
			require.NotContains(t, result.Failure.Message, testAPIKey)
		})
	}
}

func TestInvalidInputSelectsDefectWithoutARequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL, Config{})
	for name, input := range map[string]SearchArticlesInput{
		"no scope":          {Query: "  "},
		"long query":        {Query: strings.Repeat("a", 501)},
		"unknown field":     {Query: "a", SearchIn: []string{"body"}},
		"too many sources":  {Sources: strings.Split(strings.Repeat("s,", 21)[:41], ",")},
		"invalid source":    {Sources: []string{"BBC News"}},
		"comma in domain":   {Domains: []string{"a.com,b.com"}},
		"invalid exclusion": {Query: "a", ExcludeDomains: []string{"a b"}},
		"invalid from":      {Query: "a", From: "yesterday"},
		"from after to":     {Query: "a", From: "2026-10-05", To: "2026-10-01"},
		"unknown language":  {Query: "a", Language: "xx"},
		"unknown sort":      {Query: "a", SortBy: "date"},
		"large page size":   {Query: "a", PageSize: 101},
		"negative size":     {Query: "a", PageSize: -1},
		"negative page":     {Query: "a", Page: -1},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := searchArticles(t, client, input)
			require.NoError(t, err)
			require.Equal(t, SearchArticlesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, requests.Load())
}

func TestMalformedAndOversizedResponsesSelectInvalidResponse(t *testing.T) {
	for name, body := range map[string]string{
		"bad JSON":            `{`,
		"error status":        `{"status":"error","totalResults":0,"articles":[]}`,
		"missing articles":    `{"status":"ok","totalResults":1}`,
		"missing total":       `{"status":"ok","articles":[]}`,
		"negative total":      `{"status":"ok","totalResults":-1,"articles":[]}`,
		"page overflow":       `{"status":"ok","totalResults":3,"articles":[{"title":"a"},{"title":"b"},{"title":"c"}]}`,
		"bad publication":     `{"status":"ok","totalResults":1,"articles":[{"title":"a","publishedAt":"yesterday"}]}`,
		"oversized response!": `{"status":"ok","totalResults":0,"articles":[]}` + strings.Repeat(" ", 64),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := fmt.Fprint(w, body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := searchArticles(t, newTestClient(t, server.URL, Config{MaxResponseBytes: 96}), SearchArticlesInput{Query: "a", PageSize: 2})
			require.NoError(t, err)
			require.Equal(t, SearchArticlesBranchInvalidResponse, result.Branch)
		})
	}
}

func TestTransportFailureRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := newTestClient(t, server.URL, Config{})
	server.Close()
	_, err := searchArticles(t, client, SearchArticlesInput{Query: "a"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureAvailability))
}

func TestCredentialsAreResolvedForEachCall(t *testing.T) {
	var receivedKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKeys = append(receivedKeys, r.Header.Get("X-Api-Key"))
		_, err := fmt.Fprint(w, `{"status":"ok","totalResults":0,"articles":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: server.URL}, credentials)
	require.NoError(t, err)
	_, err = searchArticles(t, client, SearchArticlesInput{Query: "a"})
	require.NoError(t, err)
	credentials[testConnection] = Credentials{APIKey: sdkgo.NewSecretString("fedcba9876543210fedcba9876543210")}
	_, err = searchArticles(t, client, SearchArticlesInput{Query: "a"})
	require.NoError(t, err)
	require.Equal(t, []string{testAPIKey, "fedcba9876543210fedcba9876543210"}, receivedKeys, "a replaced key applies without a new client")
}

func TestUnusableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	for name, credentials := range map[string]sdkgo.StaticCredentialProvider[Credentials]{
		"unconfigured": {},
		"blank":        {testConnection: {APIKey: sdkgo.NewSecretString("")}},
		"header split": {testConnection: {APIKey: sdkgo.NewSecretString("abc\r\nX-Injected: 1")}},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := New(Config{BaseURL: server.URL}, credentials)
			require.NoError(t, err)
			result, err := searchArticles(t, client, SearchArticlesInput{Query: "a"})
			require.NoError(t, err)
			require.Equal(t, SearchArticlesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		})
	}
	require.Zero(t, requests.Load())
}

func TestNewValidatesItsDependencies(t *testing.T) {
	_, err := New(Config{}, nil)
	require.ErrorContains(t, err, "credential source is required")
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	_, err = New(Config{BaseURL: "http://newsapi.example/v2"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = New(Config{BaseURL: "https://newsapi.org/v2?apiKey=x"}, credentials)
	require.Error(t, err)
	client, err := New(Config{}, credentials)
	require.NoError(t, err)
	require.Equal(t, "https://newsapi.org/v2", client.baseURL)
	require.Equal(t, 15*time.Second, client.httpClient.Timeout)
}
