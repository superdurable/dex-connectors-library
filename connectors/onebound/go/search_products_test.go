// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onebound

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "onebound", Name: "onebound"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "onebound-provider-test" }
func (queryContext) StepExecutionID() string     { return "product-search" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("testKey"), APISecret: sdkgo.NewSecretString("testSecret")}})
	require.NoError(t, err)
	return client
}
func TestOfficialSignatureVector(t *testing.T) {
	parameters := map[string]string{"api_name": "item_search", "dateline": "2026-11-13 18:00:00", "key": "testKey", "page": "2", "page_size": "40", "q": "工作手套"}
	require.Equal(t, "8f2de999eee584176b353fdc6c7cc744", signSearchParameters(parameters, "testSecret"))
}
func TestSearchProductsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/1688/item_search/", r.URL.Path)
		require.Equal(t, "testKey", r.URL.Query().Get("key"))
		require.Empty(t, r.URL.Query().Get("secret"))
		require.Equal(t, "gloves", r.URL.Query().Get("q"))
		require.Equal(t, "2", r.URL.Query().Get("page"))
		parameters := map[string]string{"api_name": "item_search", "key": "testKey", "q": "gloves", "page": "2", "page_size": "40", "dateline": r.URL.Query().Get("dateline")}
		require.Equal(t, signSearchParameters(parameters, "testSecret"), r.URL.Query().Get("sign"))
		_, err := w.Write([]byte(`{"error_code":"0000","items":{"page":"2","total_results":43,"item":[{"num_iid":"123456789","title":"Work gloves","price":"12.00","detail_url":"https://detail.1688.com/offer/123456789.html","email":"sensitive@example.com"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "gloves", PageNumber: 2})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchListed, result.Branch)
	require.Equal(t, ProductSearchPage{PageNumber: 2, Total: 43, Products: []ProductSearchItem{{ProductID: "123456789", Title: "Work gloves", Price: "12.00", DetailURL: "https://detail.1688.com/offer/123456789.html"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sensitive@example.com")
	require.NotContains(t, string(encoded), "Private Person")
	require.NotContains(t, string(encoded), "private")
	require.EqualValues(t, 1, calls.Load())
}
func TestSearchProductsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"error_code":"2000"}`, SearchProductsBranchListed, false},
		{"provider code", 200, `{"error_code":"4005"}`, SearchProductsBranchProviderRejected, false},
		{"insufficient balance", 200, `{"error_code":"4016"}`, SearchProductsBranchProviderRejected, false},
		{"maintenance", 200, `{"error_code":"4009"}`, "", true},
		{"concurrency", 200, `{"error_code":"4008"}`, "", true},
		{"invalid token", 401, `{}`, SearchProductsBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, SearchProductsBranchInvalidResponse, false},
		{"missing page", 200, `{"error_code":"0000"}`, SearchProductsBranchInvalidResponse, false},
		{"missing product", 200, `{"error_code":"0000","items":{"page":"1","total_results":1,"item":[{"num_iid":"123"}]}}`, SearchProductsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "gloves"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "testKey")
				}
			}
		})
	}
}
func TestInvalidPageAndKeyNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{PageNumber: -1})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchDefect, result.Branch)
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("bad key"), APISecret: sdkgo.NewSecretString("testSecret")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.SearchProducts(), testConnection, SearchProductsInput{Keyword: "gloves"})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "gloves"})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "testKey")
	require.False(t, strings.Contains(result.Failure.Message, "sign="))
}
