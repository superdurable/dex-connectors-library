// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chukou1

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "chukou1", Name: "chukou1"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "chukou1-provider-test" }
func (queryContext) StepExecutionID() string     { return "warehouse-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("secretTestToken")}})
	require.NoError(t, err)
	return client
}
func TestListWarehousesRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/warehouses", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "Bearer secretTestToken", r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`[{"WarehouseId":"US","WarehouseName":"San Francisco Warehouse","Country":"United States","CountryCode":"US"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchListed, result.Branch)
	require.Equal(t, Warehouse{ID: "US", Name: "San Francisco Warehouse", Country: "United States", CountryCode: "US"}, result.Value.Warehouses[0])
	require.EqualValues(t, 1, calls.Load())
}
func TestListWarehousesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{{"empty", 200, `[]`, ListWarehousesBranchListed, false}, {"invalid token", 401, `{}`, ListWarehousesBranchProviderRejected, false}, {"precondition", 412, `{}`, ListWarehousesBranchProviderRejected, false}, {"rate limited", 429, `{}`, "", true}, {"unavailable", 503, `{}`, "", true}, {"malformed", 200, `{`, ListWarehousesBranchInvalidResponse, false}, {"null", 200, `null`, ListWarehousesBranchInvalidResponse, false}, {"missing warehouse", 200, `[{"WarehouseId":"US"}]`, ListWarehousesBranchInvalidResponse, false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
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
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("Bearer bad")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardToken(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestToken")
	require.False(t, strings.Contains(result.Failure.Message, "Bearer"))
}
