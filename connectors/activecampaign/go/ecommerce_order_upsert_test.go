// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func testUpsertOrderInput() UpsertEcommerceOrderInput {
	return UpsertEcommerceOrderInput{ConnectionID: "3", StoreOrderID: "store-order-1", StoreCustomerID: "store-customer-1", Email: "person@example.com", CreatedAt: "2026-11-16T10:00:00Z", Currency: "USD", FinalAmount: "91.11", CreationSource: "HISTORICAL", Status: "COMPLETED"}
}

func TestUpsertEcommerceOrderUsesGraphQLStoreKey(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/3/ecom/graphql", r.URL.Path)
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var payload struct {
			Query string `json:"query"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Contains(t, payload.Query, `legacyConnectionId: 3`)
		require.Contains(t, payload.Query, `storeOrderId: "store-order-1"`)
		require.Contains(t, payload.Query, `finalAmount: 91.11`)
		require.Contains(t, payload.Query, `creationSource: HISTORICAL`)
		if sends.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err = w.Write([]byte(`{"data":{"upsertOrder":{"connectionId":3,"storeOrderId":"store-order-1","email":"person@example.com"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	input := testUpsertOrderInput()
	ctx := &dealRetryContext{attempt: 1}
	_, err := sdkgo.RunMutation(ctx, client.UpsertEcommerceOrder(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.UpsertEcommerceOrder(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UpsertEcommerceOrderBranchUpserted, result.Branch)
	require.Equal(t, "store-order-1", result.Value.StoreOrderID)
	require.Equal(t, int32(2), sends.Load())
}

func TestUpsertEcommerceOrderRejectsInvalidInputBeforeSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected send") }))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	input := testUpsertOrderInput()
	input.ConnectionID = "99999999999999999999999"
	result, err := sdkgo.RunMutation(queryContext{}, client.UpsertEcommerceOrder(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UpsertEcommerceOrderBranchDefect, result.Branch)
}
