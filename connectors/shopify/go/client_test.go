// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string                          { return "provider-test" }
func (queryContext) StepExecutionID() string                 { return "query" }
func (queryContext) RunID() string                           { return "provider-run" }
func (queryContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (queryContext) Attempt() int32                          { return 1 }
func (queryContext) RecordHeartbeat(any) error               { return nil }
func (queryContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }
func (queryContext) Deadline() (time.Time, bool)             { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}                   { return nil }
func (queryContext) Err() error                              { return nil }
func (queryContext) Value(key any) any                       { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "shopify", Name: "shopify"}

type redirectTestTransport struct {
	endpoint *url.URL
}

func (transport redirectTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	forwarded := request.Clone(request.Context())
	forwarded.URL.Scheme = transport.endpoint.Scheme
	forwarded.URL.Host = transport.endpoint.Host
	return http.DefaultTransport.RoundTrip(forwarded)
}

func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	endpoint, err := url.Parse(serverURL)
	require.NoError(t, err)
	client, err := New(Config{ShopSubdomain: "test-shop"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{AccessToken: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(&http.Client{Transport: redirectTestTransport{endpoint: endpoint}}))
	require.NoError(t, err)
	return client
}

func TestNewRejectsInvalidShopSubdomain(t *testing.T) {
	credentialSource := sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{AccessToken: sdkgo.NewSecretString("test-token")}}
	for _, shopSubdomain := range []string{"", "other.example.com", "bad/shop", "-shop", "shop-", "Uppercase"} {
		_, err := New(Config{ShopSubdomain: shopSubdomain}, credentialSource)
		require.Error(t, err, shopSubdomain)
	}
}

func TestGetOrderReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-token", r.Header.Get("X-Shopify-Access-Token"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/admin/api/2026-10/orders/101.json", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"order\":{\"id\":101,\"name\":\"#101\",\"financial_status\":\"paid\",\"total_price\":\"29.00\",\"currency\":\"USD\"}}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
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

func TestListOrdersReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-token", r.Header.Get("X-Shopify-Access-Token"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/admin/api/2026-10/orders.json", r.URL.Path)
		require.Equal(t, "any", r.URL.Query().Get("status"))
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		w.Header().Set("Link", `<https://test-shop.myshopify.com/admin/api/2026-10/orders.json?page_info=next-page&limit=10>; rel="next"`)
		_, err := fmt.Fprint(w, "{\"orders\":[{\"id\":101,\"name\":\"#101\",\"financial_status\":\"paid\",\"total_price\":\"29.00\",\"currency\":\"USD\"}]}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.Orders, 1)
	require.Equal(t, "next-page", result.Value.NextPageInfo)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func TestListOrdersFiltersAndCursor(t *testing.T) {
	queries := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		queries <- request.URL.Query()
		_, err := fmt.Fprint(w, `{"orders":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	_, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Limit: 2, Status: "closed", FinancialStatus: "paid", CreatedAtMin: "2026-01-01T00:00:00Z"})
	require.NoError(t, err)
	filtered := <-queries
	require.Equal(t, "closed", filtered.Get("status"))
	require.Equal(t, "paid", filtered.Get("financial_status"))
	require.Equal(t, "2026-01-01T00:00:00Z", filtered.Get("created_at_min"))
	_, err = sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Limit: 2, PageInfo: "opaque-cursor"})
	require.NoError(t, err)
	cursor := <-queries
	require.Equal(t, "opaque-cursor", cursor.Get("page_info"))
	require.Empty(t, cursor.Get("status"))
	invalid, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{PageInfo: "opaque-cursor", Status: "closed"})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchDefect, invalid.Branch)
}

func TestCountOrdersReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/admin/api/2026-10/orders/count.json", request.URL.Path)
		require.Equal(t, "closed", request.URL.Query().Get("status"))
		require.Equal(t, "test-token", request.Header.Get("X-Shopify-Access-Token"))
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, `{"count":2}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	counted, err := sdkgo.RunQuery(queryContext{}, client.CountOrders(), testConnection, CountOrdersInput{Status: "closed"})
	require.NoError(t, err)
	require.Equal(t, CountOrdersBranchCounted, counted.Branch)
	require.EqualValues(t, 2, counted.Value.Count)
	status = http.StatusForbidden
	rejected, err := sdkgo.RunQuery(queryContext{}, client.CountOrders(), testConnection, CountOrdersInput{Status: "closed"})
	require.NoError(t, err)
	require.Equal(t, CountOrdersBranchProviderRejected, rejected.Branch)
}
