// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestObjectMetadataUpdateAndDeletion(t *testing.T) {
	var deleteCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/storage/v1/b/example-bucket/o/reports/a.txt", r.URL.Path)
		switch r.Method {
		case http.MethodPatch:
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			_, err := w.Write([]byte(`{"name":"reports/a.txt","generation":"123"}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "123", r.URL.Query().Get("generation"))
			if deleteCount.Add(1) == 1 {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	updated, err := sdkgo.RunMutation(singledispatchtest.NewContext("metadata-update-flow", "update-step"), client.UpdateObjectMetadata(), testConnection, UpdateObjectMetadataInput{Bucket: "example-bucket", Name: "reports/a.txt", ContentType: "text/plain", Metadata: map[string]string{"department": "reports"}})
	require.NoError(t, err)
	require.Equal(t, UpdateObjectMetadataBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("object-delete-flow", "delete-step"), client.DeleteObject(), testConnection, DeleteObjectInput{Bucket: "example-bucket", Name: "reports/a.txt", Generation: "123"})
		require.NoError(t, err)
		require.Equal(t, DeleteObjectBranchDeleted, deleted.Branch)
	}
}

func TestObjectMetadataBranchesAndRetries(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	updateInput := UpdateObjectMetadataInput{Bucket: "example-bucket", Name: "reports/a.txt", ContentType: "text/plain"}
	deleteInput := DeleteObjectInput{Bucket: "example-bucket", Name: "reports/a.txt"}
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunMutation(singledispatchtest.NewContext("object-update-retry", "update-step"), client.UpdateObjectMetadata(), testConnection, updateInput)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		_, err = sdkgo.RunMutation(singledispatchtest.NewContext("object-delete-retry", "delete-step"), client.DeleteObject(), testConnection, deleteInput)
		require.ErrorAs(t, err, &retry)
	}
	status.Store(http.StatusForbidden)
	updated, err := sdkgo.RunMutation(singledispatchtest.NewContext("object-update-reject", "update-step"), client.UpdateObjectMetadata(), testConnection, updateInput)
	require.NoError(t, err)
	require.Equal(t, UpdateObjectMetadataBranchProviderRejected, updated.Branch)
	deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("object-delete-reject", "delete-step"), client.DeleteObject(), testConnection, deleteInput)
	require.NoError(t, err)
	require.Equal(t, DeleteObjectBranchProviderRejected, deleted.Branch)
	status.Store(http.StatusNotFound)
	updated, err = sdkgo.RunMutation(singledispatchtest.NewContext("object-update-missing", "update-step"), client.UpdateObjectMetadata(), testConnection, updateInput)
	require.NoError(t, err)
	require.Equal(t, UpdateObjectMetadataBranchNotFound, updated.Branch)
	bad := updateInput
	bad.ContentType = "invalid\r\nType"
	updated, err = sdkgo.RunMutation(singledispatchtest.NewContext("object-update-invalid", "update-step"), client.UpdateObjectMetadata(), testConnection, bad)
	require.NoError(t, err)
	require.Equal(t, UpdateObjectMetadataBranchDefect, updated.Branch)
}
