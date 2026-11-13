// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easyapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "easyapi", Name: "easyapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "easyapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "shop" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString("test-key"), AppSecret: sdkgo.NewSecretString("private-secret")}})
	require.NoError(t, err)
	return client
}
func runShop(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[ShopResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryShop(), testConnection, QueryShopInput{TaxNumber: "91320211MA1WML8X6T"})
}
func TestQueryShopRequestAndPrivateOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/shop/query", r.URL.Path)
		require.Equal(t, "test-key", r.URL.Query().Get("appKey"))
		require.Equal(t, "private-secret", r.URL.Query().Get("appSecret"))
		require.Equal(t, "91320211MA1WML8X6T", r.URL.Query().Get("taxNumber"))
		_, err := w.Write([]byte(`{"code":1,"message":"提交成功","content":{"shopId":1001,"name":"Example Company","apiState":1,"endTime":"2027-01-01 00:00:00","identifyNumber":"private tax","address":"private address","phone":"private phone","bankAccount":"private bank","drawerName":"private staff"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runShop(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryShopBranchFound, result.Branch)
	require.Equal(t, ShopResult{ShopID: 1001, Name: "Example Company", APIState: 1, EndTime: "2027-01-01 00:00:00"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	for _, secret := range []string{"private tax", "private address", "private phone", "private bank", "private staff", "private-secret"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.EqualValues(t, 1, calls.Load())
}
func TestQueryShopOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejected", 200, `{"code":-7,"message":"bad secret"}`, QueryShopBranchProviderRejected, false},
		{"HTTP auth rejection", 400, `{"code":-7}`, QueryShopBranchProviderRejected, false},
		{"HTTP limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad JSON", 200, `{`, QueryShopBranchInvalidResponse, false},
		{"missing state", 200, `{"code":1,"content":{"shopId":1001,"name":"Example Company"}}`, QueryShopBranchInvalidResponse, false},
		{"unknown state", 200, `{"code":1,"content":{"shopId":1001,"name":"Example Company","apiState":8}}`, QueryShopBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runShop(t, server)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private-secret")
					require.NotContains(t, result.Failure.Message, "91320211MA1WML8X6T")
				}
			}
		})
	}
}
func TestInvalidTaxNumberAndCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryShop(), testConnection, QueryShopInput{TaxNumber: "bad"})
	require.NoError(t, err)
	require.Equal(t, QueryShopBranchDefect, result.Branch)
	client, err = New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString("test-key"), AppSecret: sdkgo.NewSecretString("")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryShop(), testConnection, QueryShopInput{TaxNumber: "91320211MA1WML8X6T"})
	require.NoError(t, err)
	require.Equal(t, QueryShopBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runShop(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryShopBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
}
