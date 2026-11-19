// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListListsFiltersProviderFieldsAndPages(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/api/3/lists", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("offset"))
		require.Equal(t, "3", r.URL.Query().Get("limit"))
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		_, err := w.Write([]byte(`{"lists":[{"id":"11","name":"Newsletter","active_subscribers":"7","non_deleted_subscribers":"8","subscription_notify":"private@example.com"}],"meta":{"total":"12"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListLists(), testConnection, ListListsInput{Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListListsBranchListed, result.Branch)
	require.Equal(t, ListsPage{Lists: []ListMetadata{{ID: "11", Name: "Newsletter", ActiveSubscribers: 7, NonDeletedSubscribers: 8}}, Offset: 2, Limit: 3, Total: 12}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
	result, err = sdkgo.RunQuery(queryContext{}, client.ListLists(), testConnection, ListListsInput{Limit: 101})
	require.NoError(t, err)
	require.Equal(t, ListListsBranchDefect, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}

func TestGetListClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/3/lists/11", r.URL.Path)
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"list":{"id":"11","name":"Newsletter","subscription_notify":"private@example.com"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetList(), testConnection, GetListInput{ListID: "11"})
	require.NoError(t, err)
	require.Equal(t, GetListBranchFound, result.Branch)
	require.Equal(t, ListMetadata{ID: "11", Name: "Newsletter"}, result.Value)
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetList(), testConnection, GetListInput{ListID: "11"})
	require.NoError(t, err)
	require.Equal(t, GetListBranchNotFound, result.Branch)
}
