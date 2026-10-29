// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "woocommerce", Name: "woocommerce"}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{ConsumerKey: sdkgo.NewSecretString("ck_test"), ConsumerSecret: sdkgo.NewSecretString("cs_test")}})
	require.NoError(t, err)
	return client
}

func TestListOrdersReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "ck_test", user)
		require.Equal(t, "cs_test", password)
		require.NotContains(t, r.URL.RawQuery, "cs_test")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/wp-json/wc/v3/orders", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "[{\"id\":101,\"status\":\"processing\",\"total\":\"29.00\",\"currency\":\"USD\"}]")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/wp-json/wc/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.Orders, 1)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Page: 1, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func TestGetOrderReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "ck_test", user)
		require.Equal(t, "cs_test", password)
		require.NotContains(t, r.URL.RawQuery, "cs_test")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/wp-json/wc/v3/orders/101", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"id\":101,\"status\":\"processing\",\"total\":\"29.00\",\"currency\":\"USD\"}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/wp-json/wc/v3")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetOrder(), testConnection, GetOrderInput{OrderID: 101})
	require.NoError(t, err)
	require.Equal(t, GetOrderBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	require.Positive(t, result.Value.ID)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetOrder(), testConnection, GetOrderInput{OrderID: 101})
	require.NoError(t, err)
	require.Equal(t, GetOrderBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}
