// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smzdm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "smzdm", Name: "smzdm"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "smzdm-provider-test" }
func (queryContext) StepExecutionID() string     { return "search-products" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString("123456789"), AppSecret: sdkgo.NewSecretString("88888888")}})
	require.NoError(t, err)
	return client
}
func TestOfficialSignatureVector(t *testing.T) {
	parameters := map[string]string{"timestamp": "1480411125", "app_key": "123456789", "order_status": "1", "page_size": "10", "page": "1"}
	require.Equal(t, "27C4D45CE6F51B71493FC2B9AA80DB23", signProducts(parameters, "88888888"))
}
func TestSearchProductsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/searchapi/search/list", r.URL.Path)
		query := r.URL.Query()
		require.Equal(t, "123456789", query.Get("app_key"))
		require.Equal(t, "new_wiki", query.Get("c"))
		require.Equal(t, "dex", query.Get("request_from"))
		require.Equal(t, "笔记本", query.Get("s"))
		require.Equal(t, "20", query.Get("size"))
		require.Equal(t, "0", query.Get("offset"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), "88888888")
		require.InDelta(t, time.Now().Unix(), mustTimestamp(t, query.Get("timestamp")), 5)
		params := map[string]string{}
		for key, values := range query {
			if key != "sign" {
				require.Len(t, values, 1)
				params[key] = values[0]
			}
		}
		require.Equal(t, signProducts(params, "88888888"), query.Get("sign"))
		_, err := w.Write([]byte(`{"status":true,"data":{"total_num":1,"offset":0,"page_size":20,"rows":[{"title":"Example Laptop","pro_price":99.9}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "笔记本"})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchListed, result.Branch)
	require.Equal(t, ProductPage{Total: 1, Products: []Product{{Title: "Example Laptop", PriceYuan: 99.9}}}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func mustTimestamp(t *testing.T, value string) int64 {
	t.Helper()
	timestamp, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)
	return timestamp
}
func TestSearchProductsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"status":true,"data":{"total_num":0,"rows":[]}}`, SearchProductsBranchListed, false},
		{"unauthorized", 401, `{}`, SearchProductsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, SearchProductsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"provider rejection", 200, `{"status":false,"msg":"invalid sign"}`, SearchProductsBranchProviderRejected, false},
		{"malformed", 200, `{`, SearchProductsBranchInvalidResponse, false},
		{"missing rows", 200, `{"status":true,"data":{"total_num":1}}`, SearchProductsBranchInvalidResponse, false},
		{"missing price", 200, `{"status":true,"data":{"total_num":1,"rows":[{"title":"X"}]}}`, SearchProductsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "笔记本"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidKeywordNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "\n"})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectNeverForwardsCredentials(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchProducts(), testConnection, SearchProductsInput{Keyword: "手机"})
	require.NoError(t, err)
	require.Equal(t, SearchProductsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "88888888")
	require.False(t, strings.Contains(result.Failure.Message, "123456789"))
}
