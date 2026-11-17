// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestEcommerceOrderQueriesExcludeAddressesAndLinks(t *testing.T) {
	order := `{"id":"1","connectionid":"3","externalid":"store-order-1","customerid":"15","email":"person@example.com","currency":"USD","totalPrice":9111,"state":1,"billingAddress":{"phone":"private-phone"},"links":{"customer":"private-url"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/ecomOrders":
			require.Equal(t, "3", r.URL.Query().Get("filters[connectionid]"))
			require.Equal(t, "2", r.URL.Query().Get("offset"))
			require.Equal(t, "3", r.URL.Query().Get("limit"))
			_, err := w.Write([]byte(`{"ecomOrders":[` + order + `],"meta":{"total":"12"}}`))
			require.NoError(t, err)
		case "/api/3/ecomOrders/1":
			_, err := w.Write([]byte(`{"ecomOrder":` + order + `}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListEcommerceOrders(), testConnection, ListEcommerceOrdersInput{ConnectionID: "3", Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListEcommerceOrdersBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetEcommerceOrder(), testConnection, GetEcommerceOrderInput{OrderID: "1"})
	require.NoError(t, err)
	require.Equal(t, GetEcommerceOrderBranchFound, found.Branch)
	require.Equal(t, page.Value.Orders[0], found.Value)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-phone")
	require.NotContains(t, string(encoded), "private-url")
}

func TestEcommerceProductQueriesExcludeProviderLinks(t *testing.T) {
	product := `{"id":"7","orderid":"1","externalid":"product-1","name":"Skateboard","price":"5000","quantity":"1","links":{"order":"private-url"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/3/ecomOrderProducts":
			require.Equal(t, "2", r.URL.Query().Get("offset"))
			require.Equal(t, "3", r.URL.Query().Get("limit"))
			_, err := w.Write([]byte(`{"ecomOrderProducts":[` + product + `],"meta":{"total":"12"}}`))
			require.NoError(t, err)
		case "/api/3/ecomOrderProducts/7":
			_, err := w.Write([]byte(`{"ecomOrderProduct":` + product + `}`))
			require.NoError(t, err)
		case "/api/3/ecomOrders/1/orderProducts":
			_, err := w.Write([]byte(`{"ecomOrderProducts":[` + product + `]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListEcommerceOrderProducts(), testConnection, ListEcommerceOrderProductsInput{Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListEcommerceOrderProductsBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetEcommerceOrderProduct(), testConnection, GetEcommerceOrderProductInput{ProductID: "7"})
	require.NoError(t, err)
	require.Equal(t, GetEcommerceOrderProductBranchFound, found.Branch)
	require.Equal(t, page.Value.Products[0], found.Value)
	perOrder, err := sdkgo.RunQuery(queryContext{}, client.ListProductsForEcommerceOrder(), testConnection, ListProductsForEcommerceOrderInput{OrderID: "1"})
	require.NoError(t, err)
	require.Equal(t, ListProductsForEcommerceOrderBranchListed, perOrder.Branch)
	require.Equal(t, page.Value.Products[0], perOrder.Value.Products[0])
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
}
