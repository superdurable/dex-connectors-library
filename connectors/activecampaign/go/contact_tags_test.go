// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestContactTagsApplyExistingAndRemoveOneContact(t *testing.T) {
	var applications, removals atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/3/contacts/68/contactTags":
			_, err := w.Write([]byte(`{"contactTags":[{"id":"22","contact":"68","tag":"16"}]}`))
			require.NoError(t, err)
		case r.Method == http.MethodPost && r.URL.Path == "/api/3/contacts/68/tags":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"tagIds":["16"],"createMissing":false}`, string(body))
			applications.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, err = w.Write([]byte(`{"contactTags":[{"id":"22","contact":"68","tag":"16"}],"tags":[{"name":"private"}]}`))
			require.NoError(t, err)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/3/contacts/68/tags/16":
			if removals.Add(1) == 1 {
				_, err := w.Write([]byte(`{}`))
				require.NoError(t, err)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListContactTags(), testConnection, ListContactTagsInput{ContactID: "68"})
	require.NoError(t, err)
	require.Equal(t, ListContactTagsBranchListed, listed.Branch)
	require.Len(t, listed.Value.Tags, 1)
	for range 2 {
		applied, err := sdkgo.RunMutation(queryContext{}, client.ApplyTagToContact(), testConnection, ApplyTagToContactInput{ContactID: "68", TagID: "16"})
		require.NoError(t, err)
		require.Equal(t, ApplyTagToContactBranchApplied, applied.Branch)
		require.Equal(t, "22", applied.Value.AssociationID)
	}
	for range 2 {
		removed, err := sdkgo.RunMutation(queryContext{}, client.RemoveTagFromContact(), testConnection, RemoveTagFromContactInput{ContactID: "68", TagID: "16"})
		require.NoError(t, err)
		require.Equal(t, RemoveTagFromContactBranchRemoved, removed.Branch)
		require.True(t, removed.Value.Removed)
	}
	require.Equal(t, int32(2), applications.Load())
	require.Equal(t, int32(2), removals.Load())
}
