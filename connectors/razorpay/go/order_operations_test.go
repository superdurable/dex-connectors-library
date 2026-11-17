// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testOrderID = "order_RB58MiP5SPFYyM"

func TestCreateOrderUsesUniqueCallReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/orders", request.URL.Path)
		var body struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
			Receipt  string `json:"receipt"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, int64(5000), body.Amount)
		require.Equal(t, "INR", body.Currency)
		require.Regexp(t, `^dex_[0-9a-f]{32}$`, body.Receipt)
		_, err := writer.Write([]byte(`{"id":"` + testOrderID + `","entity":"order","amount":5000,"currency":"INR","receipt":"` + body.Receipt + `","status":"created","notes":{"private":"ignored"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := CreateOrderInput{Amount: 5000, CurrencyCode: "INR"}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("razorpay-order", "create-order"), newTestClient(t, server.URL+"/api", 0).CreateOrder(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateOrderBranchCreated, result.Branch)
	require.Equal(t, testOrderID, result.Value.ID)
	require.Equal(t, int64(5000), result.Value.Amount)
}

func TestOrderQueriesReturnSafeSummaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		order := `{"id":"` + testOrderID + `","entity":"order","amount":5000,"currency":"INR","receipt":"receipt-1","status":"created","notes":{"private":"ignored"}}`
		if request.URL.Path == "/api/orders" {
			require.Equal(t, "2", request.URL.Query().Get("count"))
			require.Equal(t, "1", request.URL.Query().Get("skip"))
			_, err := writer.Write([]byte(`{"entity":"collection","count":1,"items":[` + order + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/orders/"+testOrderID, request.URL.Path)
		_, err := writer.Write([]byte(order))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListOrders(), testConnection, ListOrdersInput{Count: 2, Skip: 1})
	require.NoError(t, err)
	require.Equal(t, ListOrdersBranchListed, listed.Branch)
	require.Len(t, listed.Value.Orders, 1)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetOrder(), testConnection, GetOrderInput{OrderID: testOrderID})
	require.NoError(t, err)
	require.Equal(t, GetOrderBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}
