// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamics365sales

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func newAccountTestServer(t *testing.T, handleAccount func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			require.Equal(t, http.MethodPost, request.Method)
			_, err := writer.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		handleAccount(writer, request)
	}))
}

func TestListAndGetAccountsReadOnlySelectedFields(t *testing.T) {
	server := newAccountTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "accountid,name", request.URL.Query().Get("$select"))
		if request.URL.Path == "/api/data/v9.2/accounts" {
			require.Equal(t, "20", request.URL.Query().Get("$top"))
			_, err := writer.Write([]byte(`{"value":[{"accountid":"` + testContactID + `","name":"Example"}]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, accountPath(testContactID), request.URL.Path)
		_, err := writer.Write([]byte(`{"accountid":"` + testContactID + `","name":"Example"}`))
		require.NoError(t, err)
	})
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	list, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{Top: 20})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, list.Branch)
	require.Equal(t, []Account{{AccountID: testContactID, Name: "Example"}}, list.Value.Accounts)
	account, err := sdkgo.RunQuery(queryContext{}, client.GetAccount(), testConnection, GetAccountInput{AccountID: testContactID})
	require.NoError(t, err)
	require.Equal(t, GetAccountBranchFound, account.Branch)
	require.Equal(t, "Example", account.Value.Name)
}

func TestCreateAccountUsesStableConditionalIDAndConfirmsPriorWrite(t *testing.T) {
	var accountID string
	patchCount := 0
	server := newAccountTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPatch {
			patchCount++
			require.Equal(t, "*", request.Header.Get("If-None-Match"))
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, "Example", body["name"])
			accountID = strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/data/v9.2/accounts("), ")")
			require.True(t, identifierPattern.MatchString(accountID))
			writer.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, accountPath(accountID), request.URL.Path)
		_, err := writer.Write([]byte(`{"accountid":"` + accountID + `","name":"Example"}`))
		require.NoError(t, err)
	})
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).CreateAccount(), testConnection, CreateAccountInput{Name: "Example"})
	require.NoError(t, err)
	require.Equal(t, CreateAccountBranchCreated, result.Branch)
	require.Equal(t, accountID, result.Value.AccountID)
	require.Equal(t, 1, patchCount)
}

func TestUpdateAndDeleteAccountUseAbsoluteRequests(t *testing.T) {
	server := newAccountTestServer(t, func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, accountPath(testContactID), request.URL.Path)
		switch request.Method {
		case http.MethodPatch:
			require.Equal(t, "*", request.Header.Get("If-Match"))
			writer.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			writer.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected account method %s", request.Method)
		}
	})
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateAccount(), testConnection, UpdateAccountInput{AccountID: testContactID, Name: "Example"})
	require.NoError(t, err)
	require.Equal(t, UpdateAccountBranchUpdated, updated.Branch)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteAccount(), testConnection, DeleteAccountInput{AccountID: testContactID})
	require.NoError(t, err)
	require.Equal(t, DeleteAccountBranchDeleted, deleted.Branch)
}
