// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zentao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "zentao", Name: "zentao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zentao-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-products" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, token string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(token)}})
	require.NoError(t, err)
	return client
}
func TestListProductsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/zentao/api.php/v1/products", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "secretToken", r.Header.Get("Token"))
		_, err := w.Write([]byte(`{"total":1,"products":[{"id":6,"name":"测试","code":"example","type":"normal","acl":"private","PO":{"realname":"person"},"whitelist":[{"account":"private"}]}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/zentao", "secretToken").ListProducts(), testConnection, ListProductsInput{})
	require.NoError(t, err)
	require.Equal(t, ListProductsBranchListed, result.Branch)
	require.Equal(t, ProductList{Total: 1, Products: []Product{{ID: 6, Name: "测试", Code: "example", Type: "normal", AccessControl: "private"}}}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestListProductsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"total":0,"products":[]}`, ListProductsBranchListed, false},
		{"unauthorized", 401, `{}`, ListProductsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListProductsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListProductsBranchInvalidResponse, false},
		{"missing products", 200, `{"total":0}`, ListProductsBranchInvalidResponse, false},
		{"invalid product", 200, `{"total":1,"products":[{"id":0,"name":"x","type":"normal","acl":"open"}]}`, ListProductsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretToken").ListProducts(), testConnection, ListProductsInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidTokenNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "bad token").ListProducts(), testConnection, ListProductsInput{})
	require.NoError(t, err)
	require.Equal(t, ListProductsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestInstallationURLValidation(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString("secretToken")}}
	for _, endpoint := range []string{"", "http://example.com/zentao", "https://example.com/zentao/api.php", "https://example.com/zentao?token=x"} {
		_, err := New(Config{BaseURL: endpoint}, credentials)
		require.Error(t, err, endpoint)
	}
}
func TestTransportErrorDoesNotExposeToken(t *testing.T) {
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "http://127.0.0.1:1", "privateToken").ListProducts(), testConnection, ListProductsInput{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "privateToken")
}
