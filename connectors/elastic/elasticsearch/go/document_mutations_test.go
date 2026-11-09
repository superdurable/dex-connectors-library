// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestIndexDocumentReplacesSameStableIDAcrossRetry(t *testing.T) {
	status, requests := http.StatusServiceUnavailable, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "dex_reader", username)
		require.Equal(t, "fake-password", password)
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/products/_doc/sku-42", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("refresh"))
		require.NotContains(t, r.URL.RawQuery, "fake-password")
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"title":"Meridian"}`, string(body))
		w.WriteHeader(status)
		if status == http.StatusCreated {
			_, err := fmt.Fprint(w, `{"_index":"products","_id":"sku-42","result":"created"}`)
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := IndexDocumentInput{Index: "products", DocumentID: "sku-42", Source: []byte(`{"title":"Meridian"}`)}
	_, err := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, input)
	require.Error(t, err)
	status = http.StatusCreated
	indexed, err := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, IndexDocumentBranchIndexed, indexed.Branch)
	require.Equal(t, "sku-42", indexed.Value.ID)
	require.Equal(t, 2, requests)
	status = http.StatusUnauthorized
	rejected, err := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, IndexDocumentBranchProviderRejected, rejected.Branch)
	status = http.StatusOK
	_, err = sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, input)
	require.Error(t, err, "a malformed success response is safe to retry with the same ID")
	invalid, err := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, IndexDocumentInput{Index: "products", DocumentID: "sku-42", Source: []byte(`[]`)})
	require.NoError(t, err)
	require.Equal(t, IndexDocumentBranchDefect, invalid.Branch)
	require.Equal(t, 4, requests)
}

func TestDocumentMutationsTreatUnresolvedCredentialsAsDefect(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	client, err := New(Config{ClusterURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	indexed, err := sdkgo.RunMutation(queryContext{}, client.IndexDocument(), testConnection, IndexDocumentInput{Index: "products", DocumentID: "sku-42", Source: []byte(`{"title":"Meridian"}`)})
	require.NoError(t, err)
	require.Equal(t, IndexDocumentBranchDefect, indexed.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, indexed.Failure.Kind)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteDocument(), testConnection, DeleteDocumentInput{Index: "products", DocumentID: "sku-42"})
	require.NoError(t, err)
	require.Equal(t, DeleteDocumentBranchDefect, deleted.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, deleted.Failure.Kind)
	require.Zero(t, requests)
}

func TestDeleteDocumentTreatsRepeatedDeleteAsSuccess(t *testing.T) {
	status, requests, isIndexMissing := http.StatusOK, 0, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/products/_doc/sku-42", r.URL.Path)
		require.Empty(t, r.URL.Query().Get("refresh"))
		w.WriteHeader(status)
		body := `{"_index":"products","_id":"sku-42","result":"deleted"}`
		if status == http.StatusNotFound {
			body = `{"_index":"products","_id":"sku-42","result":"not_found"}`
			if isIndexMissing {
				body = `{"error":{"type":"index_not_found_exception"},"status":404}`
			}
		}
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := DeleteDocumentInput{Index: "products", DocumentID: "sku-42"}
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteDocument(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, DeleteDocumentBranchAbsent, deleted.Branch)
	require.True(t, deleted.Value.WasDeleted)
	status = http.StatusNotFound
	repeated, err := sdkgo.RunMutation(queryContext{}, client.DeleteDocument(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, DeleteDocumentBranchAbsent, repeated.Branch)
	require.False(t, repeated.Value.WasDeleted)
	isIndexMissing = true
	missingIndex, err := sdkgo.RunMutation(queryContext{}, client.DeleteDocument(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, DeleteDocumentBranchIndexNotFound, missingIndex.Branch)
	require.Equal(t, 3, requests)
}
