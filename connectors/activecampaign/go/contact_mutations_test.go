// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestContactMutationsSendOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, testAPIKey, request.Header.Get("Api-Token"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload struct {
			Contact map[string]string `json:"contact"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		switch request.URL.Path {
		case "/api/3/contact/sync":
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, map[string]string{"email": "ada@example.com", "firstName": "Ada"}, payload.Contact)
			w.WriteHeader(http.StatusCreated)
		case "/api/3/contacts/68":
			require.Equal(t, http.MethodPut, request.Method)
			require.Equal(t, map[string]string{"lastName": "Lovelace"}, payload.Contact)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		_, err := w.Write([]byte(`{"contact":{"id":"68","email":"ada@example.com","firstName":"Ada","lastName":"Lovelace"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	firstName, lastName := "Ada", "Lovelace"
	synced, err := sdkgo.RunMutation(queryContext{}, client.SyncContact(), testConnection, SyncContactInput{Email: "ada@example.com", FirstName: &firstName})
	require.NoError(t, err)
	require.Equal(t, SyncContactBranchSynced, synced.Branch)
	require.Equal(t, "68", synced.Value.ID)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateContact(), testConnection, UpdateContactInput{ContactID: "68", LastName: &lastName})
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchUpdated, updated.Branch)
	require.Equal(t, "Lovelace", updated.Value.LastName)
}

func TestContactMutationClassifiesProviderOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"rejected", 422, `{"errors":[{"detail":"bad contact"}]}`, SyncContactBranchProviderRejected, false},
		{"invalid", 200, `{"contact":{}}`, SyncContactBranchInvalidResponse, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"unknown", 500, `{}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, err := w.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/3")
			result, err := sdkgo.RunMutation(queryContext{}, client.SyncContact(), testConnection, SyncContactInput{Email: "ada@example.com"})
			if test.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
			}
			require.NotContains(t, fmt.Sprint(result.Failure, err), testAPIKey)
		})
	}
}

func TestContactMutationRejectsInvalidInputBeforeDispatch(t *testing.T) {
	client := newTestClient(t, "https://example.com/api/3")
	result, err := sdkgo.RunMutation(queryContext{}, client.SyncContact(), testConnection, SyncContactInput{Email: "bad\naddress"})
	require.NoError(t, err)
	require.Equal(t, SyncContactBranchDefect, result.Branch)
	result, err = sdkgo.RunMutation(queryContext{}, client.UpdateContact(), testConnection, UpdateContactInput{ContactID: "68"})
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchDefect, result.Branch)
}
