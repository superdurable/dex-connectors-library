// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package plaid

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetItemProjectsLinkedIdentity(t *testing.T) {
	status := http.StatusOK
	body := `{"item":{"item_id":"item_123456","institution_id":"ins_109508","products":["auth"],"access_token":"discard-me"}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/item/get", request.URL.Path)
		require.JSONEq(t, `{"access_token":"sample-plaid-access-token"}`, readRequestBody(t, request))
		writer.WriteHeader(status)
		_, err := writer.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchFound, result.Branch)
	require.Equal(t, ItemSummary{ItemID: "item_123456", InstitutionID: "ins_109508", Products: []string{"auth"}}, result.Value)
	status = http.StatusBadRequest
	body = `{"error_type":"ITEM_ERROR","error_code":"ITEM_NOT_FOUND"}`
	result, err = sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchNotFound, result.Branch)
	status = http.StatusForbidden
	body = `{}`
	result, err = sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchProviderRejected, result.Branch)
	status = http.StatusOK
	body = `{"item":{"item_id":"bad id"}}`
	result, err = sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchInvalidResponse, result.Branch)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status = retryStatus
		_, err = sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), "sample-plaid-access-token")
	}
}

func TestRemoveItemTreatsAlreadyAbsentAsRemoved(t *testing.T) {
	status := http.StatusOK
	body := `{"request_id":"example"}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/item/remove", request.URL.Path)
		require.JSONEq(t, `{"access_token":"sample-plaid-access-token"}`, readRequestBody(t, request))
		writer.WriteHeader(status)
		_, err := writer.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	result, err := sdkgo.RunMutation(queryContext{}, client.RemoveItem(), testConnection, RemoveItemInput{})
	require.NoError(t, err)
	require.Equal(t, RemoveItemBranchRemoved, result.Branch)
	require.True(t, result.Value.Removed)
	status = http.StatusBadRequest
	body = `{"error_type":"ITEM_ERROR","error_code":"ITEM_NOT_FOUND"}`
	result, err = sdkgo.RunMutation(queryContext{}, client.RemoveItem(), testConnection, RemoveItemInput{})
	require.NoError(t, err)
	require.Equal(t, RemoveItemBranchRemoved, result.Branch)
	status = http.StatusForbidden
	body = `{}`
	result, err = sdkgo.RunMutation(queryContext{}, client.RemoveItem(), testConnection, RemoveItemInput{})
	require.NoError(t, err)
	require.Equal(t, RemoveItemBranchProviderRejected, result.Branch)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status = retryStatus
		_, err = sdkgo.RunMutation(queryContext{}, client.RemoveItem(), testConnection, RemoveItemInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), "sample-plaid-access-token")
	}
}
