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

func TestProductOperationsUseWaveGraphQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/graphql/public", request.URL.Path)
		var body struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		switch {
		case strings.Contains(body.Query, "query ListProducts"):
			require.JSONEq(t, `{"businessId":"business-1","page":1,"pageSize":20}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"business":{"products":{"pageInfo":{"currentPage":1,"totalPages":1,"totalCount":1},"edges":[{"node":{"id":"product-1","name":"Example Product","description":"Lamp","unitPrice":"2.50","isArchived":false}}]}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "query GetProduct"):
			require.JSONEq(t, `{"businessId":"business-1","productId":"product-1"}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"business":{"product":{"id":"product-1","name":"Example Product","description":"Lamp","unitPrice":"2.50","isArchived":false}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation CreateProduct"):
			require.JSONEq(t, `{"input":{"businessId":"business-1","name":"Example Product","description":"Lamp","unitPrice":"2.50","incomeAccountId":"account-1"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"productCreate":{"didSucceed":true,"inputErrors":[],"product":{"id":"product-1","name":"Example Product","description":"Lamp","unitPrice":"2.50","isArchived":false}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation UpdateProduct"):
			require.JSONEq(t, `{"input":{"id":"product-1","name":"Updated Product","unitPrice":"3.00"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"productPatch":{"didSucceed":true,"inputErrors":[],"product":{"id":"product-1","name":"Updated Product","description":"Lamp","unitPrice":"3","isArchived":false}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation ArchiveProduct"):
			require.JSONEq(t, `{"input":{"id":"product-1"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"productArchive":{"didSucceed":true,"inputErrors":[],"product":{"id":"product-1","name":"Example Product","description":"Lamp","unitPrice":"2.50","isArchived":true}}}}`))
			require.NoError(t, err)
		default:
			t.Fatal("unexpected Wave product operation")
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListProducts(), testConnection, ListProductsInput{BusinessID: "business-1", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, listed.Value.Products, 1)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetProduct(), testConnection, GetProductInput{BusinessID: "business-1", ProductID: "product-1"})
	require.NoError(t, err)
	require.Equal(t, "product-1", found.Value.ID)
	created, err := sdkgo.RunMutation(&customerTestContext{}, client.CreateProduct(), testConnection, CreateProductInput{BusinessID: "business-1", Name: "Example Product", Description: "Lamp", UnitPrice: "2.50", IncomeAccountID: "account-1"})
	require.NoError(t, err)
	require.Equal(t, CreateProductBranchCreated, created.Branch)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateProduct(), testConnection, UpdateProductInput{BusinessID: "business-1", ProductID: "product-1", Name: "Updated Product", UnitPrice: "3.00"})
	require.NoError(t, err)
	require.Equal(t, "3", updated.Value.UnitPrice)
	archived, err := sdkgo.RunMutation(queryContext{}, client.ArchiveProduct(), testConnection, ArchiveProductInput{BusinessID: "business-1", ProductID: "product-1"})
	require.NoError(t, err)
	require.True(t, archived.Value.IsArchived)
}
