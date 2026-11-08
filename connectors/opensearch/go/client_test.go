// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opensearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "opensearch-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "opensearch", Name: "opensearch"}

func newTestClient(t *testing.T, clusterURL string) *Client {
	t.Helper()
	client, err := New(Config{ClusterURL: clusterURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{Username: "dex_reader", Password: sdkgo.NewSecretString("fake-password")}})
	require.NoError(t, err)
	return client
}

func TestSearchDocumentsProviderOutcomes(t *testing.T) {
	status := http.StatusOK
	body := `{"timed_out":false,"_shards":{"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"products","_id":"sku-42","_score":1.25,"_source":{"title":"Meridian"}}]}}`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "dex_reader", username)
		require.Equal(t, "fake-password", password)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/products/_search", r.URL.Path)
		require.Equal(t, "title:Meridian", r.URL.Query().Get("q"))
		require.Equal(t, "10", r.URL.Query().Get("size"))
		require.Equal(t, "0", r.URL.Query().Get("from"))
		require.Equal(t, "false", r.URL.Query().Get("allow_partial_search_results"))
		require.NotContains(t, r.URL.RawQuery, "fake-password")
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := SearchDocumentsInput{Index: "products", Query: "title:Meridian", Size: 10}
	found, err := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SearchDocumentsBranchFound, found.Branch)
	require.EqualValues(t, 1, found.Value.Total)
	require.Equal(t, "sku-42", found.Value.Hits[0].ID)
	status = http.StatusNotFound
	missing, err := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SearchDocumentsBranchIndexNotFound, missing.Branch)
	status = http.StatusUnauthorized
	rejected, err := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SearchDocumentsBranchProviderRejected, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "fake-password")
	status = http.StatusOK
	body = `{"timed_out":true,"_shards":{"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`
	_, err = sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.Error(t, err)
	body = `{}`
	malformed, err := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SearchDocumentsBranchInvalidResponse, malformed.Branch)
	status = http.StatusTooManyRequests
	_, err = sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, input)
	require.Error(t, err)
	badInput, err := sdkgo.RunQuery(queryContext{}, client.SearchDocuments(), testConnection, SearchDocumentsInput{Index: "_all", Query: "*"})
	require.NoError(t, err)
	require.Equal(t, SearchDocumentsBranchDefect, badInput.Branch)
	require.Equal(t, 6, requests)
}
