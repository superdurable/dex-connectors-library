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

func TestConnectionQueriesExcludeProviderLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/connections":
			require.Equal(t, "fooCommerce", r.URL.Query().Get("filters[service]"))
			require.Equal(t, "store-1", r.URL.Query().Get("filters[externalid]"))
			_, err := w.Write([]byte(`{"connections":[{"id":"1","service":"fooCommerce","externalid":"store-1","name":"Store","listId":"2","links":{"customers":"private-url"}}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		case "/api/3/connections/1":
			_, err := w.Write([]byte(`{"connection":{"id":"1","service":"fooCommerce","externalid":"store-1","name":"Store","listId":"2","links":{"customers":"private-url"}}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListConnections(), testConnection, ListConnectionsInput{Service: "fooCommerce", ExternalID: "store-1"})
	require.NoError(t, err)
	require.Equal(t, ListConnectionsBranchListed, page.Branch)
	require.Equal(t, 1, page.Value.Total)
	connection, err := sdkgo.RunQuery(queryContext{}, client.GetConnection(), testConnection, GetConnectionInput{ConnectionID: "1"})
	require.NoError(t, err)
	require.Equal(t, GetConnectionBranchFound, connection.Branch)
	require.Equal(t, page.Value.Connections[0], connection.Value)
	encoded, err := json.Marshal(connection.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
}

func TestCreateConnectionReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/connections", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"connection":{"service":"fooCommerce","externalid":"store-1","name":"Store","listId":"2"}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/connections", r.URL.Path)
			require.Equal(t, "fooCommerce", r.URL.Query().Get("filters[service]"))
			require.Equal(t, "store-1", r.URL.Query().Get("filters[externalid]"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"connections":[{"id":"1","service":"fooCommerce","externalid":"store-1","name":"Store","listId":"2"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateConnectionInput{Service: "fooCommerce", ExternalID: "store-1", Name: "Store", ListID: "2"}
	_, err := sdkgo.RunMutation(ctx, client.CreateConnection(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateConnection(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateConnectionBranchCreated, created.Branch)
	require.Equal(t, "1", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestUpdateAndDeleteConnectionConverge(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			require.Equal(t, "/api/3/connections/1", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"connection":{"name":"Store"}}`, string(body))
			_, err = w.Write([]byte(`{"connection":{"id":"1","service":"fooCommerce","externalid":"store-1","name":"Store","listId":"2"}}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/api/3/connections/1", r.URL.Path)
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
	name := "Store"
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateConnection(), testConnection, UpdateConnectionInput{ConnectionID: "1", Name: &name})
	require.NoError(t, err)
	require.Equal(t, UpdateConnectionBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteConnection(), testConnection, DeleteConnectionInput{ConnectionID: "1"})
		require.NoError(t, err)
		require.Equal(t, DeleteConnectionBranchDeleted, deleted.Branch)
	}
	require.Equal(t, int32(2), deletes.Load())
}
