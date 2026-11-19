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

func TestAccountQueriesExcludeProviderRelationships(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/accounts":
			require.Equal(t, "3", r.URL.Query().Get("limit"))
			require.Equal(t, "2", r.URL.Query().Get("offset"))
			_, err := w.Write([]byte(`{"accounts":[{"id":"1","name":"Example Account","accountUrl":"https://example.com","links":{"contacts":"private-url"}}],"meta":{"total":"12"}}`))
			require.NoError(t, err)
		case "/api/3/accounts/1":
			_, err := w.Write([]byte(`{"account":{"id":"1","name":"Example Account","accountUrl":"https://example.com","links":{"contacts":"private-url"}}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	account, err := sdkgo.RunQuery(queryContext{}, client.GetAccount(), testConnection, GetAccountInput{AccountID: "1"})
	require.NoError(t, err)
	require.Equal(t, GetAccountBranchFound, account.Branch)
	require.Equal(t, page.Value.Accounts[0], account.Value)
	encoded, err := json.Marshal(account.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
}

func TestCreateAccountReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/accounts", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"account":{"name":"Example Account","accountUrl":"https://example.com"}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/accounts", r.URL.Path)
			require.Equal(t, "Example Account", r.URL.Query().Get("search"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"accounts":[{"id":"1","name":"Example Account","accountUrl":"https://example.com"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateAccountInput{Name: "Example Account", AccountURL: "https://example.com"}
	_, err := sdkgo.RunMutation(ctx, client.CreateAccount(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateAccount(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateAccountBranchCreated, created.Branch)
	require.Equal(t, "1", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestUpdateAndDeleteAccountConverge(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			require.Equal(t, "/api/3/accounts/1", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"account":{"name":"Example Account"}}`, string(body))
			_, err = w.Write([]byte(`{"account":{"id":"1","name":"Example Account","accountUrl":"https://example.com"}}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/api/3/accounts/1", r.URL.Path)
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
	name := "Example Account"
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateAccount(), testConnection, UpdateAccountInput{AccountID: "1", Name: &name})
	require.NoError(t, err)
	require.Equal(t, UpdateAccountBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteAccount(), testConnection, DeleteAccountInput{AccountID: "1"})
		require.NoError(t, err)
		require.Equal(t, DeleteAccountBranchDeleted, deleted.Branch)
	}
	require.Equal(t, int32(2), deletes.Load())
}
