// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azureblob

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListBlobsSignsQueryAndParsesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/example-container", request.URL.Path)
		require.Equal(t, "list", request.URL.Query().Get("comp"))
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		require.Equal(t, "100", request.URL.Query().Get("maxresults"))
		require.Equal(t, "reports/", request.URL.Query().Get("prefix"))
		require.NotEmpty(t, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`<EnumerationResults><Blobs><Blob><Name>reports/a.txt</Name></Blob></Blobs><NextMarker>cursor</NextMarker></EnumerationResults>`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListBlobs(), testConnection, ListBlobsInput{Container: "example-container", Prefix: "reports/"})
	require.NoError(t, err)
	require.Equal(t, ListBlobsBranchListed, result.Branch)
	require.Equal(t, BlobsPage{Names: []string{"reports/a.txt"}, NextMarker: "cursor"}, result.Value)
}

func TestPutBlobSendsExactBodyAndMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/example-container/reports/a.txt", request.URL.Path)
		require.Equal(t, "BlockBlob", request.Header.Get("x-ms-blob-type"))
		require.Equal(t, "text/plain", request.Header.Get("Content-Type"))
		require.NotEmpty(t, request.Header.Get("Authorization"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, "hello", string(body))
		writer.Header().Set("ETag", `"0x1"`)
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).PutBlob(), testConnection, PutBlobInput{Container: "example-container", BlobName: "reports/a.txt", DataBase64: "aGVsbG8=", ContentType: "text/plain"})
	require.NoError(t, err)
	require.Equal(t, PutBlobBranchStored, result.Branch)
	require.Equal(t, `"0x1"`, result.Value.ETag)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-test-key")
}

func TestDeleteBlobRemovesSnapshotsAndAcceptsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Equal(t, "include", request.Header.Get("x-ms-delete-snapshots"))
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).DeleteBlob(), testConnection, DeleteBlobInput{Container: "example-container", BlobName: "reports/a.txt"})
	require.NoError(t, err)
	require.Equal(t, DeleteBlobBranchDeleted, result.Branch)
}
