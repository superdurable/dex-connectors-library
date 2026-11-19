// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestListAccountsExcludesContactDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/accounts", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("limit"))
		_, err := writer.Write([]byte(`{"object":"list","has_more":false,"data":[{"id":"e28zov4fw0v2","code":"acme-corp","email":"private@example.com","billing_info":{"number":"private"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).ListAccounts(), testConnection, ListAccountsInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, result.Branch)
	require.Len(t, result.Value.Accounts, 1)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestCreateAccountDerivesUniqueCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/accounts", request.URL.Path)
		var body struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Regexp(t, `^demo_[0-9a-f]{32}$`, body.Code)
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"` + body.Code + `","email":"private@example.com"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("recurly-account", "create-account"), newTestClient(t, server.URL+"/api", 0).CreateAccount(), testConnection, CreateAccountInput{CodePrefix: "demo"})
	require.NoError(t, err)
	require.Equal(t, CreateAccountBranchCreated, result.Branch)
	require.Equal(t, "e28zov4fw0v2", result.Value.ID)
}
