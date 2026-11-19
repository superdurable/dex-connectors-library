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

func TestEcommerceCustomerQueriesExcludeProviderAggregates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/ecomCustomers":
			require.Equal(t, "3", r.URL.Query().Get("filters[connectionid]"))
			require.Equal(t, "store-customer-1", r.URL.Query().Get("filters[externalid]"))
			_, err := w.Write([]byte(`{"ecomCustomers":[{"id":"15","connectionid":"3","externalid":"store-customer-1","email":"person@example.com","links":{"orders":"private-url"},"totalRevenue":"900"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		case "/api/3/ecomCustomers/15":
			_, err := w.Write([]byte(`{"ecomCustomer":{"id":"15","connectionid":"3","externalid":"store-customer-1","email":"person@example.com","links":{"orders":"private-url"},"totalRevenue":"900"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListEcommerceCustomers(), testConnection, ListEcommerceCustomersInput{ConnectionID: "3", ExternalID: "store-customer-1"})
	require.NoError(t, err)
	require.Equal(t, ListEcommerceCustomersBranchListed, page.Branch)
	require.Equal(t, 1, page.Value.Total)
	customer, err := sdkgo.RunQuery(queryContext{}, client.GetEcommerceCustomer(), testConnection, GetEcommerceCustomerInput{CustomerID: "15"})
	require.NoError(t, err)
	require.Equal(t, GetEcommerceCustomerBranchFound, customer.Branch)
	require.Equal(t, page.Value.Customers[0], customer.Value)
	encoded, err := json.Marshal(customer.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
	require.NotContains(t, string(encoded), "totalRevenue")
}

func TestCreateEcommerceCustomerReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/ecomCustomers", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"ecomCustomer":{"connectionid":"3","externalid":"store-customer-1","email":"person@example.com"}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/ecomCustomers", r.URL.Path)
			require.Equal(t, "3", r.URL.Query().Get("filters[connectionid]"))
			require.Equal(t, "store-customer-1", r.URL.Query().Get("filters[externalid]"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"ecomCustomers":[{"id":"15","connectionid":"3","externalid":"store-customer-1","email":"person@example.com"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateEcommerceCustomerInput{ConnectionID: "3", ExternalID: "store-customer-1", Email: "person@example.com"}
	_, err := sdkgo.RunMutation(ctx, client.CreateEcommerceCustomer(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateEcommerceCustomer(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateEcommerceCustomerBranchCreated, created.Branch)
	require.Equal(t, "15", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestUpdateAndDeleteEcommerceCustomerConverge(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			require.Equal(t, "/api/3/ecomCustomers/15", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"ecomCustomer":{"externalid":"store-customer-1"}}`, string(body))
			_, err = w.Write([]byte(`{"ecomCustomer":{"id":"15","connectionid":"3","externalid":"store-customer-1","email":"person@example.com"}}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/api/3/ecomCustomers/15", r.URL.Path)
			if deletes.Add(1) == 1 {
				_, err := w.Write([]byte(`{}`))
				require.NoError(t, err)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	externalID := "store-customer-1"
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateEcommerceCustomer(), testConnection, UpdateEcommerceCustomerInput{CustomerID: "15", ExternalID: &externalID})
	require.NoError(t, err)
	require.Equal(t, UpdateEcommerceCustomerBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteEcommerceCustomer(), testConnection, DeleteEcommerceCustomerInput{CustomerID: "15"})
		require.NoError(t, err)
		require.Equal(t, DeleteEcommerceCustomerBranchDeleted, deleted.Branch)
	}
	require.Equal(t, int32(2), deletes.Load())
}
