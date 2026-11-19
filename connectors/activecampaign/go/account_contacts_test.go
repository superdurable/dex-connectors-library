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

func TestAccountContactQueriesExcludeProviderLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/accountContacts":
			require.Equal(t, "2", r.URL.Query().Get("filters[account]"))
			require.Equal(t, "3", r.URL.Query().Get("limit"))
			require.Equal(t, "2", r.URL.Query().Get("offset"))
			_, err := w.Write([]byte(`{"accountContacts":[{"id":"17","account":"2","contact":"3","jobTitle":"Manager","links":{"contact":"private-url"}}],"meta":{"total":"12"}}`))
			require.NoError(t, err)
		case "/api/3/accountContacts/17":
			_, err := w.Write([]byte(`{"accountContact":{"id":"17","account":"2","contact":"3","jobTitle":"Manager","links":{"contact":"private-url"}}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListAccountContacts(), testConnection, ListAccountContactsInput{AccountID: "2", Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListAccountContactsBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	link, err := sdkgo.RunQuery(queryContext{}, client.GetAccountContact(), testConnection, GetAccountContactInput{AssociationID: "17"})
	require.NoError(t, err)
	require.Equal(t, GetAccountContactBranchFound, link.Branch)
	require.Equal(t, page.Value.Associations[0], link.Value)
	encoded, err := json.Marshal(link.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
}

func TestCreateAccountContactReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/accountContacts", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"accountContact":{"account":"2","contact":"3","jobTitle":"Manager"}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/accountContacts", r.URL.Path)
			require.Equal(t, "3", r.URL.Query().Get("filters[contact]"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"accountContacts":[{"id":"17","account":"2","contact":"3","jobTitle":"Manager"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateAccountContactInput{AccountID: "2", ContactID: "3", JobTitle: "Manager"}
	_, err := sdkgo.RunMutation(ctx, client.CreateAccountContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateAccountContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateAccountContactBranchCreated, created.Branch)
	require.Equal(t, "17", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestUpdateAndDeleteAccountContactConverge(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			require.Equal(t, "/api/3/accountContacts/17", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"accountContact":{"jobTitle":"Manager"}}`, string(body))
			_, err = w.Write([]byte(`{"accountContact":{"id":"17","account":"2","contact":"3","jobTitle":"Manager"}}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/api/3/accountContacts/17", r.URL.Path)
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
	title := "Manager"
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateAccountContact(), testConnection, UpdateAccountContactInput{AssociationID: "17", JobTitle: &title})
	require.NoError(t, err)
	require.Equal(t, UpdateAccountContactBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteAccountContact(), testConnection, DeleteAccountContactInput{AssociationID: "17"})
		require.NoError(t, err)
		require.Equal(t, DeleteAccountContactBranchDeleted, deleted.Branch)
	}
	require.Equal(t, int32(2), deletes.Load())
}
