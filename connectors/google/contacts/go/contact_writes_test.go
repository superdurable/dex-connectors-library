// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestContactRevisionUpdateAndDelete(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			require.Equal(t, "/v1/people/c123", r.URL.Path)
			require.Equal(t, "names,emailAddresses,metadata", r.URL.Query().Get("personFields"))
			_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace","givenName":"Ada","familyName":"Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}],"metadata":{"sources":[{"type":"CONTACT","etag":"etag-1"}]}}`))
			require.NoError(t, err)
		case http.MethodPatch:
			require.Equal(t, "/v1/people/c123:updateContact", r.URL.Path)
			require.Equal(t, "names,emailAddresses", r.URL.Query().Get("updatePersonFields"))
			_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}]}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/v1/people/c123:deleteContact", r.URL.Path)
			if deletes.Add(1) == 1 {
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
	revision, err := sdkgo.RunQuery(queryContext{}, client.GetContactRevision(), testConnection, GetContactInput{ResourceName: "people/c123"})
	require.NoError(t, err)
	require.Equal(t, GetContactRevisionBranchFound, revision.Branch)
	require.Equal(t, "etag-1", revision.Value.Etag)
	updated, err := sdkgo.RunMutation(singledispatchtest.NewContext("contact-update-flow", "update-step"), client.UpdateContact(), testConnection, UpdateContactInput{ResourceName: "people/c123", Etag: revision.Value.Etag, GivenName: "Ada", FamilyName: "Lovelace", EmailAddress: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("contact-delete-flow", "delete-step"), client.DeleteContact(), testConnection, DeleteContactInput{ResourceName: "people/c123"})
		require.NoError(t, err)
		require.Equal(t, DeleteContactBranchDeleted, deleted.Branch)
	}
}

func TestContactUpdateDetectsStaleRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusBadRequest)
			_, err := w.Write([]byte(`{"error":{"status":"FAILED_PRECONDITION","details":[{"reason":"failedPrecondition"}]}}`))
			require.NoError(t, err)
			return
		}
		_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Grace Hopper","givenName":"Grace","familyName":"Hopper"}],"emailAddresses":[{"value":"grace@example.com"}],"metadata":{"sources":[{"type":"CONTACT","etag":"etag-2"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("contact-stale-flow", "update-step"), client.UpdateContact(), testConnection, UpdateContactInput{ResourceName: "people/c123", Etag: "etag-1", GivenName: "Ada"})
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchStaleRevision, result.Branch)
}

func TestContactWriteBranchesAndRetries(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(int(status.Load())) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	updateInput := UpdateContactInput{ResourceName: "people/c123", Etag: "etag-1", GivenName: "Ada"}
	deleteInput := DeleteContactInput{ResourceName: "people/c123"}
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunMutation(singledispatchtest.NewContext("update-retry", "step"), client.UpdateContact(), testConnection, updateInput)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		_, err = sdkgo.RunMutation(singledispatchtest.NewContext("delete-retry", "step"), client.DeleteContact(), testConnection, deleteInput)
		require.ErrorAs(t, err, &retry)
	}
	status.Store(http.StatusForbidden)
	updated, err := sdkgo.RunMutation(singledispatchtest.NewContext("update-reject", "step"), client.UpdateContact(), testConnection, updateInput)
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchProviderRejected, updated.Branch)
	deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("delete-reject", "step"), client.DeleteContact(), testConnection, deleteInput)
	require.NoError(t, err)
	require.Equal(t, DeleteContactBranchProviderRejected, deleted.Branch)
	bad, err := sdkgo.RunQuery(queryContext{}, client.GetContactRevision(), testConnection, GetContactInput{ResourceName: "bad/name"})
	require.NoError(t, err)
	require.Equal(t, GetContactRevisionBranchDefect, bad.Branch)
}
