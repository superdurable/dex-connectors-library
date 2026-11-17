// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCustomerOperationsUseWaveGraphQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/graphql/public", request.URL.Path)
		require.Equal(t, "Bearer sample-wave-access-token", request.Header.Get("Authorization"))
		var body struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		switch {
		case strings.Contains(body.Query, "query ListCustomers"):
			require.JSONEq(t, `{"businessId":"business-1","page":1,"pageSize":20}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"business":{"customers":{"pageInfo":{"currentPage":1,"totalPages":1,"totalCount":1},"edges":[{"node":{"id":"customer-1","name":"Example Customer"}}]}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation CreateCustomer"):
			require.JSONEq(t, `{"input":{"businessId":"business-1","name":"Example Customer"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"customerCreate":{"didSucceed":true,"inputErrors":[],"customer":{"id":"customer-1","name":"Example Customer"}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation UpdateCustomer"):
			require.JSONEq(t, `{"input":{"id":"customer-1","name":"Updated Customer"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"customerPatch":{"didSucceed":true,"inputErrors":[],"customer":{"id":"customer-1","name":"Updated Customer"}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation DeleteCustomer"):
			require.JSONEq(t, `{"input":{"id":"customer-1"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"customerDelete":{"didSucceed":true,"inputErrors":[]}}}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected Wave GraphQL operation")
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListCustomers(), testConnection, ListCustomersInput{BusinessID: "business-1", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, listed.Value.Customers, 1)
	created, err := sdkgo.RunMutation(&customerTestContext{}, client.CreateCustomer(), testConnection, CreateCustomerInput{BusinessID: "business-1", Name: "Example Customer"})
	require.NoError(t, err)
	require.Equal(t, CreateCustomerBranchCreated, created.Branch)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateCustomer(), testConnection, UpdateCustomerInput{BusinessID: "business-1", CustomerID: "customer-1", Name: "Updated Customer"})
	require.NoError(t, err)
	require.Equal(t, "Updated Customer", updated.Value.Name)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteCustomer(), testConnection, DeleteCustomerInput{CustomerID: "customer-1"})
	require.NoError(t, err)
	require.Equal(t, DeleteCustomerBranchDeleted, deleted.Branch)
}
